package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/metrics"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/store"
)

const testToken = "test-secret-token"

func validPolicy(version string) string {
	return "version: 1\ndefault_action: DROP\nrules:\n  - id: r\n    action: ACCEPT\n    protocol: tcp\n    dst_port: " + version + "\n"
}

type testLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *testLogger) Info(msg string, args ...any) { l.append("info", msg, args...) }
func (l *testLogger) Warn(msg string, args ...any) { l.append("warn", msg, args...) }
func (l *testLogger) append(kind, msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, kind+"|"+msg+"|"+fmt.Sprint(args...))
}
func (l *testLogger) contains(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, ln := range l.lines {
		if strings.Contains(ln, sub) {
			return true
		}
	}
	return false
}

type fixture struct {
	t     *testing.T
	store *store.Store
	king  *policy.King
	srv   *Server
	log   *testLogger
	port  int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	king := policy.NewKing()
	l := &testLogger{}
	srv, err := New(Options{
		Host:    "127.0.0.1",
		Port:    0,
		Token:   testToken,
		Store:   st,
		King:    king,
		Metrics: metrics.New(),
		Log:     l,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, store: st, king: king, srv: srv, log: l}
}

func (f *fixture) serve() {
	f.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	go f.srv.Serve(ctx)
	f.t.Cleanup(cancel)
	// Addr() reports the configured address until Serve has bound the
	// listener (net.Listen). Wait for the real bound port instead of a
	// fixed sleep; a fixed sleep races slow machines and race builds.
	deadline := time.Now().Add(5 * time.Second)
	for strings.HasSuffix(f.srv.Addr(), ":0") {
		if time.Now().After(deadline) {
			f.t.Fatal("api server did not bind within 5s")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (f *fixture) addr() string {
	return f.srv.Addr()
}

func (f *fixture) request(method, path string, body string, authed bool) (int, map[string]any, string) {
	f.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, "http://"+f.addr()+path, rd)
	if err != nil {
		f.t.Fatal(err)
	}
	if authed {
		req.Header.Set("Authorization", "Bearer "+testToken)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, m, string(raw)
}

func applyV(f *fixture, base string) string {
	f.t.Helper()
	next := "1"
	if base != "" {
		n, _ := strconv.Atoi(base)
		next = strconv.Itoa(n + 1)
	}
	body := fmt.Sprintf(`{"policy": %q, "base_version": %q}`, validPolicy(next), base)
	code, m, raw := f.request("POST", "/v1/policy", body, true)
	if code != http.StatusCreated {
		f.t.Fatalf("apply: code=%d %s", code, raw)
	}
	return m["applied"].(string)
}

func TestAPIApplyAndView(t *testing.T) {
	f := newFixture(t)
	f.serve()

	v := applyV(f, "")
	code, m, _ := f.request("GET", "/v1/policy", "", false)
	if code != http.StatusOK {
		t.Fatalf("GET policy: %d", code)
	}
	if m["version"] != v {
		t.Errorf("version = %v, want %s", m["version"], v)
	}
	if m["rules"].(float64) != 1 {
		t.Errorf("rules = %v", m["rules"])
	}
}

func TestAPIAuthRequired(t *testing.T) {
	f := newFixture(t)
	f.serve()

	// Unauthenticated mutation → 401.
	code, m, _ := f.request("POST", "/v1/policy", `{"policy": "", "base_version": ""}`, false)
	if code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", code)
	}
	if m["error"] != "unauthorized" {
		t.Errorf("error = %v", m["error"])
	}
	// Wrong token → 401.
	req, _ := http.NewRequest("POST", "http://"+f.addr()+"/v1/policy", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token code = %d, want 401", resp.StatusCode)
	}
	// Audit log has the denial.
	if !f.log.contains("denied") {
		t.Error("no audit denial logged")
	}
	if f.log.contains("wrong") {
		t.Error("audit log leaked the presented token")
	}
}

func TestAPIInvalidPolicyPreservesActive(t *testing.T) {
	f := newFixture(t)
	f.serve()
	v := applyV(f, "")

	body := `{"policy": "version: 99\nrules: []\n", "base_version": "` + v + `"}`
	code, m, _ := f.request("POST", "/v1/policy", body, true)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422", code)
	}
	if m["error"] != "invalid_policy" {
		t.Errorf("error = %v", m["error"])
	}
	// Active policy untouched.
	code, m, _ = f.request("GET", "/v1/policy", "", false)
	if code != http.StatusOK || m["version"] != v {
		t.Errorf("active policy changed after invalid apply: code=%d %v", code, m)
	}
	// Server still healthy.
	code, _, _ = f.request("GET", "/healthz", "", false)
	if code != http.StatusOK {
		t.Errorf("healthz = %d", code)
	}
}

func TestAPIConflict(t *testing.T) {
	f := newFixture(t)
	f.serve()
	v1 := applyV(f, "")
	v2 := applyV(f, v1)

	// Applying with a stale base_version → 409.
	body := fmt.Sprintf(`{"policy": %q, "base_version": %q}`, validPolicy("99"), v1)
	code, m, _ := f.request("POST", "/v1/policy", body, true)
	if code != http.StatusConflict {
		t.Fatalf("code = %d, want 409", code)
	}
	if m["error"] != "version_conflict" {
		t.Errorf("error = %v", m["error"])
	}
	// Still v2.
	code, m, _ = f.request("GET", "/v1/policy", "", false)
	if m["version"] != v2 {
		t.Errorf("active = %v, want %s", m["version"], v2)
	}
}

func TestAPIConcurrentApply(t *testing.T) {
	f := newFixture(t)
	f.serve()
	v0 := applyV(f, "")

	// Two clients race with the same base_version; exactly one must win.
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := fmt.Sprintf(`{"policy": %q, "base_version": %q}`, validPolicy("2"), v0)
			code, _, _ := f.request("POST", "/v1/policy", body, true)
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	created, conflicted := 0, 0
	for c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicted++
		}
	}
	if created != 1 || conflicted != 1 {
		t.Errorf("created=%d conflicted=%d, want 1/1", created, conflicted)
	}
}

