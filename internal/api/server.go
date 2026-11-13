// Package api implements the REST API for policy management and metrics.
//
// Properties:
//   - binds to 127.0.0.1 by default (localhost only)
//   - administrative operations require a bearer token
//   - request bodies are bounded (MaxBodyBytes)
//   - read/write/idle timeouts are set on the HTTP server
//   - errors are structured JSON {"error": code, "detail": msg}
//   - the active policy can be viewed
//   - a policy can be validated without applying it
//   - policies are applied by version with optimistic concurrency
//     (base_version compare-and-swap), so concurrent writers cannot
//     silently overwrite each other
//   - an invalid policy is rejected before any state changes, preserving
//     the active policy
//   - administrative actions are audit-logged (never with secrets)
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/metrics"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/store"
)

// MaxBodyBytes bounds request bodies (16 KiB covers realistic policies).
const MaxBodyBytes = 16 << 10

// Logger is the minimal logging surface the API needs.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
}

// Server is the API server.
type Server struct {
	mu         *sync.Mutex // shared policy-mutation lock (nil → own)
	token      string
	store      *store.Store
	king       *policy.King
	metrics    *metrics.Registry
	log        Logger
	ready      func() bool
	srv        *http.Server
	actualAddr atomic.Value // string
}

// Options configures the server.
type Options struct {
	Host    string
	Port    int
	Token   string
	Store   *store.Store
	King    *policy.King
	Metrics *metrics.Registry
	Log     Logger
	// Mu optionally serializes policy mutations with the daemon's own
	// publish path (SIGHUP reload). When nil, the server uses its own
	// internal mutex; sharing one mutex guarantees a total order on
	// policy publishes so no update is lost between the API and a
	// daemon reload.
	Mu *sync.Mutex
	// Ready reports whether the daemon is ready to serve traffic
	// (policy active, hook installed, queue bound).
	Ready func() bool
}

// New builds the API server (it does not listen until Serve).
func New(o Options) (*Server, error) {
	if o.Host == "" {
		o.Host = "127.0.0.1"
	}
	if o.Store == nil {
		return nil, errors.New("api: nil store")
	}
	if o.King == nil {
		return nil, errors.New("api: nil king")
	}
	if o.Metrics == nil {
		return nil, errors.New("api: nil metrics")
	}
	ready := o.Ready
	if ready == nil {
		ready = func() bool { return true }
	}
	mu := o.Mu
	if mu == nil {
		mu = &sync.Mutex{}
	}
	s := &Server{
		mu:      mu,
		token:   o.Token,
		store:   o.Store,
		king:    o.King,
		metrics: o.Metrics,
		log:     o.Log,
		ready:   ready,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/policy", s.handleGetPolicy)
	mux.HandleFunc("GET /v1/policy/versions", s.handleListVersions)
	mux.HandleFunc("POST /v1/policy", s.handleApplyNew)
	mux.HandleFunc("POST /v1/policy/validate", s.handleValidate)
	mux.HandleFunc("POST /v1/policy/versions/{version}/activate", s.handleActivate)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	s.srv = &http.Server{
		Addr:              net.JoinHostPort(o.Host, strconv.Itoa(o.Port)),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	return s, nil
}

// Serve listens and serves until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return fmt.Errorf("api: listen: %w", err)
	}
	s.actualAddr.Store(ln.Addr().String())
	errCh := make(chan error, 1)
	go func() {
		err := s.srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// Addr returns the bound address (usable after Serve has begun).
func (s *Server) Addr() string {
	if v, ok := s.actualAddr.Load().(string); ok && v != "" {
		return v
	}
	return s.srv.Addr
}

// ---------------------------------------------------------------------------
// Structured errors
// ---------------------------------------------------------------------------

type apiError struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiError{Error: code, Detail: detail})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

// authorized reports whether the request carries the valid bearer token.
// When no token is configured, mutations are disabled entirely.
func (s *Server) authorized(r *http.Request) bool {
	if s.token == "" {
		return false
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	return constantTimeEqual(strings.TrimPrefix(auth, "Bearer "), s.token)
}

// requireAuth enforces authentication for an administrative operation and
// audit-logs the outcome. It returns false when the caller must stop.
func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request, action string) bool {
	if !s.authorized(r) {
		// Never log the presented credential.
		s.audit(r, action, "denied", "", "missing or invalid token")
		writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
		return false
	}
	return true
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// audit writes one audit record for an administrative action. Secrets are
// never included.
func (s *Server) audit(r *http.Request, action, outcome, version, detail string) {
	if s.log == nil {
		return
	}
	args := []any{
		"audit", true,
		"action", action,
		"outcome", outcome,
		"remote", remoteIP(r),
	}
	if version != "" {
		args = append(args, "version", version)
	}
	if detail != "" {
		args = append(args, "detail", detail)
	}
	if outcome == "denied" {
		s.log.Warn("admin action", args...)
		return
	}
	s.log.Info("admin action", args...)
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------------------------------------------------------------------------
// Request decoding
// ---------------------------------------------------------------------------

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large",
				fmt.Sprintf("request body exceeds %d bytes", MaxBodyBytes))
			return nil, false
		}
		writeError(w, http.StatusBadRequest, "bad_request", "could not read body")
		return nil, false
	}
	return body, true
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleGetPolicy(w http.ResponseWriter, _ *http.Request) {
	cur, err := s.store.Active()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if cur == "" {
		writeError(w, http.StatusNotFound, "no_active_policy", "no policy has been applied yet")
		return
	}
	data, err := s.store.ReadPolicy(cur)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	snap := s.king.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":        cur,
		"policy":         string(data),
		"rules":          snap.Len(),
		"default_action": snap.DefaultAction().String(),
	})
}

