// Package app implements the goshisen daemon lifecycle: logging setup,
// startup readiness, signal handling, and graceful shutdown. Phase 1
// provides the process skeleton; Phase 3 wires the real NFQUEUE hook and
// enforcement loop.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/api"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/config"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/hook"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/metrics"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/packet"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/store"
)

// Version exposes the build version to the runtime logger.
var Version = "dev"

// Queue is the packet transport the daemon drives. *packet.Backend is
// the production implementation; tests substitute a fake to exercise the
// lifecycle without CAP_NET_ADMIN.
type Queue interface {
	SetDecider(func(policy.Packet) policy.Verdict)
	Run(ctx context.Context) error
	Close() error
	Counters() packet.Counters
}

// queueStatsReader reads kernel NFQUEUE stats for queue 0. It returns an
// error when the kernel does not expose queue stats (no procfs entry).
type queueStatsReader func() (packet.QueueStats, error)

// queueStatsPath is the kernel per-queue stats file. It is namespace-
// scoped: when the daemon runs inside the lab router namespace, it
// reports that namespace's queue stats.
const queueStatsPath = "/proc/net/netfilter/nfnetlink_queue"

// watchQueueOverflow polls the kernel queue stats and records overflow
// (packets the kernel dropped before they reached the daemon) as
// bounded metrics and rate-limited warning logs. It stops when ctx is
// cancelled. It is best-effort: if the kernel does not support the
// proc file, it logs once at debug level and stops.
func (d *Daemon) watchQueueOverflow(ctx context.Context, read queueStatsReader) {
	prev, err := read()
	if err != nil {
		d.log.Debug("queue stats unavailable; overflow monitoring disabled", "error", err)
		return
	}
	var lastLog time.Time
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		cur, err := read()
		if err != nil {
			d.log.Debug("queue stats read failed; overflow monitoring suspended", "error", err)
			return
		}
		kernelDrop := delta32(cur.Dropped, prev.Dropped)
		userDrop := delta32(cur.UserDropped, prev.UserDropped)
		prev = cur
		if kernelDrop > 0 {
			d.metrics.ObserveQueueOverflow(metrics.OverflowKernelDrop, uint64(kernelDrop))
		}
		if userDrop > 0 {
			d.metrics.ObserveQueueOverflow(metrics.OverflowUserDrop, uint64(userDrop))
		}
		if (kernelDrop > 0 || userDrop > 0) && time.Since(lastLog) >= 10*time.Second {
			lastLog = time.Now()
			d.log.Warn("kernel queue dropped packets before userspace (fail-closed)",
				"kernel_dropped", kernelDrop,
				"user_dropped", userDrop,
				"queue_total", cur.Total,
				"queue_maxlen", d.opts.QueueMaxlen,
			)
		}
	}
}

// watchFlowOffload re-runs flow-offload detection periodically so a
// firewall reload that enables or disables offloading is reflected in
// the metric and the warning without a daemon restart. Best-effort.
//
// Under offload_policy=readiness, readiness tracks the enforcement gap
// in both directions: it drops when offloading appears, and recovers
// once the ruleset is clean again (the gap is gone). Readiness is never
// raised during shutdown (ctx cancelled).
func (d *Daemon) watchFlowOffload(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		rep := d.detectFlowOffload()
		d.metrics.SetFlowOffloadActive(rep.Active)
		if rep.Active {
			d.log.Warn("flow offloading detected: established flows bypass goshisen queue (enforcement gap)",
				"detail", rep.Detail)
			if d.opts.OffloadPolicy == config.OffloadPolicyReadiness {
				d.ready.Store(false)
			}
		} else if d.opts.OffloadPolicy == config.OffloadPolicyReadiness && ctx.Err() == nil {
			d.ready.Store(true)
		}
	}
}

// delta32 returns the positive difference between two unsigned counters.
// The kernel counters are 32-bit; under sustained flooding they can wrap.
// Because we poll every second, a genuine wrap yields a small delta,
// while a queue reset (re-bind/restart) resets the counter to 0 and
// would otherwise look like a huge "wrap". Deltas over half the counter
// range are treated as a reset (0 drops).
func delta32(cur, prev uint32) uint32 {
	if cur >= prev {
		return cur - prev
	}
	wrap := cur + (^uint32(0) - prev) + 1
	if wrap > 1<<31 {
		return 0
	}
	return wrap
}