func TestAPIRestartRecovery(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a previous daemon run: store v1 and activate it.
	if err := st.WritePolicy("1", []byte(validPolicy("1"))); err != nil {
		t.Fatal(err)
	}
	if err := st.SetActive("1"); err != nil {
		t.Fatal(err)
	}

	// New "process" on the same dir.
	king := policy.NewKing()
	// Recover like the daemon: read active, compile, replace.
	active, err := st.Active()
	if err != nil {
		t.Fatal(err)
	}
	data, err := st.ReadPolicy(active)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := policy.Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	king.Replace(snap)

	f := &fixture{t: t, store: st, king: king, srv: nil, log: &testLogger{}}
	srv, err := New(Options{
		Host: "127.0.0.1", Port: 0, Token: testToken,
		Store: st, King: king, Metrics: metrics.New(), Log: f.log,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.srv = srv
	f.serve()

	code, m, _ := f.request("GET", "/v1/policy", "", false)
	if code != http.StatusOK || m["version"] != "1" {
		t.Errorf("recovery: code=%d version=%v", code, m["version"])
	}
}

func TestAPIValidateEndpoint(t *testing.T) {
	f := newFixture(t)
	f.serve()

	good := fmt.Sprintf(`{"policy": %q}`, validPolicy("1"))
	code, m, _ := f.request("POST", "/v1/policy/validate", good, true)
	if code != http.StatusOK || m["valid"] != true {
		t.Errorf("valid policy: code=%d %v", code, m)
	}

	bad := `{"policy": "version: 99\n"}`
	code, m, _ = f.request("POST", "/v1/policy/validate", bad, true)
	if code != http.StatusUnprocessableEntity || m["error"] != "invalid_policy" {
		t.Errorf("invalid policy: code=%d %v", code, m)
	}

	// Unauthenticated validate → 401.
	code, _, _ = f.request("POST", "/v1/policy/validate", good, false)
	if code != http.StatusUnauthorized {
		t.Errorf("unauth validate: %d", code)
	}
}

func TestAPIActivateVersion(t *testing.T) {
	f := newFixture(t)
	f.serve()
	v1 := applyV(f, "")
	v2 := applyV(f, v1)

	// Roll back to v1.
	body := fmt.Sprintf(`{"base_version": %q}`, v2)
	code, m, _ := f.request("POST", "/v1/policy/versions/"+v1+"/activate", body, true)
	if code != http.StatusOK {
		t.Fatalf("activate: %d %v", code, m)
	}
	code, m, _ = f.request("GET", "/v1/policy", "", false)
	if m["version"] != v1 {
		t.Errorf("active = %v, want %s", m["version"], v1)
	}

	// Stale base → conflict.
	body = fmt.Sprintf(`{"base_version": %q}`, v2)
	code, m, _ = f.request("POST", "/v1/policy/versions/"+v1+"/activate", body, true)
	if code != http.StatusConflict {
		t.Errorf("stale activate: %d %v", code, m)
	}
}

func TestAPIBodyLimit(t *testing.T) {
	f := newFixture(t)
	f.serve()
	big := strings.Repeat("a", MaxBodyBytes+100)
	body := fmt.Sprintf(`{"policy": %q, "base_version": ""}`, big)
	req, _ := http.NewRequest("POST", "http://"+f.addr()+"/v1/policy", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("code = %d, want 413", resp.StatusCode)
	}
}

func TestAPIReadiness(t *testing.T) {
	f := newFixture(t)
	ready := false
	f.srv.ready = func() bool { return ready }
	f.serve()

	code, m, _ := f.request("GET", "/readyz", "", false)
	if code != http.StatusServiceUnavailable {
		t.Errorf("not ready: %d %v", code, m)
	}
	ready = true
	code, _, _ = f.request("GET", "/readyz", "", false)
	if code != http.StatusOK {
		t.Errorf("ready: %d", code)
	}
}

func TestAPIMetrics(t *testing.T) {
	f := newFixture(t)
	f.serve()
	applyV(f, "")

	code, _, raw := f.request("GET", "/metrics", "", false)
	if code != http.StatusOK {
		t.Fatalf("metrics: %d", code)
	}
	for _, want := range []string{
		"goshisen_active_policy_version",
		"goshisen_packets_total",
		"goshisen_backend_errors_total",
		"goshisen_packet_verdict_latency_seconds",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("metrics missing %s", want)
		}
	}
}

func TestStoreAtomicity(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WritePolicy("1", []byte(validPolicy("1"))); err != nil {
		t.Fatal(err)
	}
	if err := st.SetActive("1"); err != nil {
		t.Fatal(err)
	}
	// active.json must be parseable and correct.
	data, err := os.ReadFile(filepath.Join(dir, "active.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ptr store.ActivePointer
	if err := json.Unmarshal(data, &ptr); err != nil {
		t.Fatalf("active.json corrupted: %v", err)
	}
	if ptr.Version != "1" {
		t.Errorf("active = %s", ptr.Version)
	}
	// No stray temp files.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("stray temp file: %s", e.Name())
		}
	}
}