func (s *Server) handleListVersions(w http.ResponseWriter, _ *http.Request) {
	versions, err := s.store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	active, _ := s.store.Active()
	writeJSON(w, http.StatusOK, map[string]any{"active": active, "versions": versions})
}

// handleValidate compiles a policy without applying it.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r, "validate_policy") {
		return
	}
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var req struct {
		Policy string `json:"policy"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Policy == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "missing policy")
		return
	}
	snap, err := policy.Compile([]byte(req.Policy))
	if err != nil {
		s.audit(r, "validate_policy", "invalid", "", "")
		writeError(w, http.StatusUnprocessableEntity, "invalid_policy", err.Error())
		return
	}
	s.audit(r, "validate_policy", "ok", "", "")
	writeJSON(w, http.StatusOK, map[string]any{
		"valid":          true,
		"rules":          snap.Len(),
		"default_action": snap.DefaultAction().String(),
	})
}

// handleApplyNew stores a new immutable version and activates it, guarded
// by base_version compare-and-swap.
func (s *Server) handleApplyNew(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r, "apply_policy") {
		return
	}
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var req struct {
		Policy      string `json:"policy"`
		BaseVersion string `json:"base_version"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Policy == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "missing policy")
		return
	}

	// Compile before touching state: an invalid policy changes nothing.
	snap, err := policy.Compile([]byte(req.Policy))
	if err != nil {
		s.audit(r, "apply_policy", "invalid", "", "rejected")
		s.metrics.ObservePolicyUpdate(metrics.PolicyUpdateFailure)
		writeError(w, http.StatusUnprocessableEntity, "invalid_policy", err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	active, err := s.store.Active()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if req.BaseVersion != active {
		s.audit(r, "apply_policy", "conflict", active, "base_version mismatch")
		s.metrics.ObservePolicyUpdate(metrics.PolicyUpdateConflict)
		writeError(w, http.StatusConflict, "version_conflict",
			fmt.Sprintf("active version is %q, not %q", active, req.BaseVersion))
		return
	}

	next, err := s.store.NextVersion()
	if err != nil {
		s.metrics.ObservePolicyUpdate(metrics.PolicyUpdateFailure)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := s.store.WritePolicy(next, []byte(req.Policy)); err != nil {
		s.metrics.ObservePolicyUpdate(metrics.PolicyUpdateFailure)
		writeError(w, http.StatusConflict, "store_write_failed", err.Error())
		return
	}
	if err := s.store.SetActive(next); err != nil {
		s.metrics.ObservePolicyUpdate(metrics.PolicyUpdateFailure)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	s.king.Replace(snap)
	s.setVersionMetric(next)
	s.metrics.ObservePolicyUpdate(metrics.PolicyUpdateSuccess)
	s.audit(r, "apply_policy", "applied", next, fmt.Sprintf("rules=%d", snap.Len()))
	writeJSON(w, http.StatusCreated, map[string]any{"applied": next, "previous": active})
}

// handleActivate activates an already-stored version (rollback), guarded
// by base_version compare-and-swap.
func (s *Server) handleActivate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r, "activate_policy") {
		return
	}
	target := r.PathValue("version")
	if err := store.ValidateVersion(target); err != nil {
		writeError(w, http.StatusBadRequest, "bad_version", err.Error())
		return
	}
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var req struct {
		BaseVersion string `json:"base_version"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}
	}

	data, err := s.store.ReadPolicy(target)
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown_version", err.Error())
		return
	}
	snap, err := policy.Compile(data)
	if err != nil {
		// Stored policies are validated on write; this protects against
		// tampering/corruption and preserves the active policy.
		s.metrics.ObservePolicyUpdate(metrics.PolicyUpdateFailure)
		writeError(w, http.StatusUnprocessableEntity, "invalid_stored_policy", err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	active, err := s.store.Active()
	if err != nil {
		s.metrics.ObservePolicyUpdate(metrics.PolicyUpdateFailure)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if req.BaseVersion != active {
		s.audit(r, "activate_policy", "conflict", active, "base_version mismatch")
		s.metrics.ObservePolicyUpdate(metrics.PolicyUpdateConflict)
		writeError(w, http.StatusConflict, "version_conflict",
			fmt.Sprintf("active version is %q, not %q", active, req.BaseVersion))
		return
	}
	if err := s.store.SetActive(target); err != nil {
		s.metrics.ObservePolicyUpdate(metrics.PolicyUpdateFailure)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	s.king.Replace(snap)
	s.setVersionMetric(target)
	s.metrics.ObservePolicyUpdate(metrics.PolicyUpdateSuccess)
	s.audit(r, "activate_policy", "activated", target, "")
	writeJSON(w, http.StatusOK, map[string]any{"active": target, "previous": active})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if !s.ready() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"reason": "firewall not fully started or no active policy",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	s.metrics.WritePrometheus(w)
}

func (s *Server) setVersionMetric(version string) {
	if n, err := strconv.ParseInt(version, 10, 64); err == nil {
		s.metrics.SetActivePolicyVersion(n)
	}
}