// Daemon is the running process. It owns the logger, the policy store,
// and the packet backend lifecycle.
type Daemon struct {
	opts    *config.Options
	log     *slog.Logger
	king    *policy.King
	metrics *metrics.Registry
	store   *store.Store

	// ready reflects the enforcement path: false until the nftables hook
	// is installed AND the NFQUEUE is bound, and false again once
	// shutdown begins. Read by the API's /readyz handler.
	ready atomic.Bool

	// reloadMu serializes every policy mutation in the process: REST API
	// apply/activate and SIGHUP file reload. Without it, two writers
	// could compute the same next version and tear a publish (one
	// WritePolicy overwriting the other), leaving the durable active
	// pointer and the in-memory snapshot disagreeing. The API server is
	// handed this mutex when it is constructed (api.Options.Mu).
	reloadMu sync.Mutex

	// Seams (defaults set by New; overridable in tests).
	installHook func(hook.Ruleset) error
	openQueue   func(packet.Options) (Queue, error)
	// detectFlowOffload reports whether nftables flow offloading is
	// configured (established flows would bypass the queue). Overridable
	// in tests.
	detectFlowOffload func() hook.FlowOffloadReport
	// offloadInterval is the flow-offload re-check period (10 s in
	// production; shortened in tests).
	offloadInterval time.Duration
}

// New returns a Daemon ready to run, with logging configured from the
// resolved options, a fail-closed (empty, DROP) policy store, and the
// durable policy store opened. It performs no system changes.
func New(opts *config.Options) (*Daemon, error) {
	if opts == nil {
		return nil, errors.New("app: nil options")
	}
	st, err := store.Open(opts.PolicyDir)
	if err != nil {
		return nil, err
	}
	return &Daemon{
		opts:        opts,
		log:         newLogger(opts),
		king:        policy.NewKing(),
		metrics:     metrics.New(),
		store:       st,
		installHook: hook.Install,
		openQueue: func(o packet.Options) (Queue, error) {
			return packet.Open(o)
		},
		detectFlowOffload: hook.DetectFlowOffload,
		offloadInterval:   10 * time.Second,
	}, nil
}

// Metrics exposes the daemon registry (for tests and the API).
func (d *Daemon) Metrics() *metrics.Registry { return d.metrics }

// Store exposes the durable policy store.
func (d *Daemon) Store() *store.Store { return d.store }

// Logger exposes the daemon's logger.
func (d *Daemon) Logger() *slog.Logger { return d.log }

// Ready reports whether the enforcement path is fully up (hook installed
// and NFQUEUE bound). The API's /readyz handler reads this.
func (d *Daemon) Ready() bool { return d.ready.Load() }

// Run starts the daemon and blocks until a termination signal arrives or
// ctx is cancelled. In Phase 3 it installs the nftables hook, binds
// NFQUEUE, and hands every queued packet to the policy engine. Any
// failure before the loop is a startup error; during the loop, engine
// faults fail closed (DROP). Shutdown is orderly: stop receiving, close
// the queue, leave the hook in place (fail-closed by design).
func (d *Daemon) Run(ctx context.Context) error {
	d.log.Info("goshisen starting",
		"version", Version,
		"interface", d.opts.Interface,
		"policy_file", d.opts.PolicyFile,
		"config_sources", d.opts.SourceSummary(),
	)

	// Load the policy first; invalid policy means we will not touch
	// the system firewall at all. If the durable store has an active
	// policy (restart recovery), it wins; otherwise the configured file
	// is the initial policy.
	snap, err := d.loadPolicy()
	if err != nil {
		return fmt.Errorf("policy: %w", err)
	}
	if active, aerr := d.store.Active(); aerr == nil && active != "" {
		if data, rerr := d.store.ReadPolicy(active); rerr == nil {
			if snap2, cerr := policy.Compile(data); cerr == nil {
				snap = snap2
				d.log.Info("recovered active policy from store", "version", active)
				if v, perr := strconv.ParseInt(active, 10, 64); perr == nil {
					d.metrics.SetActivePolicyVersion(v)
				}
			}
		}
	}
	d.king.Replace(snap)
	d.log.Info("policy loaded", "rules", snap.Len(), "default_action", snap.DefaultAction())
	if active, aerr := d.store.Active(); aerr != nil || active == "" {
		if v, perr := strconv.ParseInt(strings.TrimSuffix(filepath.Base(d.opts.PolicyFile), ".yaml"), 10, 64); perr == nil {
			d.metrics.SetActivePolicyVersion(v)
		}
	}

	// Start the REST API (localhost bind) and metrics endpoint before the
	// privileged hook work: they do not require CAP_NET_ADMIN and their
	// readiness reflects enforcement state. If the hook fails, the API
	// still reports not-ready and the daemon exits.
	apiCtx, apiCancel := context.WithCancel(ctx)
	defer apiCancel()
	if d.opts.APIPort != 0 {
		srv, err := api.New(api.Options{
			Host:    d.opts.APIHost,
			Port:    d.opts.APIPort,
			Token:   d.opts.APIToken,
			Store:   d.store,
			King:    d.king,
			Metrics: d.metrics,
			Log:     d.log,
			Ready:   d.Ready,
			Mu:      &d.reloadMu,
		})
		if err != nil {
			return fmt.Errorf("api: %w", err)
		}
		go func() {
			if err := srv.Serve(apiCtx); err != nil {
				d.log.Warn("api server stopped", "error", err)
			}
		}()
		d.log.Info("REST API listening", "addr", srv.Addr())
	}
	if d.opts.MetricsPort != 0 && d.opts.MetricsPort != d.opts.APIPort {
		go d.serveMetrics(apiCtx, d.opts.MetricsPort)
	}

	scope := hook.ScopeForwardAll
	switch d.opts.HookScope {
	case config.HookScopeOpenWrt:
		scope = hook.ScopeOpenWrt
	}
	queueNum := d.opts.QueueNum

	// Flow-offload pre-check: offload_policy=refuse means the daemon
	// must not start enforcing when established flows would bypass the
	// queue. Fail-closed by absence: return before installing the hook
	// so the system is left exactly as it was.
	offload := d.detectFlowOffload()
	if offload.Active && d.opts.OffloadPolicy == config.OffloadPolicyRefuse {
		return fmt.Errorf("flow offloading is active but offload_policy=refuse: "+
			"established flows bypass the queue (enforcement gap); "+
			"disable flow offloading or set offload_policy=warn/readiness (%s)", offload.Detail)
	}

	rs := hook.Ruleset{
		Filter: hook.FilterConfig{QueueNum: queueNum},
		Nat:    d.opts.Nat,
		Scope:  scope,
	}
	if err := d.installHook(rs); err != nil {
		return err
	}
	d.log.Info("nftables hook installed", "table", hook.Table, "nat_enabled", d.opts.Nat.Enabled, "scope", scope)

	// Flow-offload detection: if offloading is configured, established
	// flows bypass NFQUEUE entirely. We never silently modify the
	// user's firewall; we flag it loudly and expose a health metric.
	if offload.Active {
		d.log.Warn("flow offloading detected: established flows bypass goshisen queue (enforcement gap)",
			"detail", offload.Detail)
		d.metrics.SetFlowOffloadActive(true)
	} else {
		d.metrics.SetFlowOffloadActive(false)
	}

	be, err := d.openQueue(packet.Options{Queue: queueNum, Maxlen: d.opts.QueueMaxlen})
	if err != nil {
		return fmt.Errorf("nfqueue: %w", err)
	}
	defer be.Close()
	d.log.Info("NFQUEUE bound", "queue", queueNum, "maxlen", d.opts.QueueMaxlen)

	// The enforcement path is now fully up: hook installed and queue
	// bound. Only now does readiness become true. With
	// offload_policy=readiness, readiness stays false while flow
	// offloading is active (enforcement gap present); the API keeps
	// reporting 503 and the metric carries the gauge.
	if offload.Active && d.opts.OffloadPolicy == config.OffloadPolicyReadiness {
		d.log.Error("offload_policy=readiness: flow offloading active, staying not-ready",
			"detail", offload.Detail)
	} else {
		d.ready.Store(true)
	}

	qCtx, qCancel := context.WithCancel(ctx)
	defer qCancel()
	go d.watchQueueOverflow(qCtx, func() (packet.QueueStats, error) {
		return packet.ReadQueueStats(queueStatsPath, queueNum)
	})
	go d.watchFlowOffload(qCtx, d.offloadInterval)

	// Surface backend errors (parse/panic/send) as metrics.
	if be2, ok := be.(*packet.Backend); ok {
		be2.SetErrorObserver(func(kind string) {
			switch kind {
			case "receive":
				d.metrics.ObserveBackendError(metrics.BackendErrReceive)
			case "send":
				d.metrics.ObserveBackendError(metrics.BackendErrSend)
			case "parse":
				d.metrics.ObserveBackendError(metrics.BackendErrParse)
			case "panic":
				d.metrics.ObserveBackendError(metrics.BackendErrPanic)
			}
		})
	}

	be.SetDecider(func(p policy.Packet) policy.Verdict {
		start := time.Now()
		v := d.king.Process(p)
		stateIdx := metrics.StateUnknownIdx
		switch p.State {
		case policy.StateNew:
			stateIdx = metrics.StateNewIdx
		case policy.StateEstablished:
			stateIdx = metrics.StateEstablishedIdx
		case policy.StateRelated:
			stateIdx = metrics.StateRelatedIdx
		case policy.StateInvalid:
			stateIdx = metrics.StateInvalidIdx
		}
		actionIdx := metrics.ActionDropIdx
		if v.Action == policy.ActionAccept {
			actionIdx = metrics.ActionAcceptIdx
		}
		protoIdx := metrics.ProtoAnyIdx
		switch p.Protocol {
		case policy.ProtoTCP:
			protoIdx = metrics.ProtoTCPIdx
		case policy.ProtoUDP:
			protoIdx = metrics.ProtoUDPIdx
		}
		d.metrics.ObservePacket(metrics.VerdictLabels{
			ActionIdx: actionIdx, ProtocolIdx: protoIdx, StateIdx: stateIdx,
		}, time.Since(start))
		return v
	})

	// SIGHUP reloads the policy file atomically. The new policy must
	// compile before it replaces the old one; on failure the old policy
	// stays active. Existing accepted connections keep flowing.
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP)
	defer signal.Stop(reload)

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	done := make(chan error, 1)
	go func() { done <- be.Run(ctx) }()

	// Readiness must be cleared on every exit path (including when the
	// backend completes first and the select below takes the done branch
	// instead of ctx.Done()). Otherwise a fast shutdown can leave ready
	// true after Run returns.
	defer d.ready.Store(false)

	for {
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("nfqueue: %w", err)
			}
			return nil
		case <-ctx.Done():
			d.log.Info("shutdown signal received, stopping queue")
			d.ready.Store(false)
			<-done
			c := be.Counters()
			d.log.Info("queue stopped; hook left in place (fail-closed)",
				"packets", c.PacketsSeen,
				"accepted", c.Accepted,
				"dropped", c.Dropped,
				"parse_errors", c.ParseErrors,
			)
			return nil
		case <-reload:
			// Compile → durable store → king swap. Any failure keeps the
			// previous policy (both store and live ruleset).
			d.reloadFromFile()
		}
	}
}

// serveMetrics exposes the Prometheus metrics on its own port.
func (d *Daemon) serveMetrics(ctx context.Context, port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		d.metrics.WritePrometheus(w)
	})
	srv := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		d.log.Warn("metrics server stopped", "error", err)
	}
}

// loadPolicy reads and compiles the configured policy file. The policy
// is applied only after it compiles cleanly.
func (d *Daemon) loadPolicy() (*policy.Snapshot, error) {
	data, err := os.ReadFile(d.opts.PolicyFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", d.opts.PolicyFile, err)
	}
	return policy.Compile(data)
}

// publish activates a compiled policy durably and atomically. The
// caller must have validated snap (Compile). The publish proceeds in a
// strict order:
//
//  1. serialize with the API's policy mutations (reloadMu),
//  2. compute the next version from the durable store,
//  3. write the policy file (immutable, fsync'd) and the active pointer
//     (fsync'd) — both or neither,
//  4. only after the store is durable, swap the in-memory snapshot
//     (king.Replace) and record the success.
//
// Any failure before step 4 leaves the store AND the live ruleset on
// the previous policy: the policy is never published half-applied. The
// previous policy is never lost. The returned version is the new active
// version, or "" when the publish was rejected.
//
// source names the reload trigger ("sighup" vs "api"); it appears only
// in logs, never in metrics (label cardinality stays bounded).
func (d *Daemon) publish(snap *policy.Snapshot, data []byte, source string) (string, error) {
	d.reloadMu.Lock()
	defer d.reloadMu.Unlock()

	next, err := d.store.NextVersion()
	if err != nil {
		d.metrics.ObservePolicyUpdate(metrics.PolicyUpdateFailure)
		return "", fmt.Errorf("policy publish: %w", err)
	}
	if err := d.store.WritePolicy(next, data); err != nil {
		d.metrics.ObservePolicyUpdate(metrics.PolicyUpdateFailure)
		return "", fmt.Errorf("policy publish: %w", err)
	}
	if err := d.store.SetActive(next); err != nil {
		d.metrics.ObservePolicyUpdate(metrics.PolicyUpdateFailure)
		return "", fmt.Errorf("policy publish: %w", err)
	}
	d.king.Replace(snap)
	if n, perr := strconv.ParseInt(next, 10, 64); perr == nil {
		d.metrics.SetActivePolicyVersion(n)
	}
	d.metrics.ObservePolicyUpdate(metrics.PolicyUpdateSuccess)
	d.log.Info("policy published", "version", next, "source", source,
		"rules", snap.Len(), "default_action", snap.DefaultAction().String())
	return next, nil
}

// reloadFromFile re-reads the configured policy file and publishes it.
// Invalid YAML, compile errors, or store failures keep the previous
// policy active (fail-closed: nothing is published half-applied).
func (d *Daemon) reloadFromFile() {
	d.log.Info("policy reload requested (SIGHUP)")
	data, err := os.ReadFile(d.opts.PolicyFile)
	if err != nil {
		d.log.Warn("policy reload rejected; keeping previous policy", "error", err)
		d.metrics.ObservePolicyUpdate(metrics.PolicyUpdateFailure)
		return
	}
	snap, err := policy.Compile(data)
	if err != nil {
		d.log.Warn("policy reload rejected; keeping previous policy", "error", err)
		d.metrics.ObservePolicyUpdate(metrics.PolicyUpdateFailure)
		return
	}
	if _, err := d.publish(snap, data, "sighup"); err != nil {
		d.log.Warn("policy reload rejected; keeping previous policy", "error", err)
	}
}

// newLogger builds the process logger per configuration.
func newLogger(opts *config.Options) *slog.Logger {
	return newLoggerTo(os.Stderr, opts)
}

// newLoggerTo builds a logger writing to w; the writer is injectable for
// tests.
func newLoggerTo(w io.Writer, opts *config.Options) *slog.Logger {
	if opts.LogJSON {
		return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: levelOf(opts.LogLevel)}))
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: levelOf(opts.LogLevel)}))
}

func levelOf(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// RunCLI is the CLI entry used by cmd/goshisen. It resolves the daemon
// options from all layers, performs read-only host validation, and runs
// the daemon until signal. It returns a clear, actionable error for any
// invalid configuration.
func RunCLI(configFile string, ov config.Overrides, env func(string) (string, bool)) error {
	opts, err := config.Load(configFile, env, ov)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	if err := opts.Check(); err != nil {
		return fmt.Errorf("host validation: %w", err)
	}
	d, err := New(opts)
	if err != nil {
		return err
	}
	return d.Run(context.Background())
}
