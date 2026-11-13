package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/config"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/hook"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/packet"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
)

func testOptions(t *testing.T) *config.Options {
	t.Helper()
	pol := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(pol, []byte("version: 1\nrules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &config.Options{
		File: config.File{
			Interface:  "lo",
			PolicyFile: pol,
			LogLevel:   "info",
			LogJSON:    false,
			PolicyDir:  t.TempDir(),
		},
		Sources: map[string]string{},
	}
}

func TestNewRejectsNilOptions(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) succeeded, want error")
	}
}

func TestLevelOf(t *testing.T) {
	cases := map[string]string{"debug": "DEBUG", "info": "INFO", "warn": "WARN", "error": "ERROR", "bogus": "INFO"}
	for in, want := range cases {
		if got := strings.ToUpper(levelOf(in).String()); got != want {
			t.Errorf("levelOf(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestLoggerTextAndJSON(t *testing.T) {
	opts := testOptions(t)

	var text bytes.Buffer
	newLoggerTo(&text, opts).Info("hello", "k", "v")
	if !strings.Contains(text.String(), "msg=hello") {
		t.Errorf("text log = %q, want msg=hello", text.String())
	}

	opts.LogJSON = true
	var js bytes.Buffer
	newLoggerTo(&js, opts).Info("hello", "k", "v")
	var decoded map[string]any
	if err := json.Unmarshal(js.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON log %q is not valid JSON: %v", js.String(), err)
	}
	if decoded["msg"] != "hello" || decoded["k"] != "v" {
		t.Errorf("JSON log = %v, want msg=hello k=v", decoded)
	}
}

func TestLoggerRespectsLevel(t *testing.T) {
	opts := testOptions(t)
	opts.LogLevel = "error"
	var buf bytes.Buffer
	newLoggerTo(&buf, opts).Info("suppressed")
	if buf.Len() != 0 {
		t.Errorf("info message emitted at error level: %q", buf.String())
	}
}

// fakeQueue is a minimal Queue implementation for lifecycle tests.
type fakeQueue struct {
	decider func(policy.Packet) policy.Verdict
	ran     chan struct{}
	closes  chan struct{}
}

func newFakeQueue() *fakeQueue {
	return &fakeQueue{ran: make(chan struct{}), closes: make(chan struct{})}
}

func (f *fakeQueue) SetDecider(d func(policy.Packet) policy.Verdict) { f.decider = d }
func (f *fakeQueue) Run(ctx context.Context) error {
	close(f.ran)
	<-ctx.Done()
	return nil
}
func (f *fakeQueue) Close() error { close(f.closes); return nil }
func (f *fakeQueue) Counters() packet.Counters {
	return packet.Counters{PacketsSeen: 7, Accepted: 2, Dropped: 5}
}

func TestRunLoadsPolicyBeforeHook(t *testing.T) {
	opts := testOptions(t)

	// Invalid policy: the daemon must refuse to start and never call
	// the hook installer.
	pol := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(pol, []byte("version: 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts.PolicyFile = pol

	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	hookCalled := false
	d.installHook = func(rs hook.Ruleset) error { hookCalled = true; return nil }
	err = d.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("Run = %v, want version error", err)
	}
	if hookCalled {
		t.Fatal("hook installed despite invalid policy")
	}
}

func TestRunGracefulShutdown(t *testing.T) {
	opts := testOptions(t)
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	fq := newFakeQueue()
	d.installHook = func(rs hook.Ruleset) error { return nil }
	d.openQueue = func(packet.Options) (Queue, error) { return fq, nil }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	select {
	case <-fq.ran:
	case <-time.After(2 * time.Second):
		t.Fatal("queue never started")
	}

	// Simulate SIGTERM via context; the daemon must stop the queue and
	// return cleanly.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit after cancel")
	}

	select {
	case <-fq.closes:
	default:
		t.Error("queue not closed")
	}
}

func TestRunQueueStartFailure(t *testing.T) {
	opts := testOptions(t)
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	d.installHook = func(rs hook.Ruleset) error { return nil }
	d.openQueue = func(packet.Options) (Queue, error) {
		return nil, os.ErrPermission
	}
	if err := d.Run(context.Background()); err == nil {
		t.Fatal("Run succeeded despite queue open failure")
	}
}

func TestDelta32Wrap(t *testing.T) {
	// prev near max, cur wrapped past zero: delta must still be the
	// real number of drops.
	if got := delta32(5, ^uint32(0)-10); got != 16 {
		t.Errorf("delta32(wrap) = %d, want 16", got)
	}
	if got := delta32(20, 10); got != 10 {
		t.Errorf("delta32(20,10) = %d, want 10", got)
	}
	if got := delta32(10, 20); got != 0 {
		t.Errorf("delta32(10,20) = %d, want 0 (counter went backwards)", got)
	}
	// A queue reset (counter back to 0) must not be read as a 4B wrap.
	if got := delta32(0, 100); got != 0 {
		t.Errorf("delta32(0,100) = %d, want 0 (reset)", got)
	}
}

// TestRunReadinessGatedOnQueueBind verifies /readyz semantics: false
// until the queue is bound, true only after, and false again once
// shutdown begins. This pins the fail-closed window — a daemon that is
// not yet enforcing must never report ready.
func TestRunReadinessGatedOnQueueBind(t *testing.T) {
	opts := testOptions(t)
	// Disable the API server for this test (readiness is exercised via
	// d.Ready()).
	opts.APIPort = 0
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}

	hookReady := make(chan struct{})
	queueOpen := make(chan struct{})
	d.installHook = func(rs hook.Ruleset) error {
		close(hookReady)
		<-queueOpen // hold the queue bind until the test says go
		return nil
	}
	fq := newFakeQueue()
	d.openQueue = func(packet.Options) (Queue, error) { return fq, nil }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	select {
	case <-hookReady:
	case <-time.After(2 * time.Second):
		t.Fatal("hook never installed")
	}
	// Hook installed, queue not yet open: must NOT be ready.
	if d.Ready() {
		t.Fatal("ready = true before queue bind")
	}
	close(queueOpen)

	select {
	case <-fq.ran:
	case <-time.After(2 * time.Second):
		t.Fatal("queue never started")
	}
	// Give Run a moment to set the ready flag.
	deadline := time.Now().Add(2 * time.Second)
	for !d.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !d.Ready() {
		t.Fatal("ready = false after queue bind")
	}

	// Shutdown must clear readiness immediately.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit after cancel")
	}
	if d.Ready() {
		t.Fatal("ready = true after shutdown")
	}
}

// TestRunReadinessFalseWhenQueueOpenFails verifies the daemon never
// reports ready when the queue cannot be bound.
func TestRunReadinessFalseWhenQueueOpenFails(t *testing.T) {
	opts := testOptions(t)
	opts.APIPort = 0
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	d.installHook = func(rs hook.Ruleset) error { return nil }
	d.openQueue = func(packet.Options) (Queue, error) {
		return nil, os.ErrPermission
	}
	if err := d.Run(context.Background()); err == nil {
		t.Fatal("Run succeeded despite queue open failure")
	}
	if d.Ready() {
		t.Fatal("ready = true despite queue open failure")
	}
}

// TestWatchQueueOverflow verifies the watcher converts kernel queue
// drop deltas into bounded metrics and rate-limited warnings, and that
// it stops on context cancellation.
func TestWatchQueueOverflow(t *testing.T) {
	opts := testOptions(t)
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	// Log to a buffer so we can assert warning content.
	var logBuf bytes.Buffer
	d.log = newLoggerTo(&logBuf, opts)

	stats := []packet.QueueStats{
		{QueueNum: 0, Total: 5, Dropped: 0, UserDropped: 0, Sequence: 100},
		{QueueNum: 0, Total: 5, Dropped: 10, UserDropped: 0, Sequence: 100},
		{QueueNum: 0, Total: 5, Dropped: 10, UserDropped: 3, Sequence: 100},
		{QueueNum: 0, Total: 5, Dropped: 12, UserDropped: 3, Sequence: 100},
	}
	idx := 0
	read := func() (packet.QueueStats, error) {
		if idx >= len(stats) {
			return packet.QueueStats{}, nil
		}
		s := stats[idx]
		idx++
		return s, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		d.watchQueueOverflow(ctx, read)
	}()

	// Poll the (mutex-guarded) registry until the expected overflow
	// counters appear; then stop the watcher.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var buf bytes.Buffer
		d.metrics.WritePrometheus(&buf)
		out := buf.String()
		if strings.Contains(out, `goshisen_queue_overflow_total{cause="kernel_drop"} 12`) &&
			strings.Contains(out, `goshisen_queue_overflow_total{cause="user_drop"} 3`) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-watched:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop after cancel")
	}

	var buf bytes.Buffer
	d.metrics.WritePrometheus(&buf)
	out := buf.String()
	if !strings.Contains(out, `goshisen_queue_overflow_total{cause="kernel_drop"} 12`) {
		t.Errorf("kernel_drop overflow counter wrong:\n%s", out)
	}
	if !strings.Contains(out, `goshisen_queue_overflow_total{cause="user_drop"} 3`) {
		t.Errorf("user_drop overflow counter wrong:\n%s", out)
	}
	if !strings.Contains(logBuf.String(), "kernel queue dropped packets before userspace") {
		t.Errorf("expected warning log, got:\n%s", logBuf.String())
	}
}

// TestWatchQueueOverflowUnavailable verifies that when the kernel does
// not expose queue stats (proc file missing), the watcher exits quietly
// without error.
func TestWatchQueueOverflowUnavailable(t *testing.T) {
	opts := testOptions(t)
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	read := func() (packet.QueueStats, error) {
		return packet.QueueStats{}, errors.New("no such file")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.watchQueueOverflow(ctx, read)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not exit when stats unavailable")
	}
}

// TestRunOffloadPolicyRefuse verifies that with offload_policy=refuse
// and flow offloading detected, Run fails BEFORE installing the hook
// (fail-closed by absence) and never binds the queue.
func TestRunOffloadPolicyRefuse(t *testing.T) {
	opts := testOptions(t)
	opts.APIPort = 0
	opts.OffloadPolicy = config.OffloadPolicyRefuse
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	hookCalled := false
	queueCalled := false
	d.detectFlowOffload = func() hook.FlowOffloadReport {
		return hook.FlowOffloadReport{Active: true, Detail: "flow add @ft"}
	}
	d.installHook = func(rs hook.Ruleset) error { hookCalled = true; return nil }
	d.openQueue = func(packet.Options) (Queue, error) { queueCalled = true; return newFakeQueue(), nil }
	err = d.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refuse") {
		t.Fatalf("Run = %v, want refuse error", err)
	}
	if hookCalled || queueCalled {
		t.Fatal("hook/queue touched despite offload_policy=refuse")
	}
	// The API must stay not-ready (Run returned before ever setting it).
	if d.Ready() {
		t.Fatal("ready = true despite refuse")
	}
}

// TestRunOffloadPolicyReadiness verifies that with offload_policy=readiness
// and flow offloading detected, the daemon runs and enforces but never
// becomes ready.
func TestRunOffloadPolicyReadiness(t *testing.T) {
	opts := testOptions(t)
	opts.APIPort = 0
	opts.OffloadPolicy = config.OffloadPolicyReadiness
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	d.detectFlowOffload = func() hook.FlowOffloadReport {
		return hook.FlowOffloadReport{Active: true, Detail: "flowtable f"}
	}
	hookCalled := false
	d.installHook = func(rs hook.Ruleset) error { hookCalled = true; return nil }
	fq := newFakeQueue()
	d.openQueue = func(packet.Options) (Queue, error) { return fq, nil }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case <-fq.ran:
	case <-time.After(3 * time.Second):
		t.Fatal("queue never started")
	}
	if !hookCalled {
		t.Fatal("hook not installed under readiness policy")
	}
	// Give Run a moment to (not) set ready.
	deadline := time.Now().Add(2 * time.Second)
	for !d.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if d.Ready() {
		t.Fatal("ready = true under offload_policy=readiness with offload active")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit")
	}
}

// TestRunOffloadPolicyReadinessRecovers verifies that under
// offload_policy=readiness the watcher drops readiness when offloading
// appears AND restores it when offloading is removed (enforcement gap
// gone). Without recovery, readiness would stay false forever.
func TestRunOffloadPolicyReadinessRecovers(t *testing.T) {
	opts := testOptions(t)
	opts.APIPort = 0
	opts.OffloadPolicy = config.OffloadPolicyReadiness
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	offload := atomic.Bool{}
	offload.Store(true)
	d.detectFlowOffload = func() hook.FlowOffloadReport {
		if offload.Load() {
			return hook.FlowOffloadReport{Active: true, Detail: "flowtable f"}
		}
		return hook.FlowOffloadReport{}
	}
	d.installHook = func(rs hook.Ruleset) error { return nil }
	fq := newFakeQueue()
	d.openQueue = func(packet.Options) (Queue, error) { return fq, nil }
	d.offloadInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case <-fq.ran:
	case <-time.After(3 * time.Second):
		t.Fatal("queue never started")
	}
	deadline := time.Now().Add(2 * time.Second)
	for d.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if d.Ready() {
		t.Fatal("ready = true under readiness policy while offload active")
	}

	// Offload clears; the watcher must restore readiness.
	offload.Store(false)
	deadline = time.Now().Add(5 * time.Second)
	for !d.Ready() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !d.Ready() {
		t.Fatal("ready stayed false after offload removed under readiness policy")
	}

	// Offload reappears; readiness drops again.
	offload.Store(true)
	deadline = time.Now().Add(5 * time.Second)
	for d.Ready() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if d.Ready() {
		t.Fatal("ready stayed true after offload reappeared under readiness policy")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit")
	}
}

// TestWatchFlowOffloadReadinessRecovery drives the watcher directly with
// a short interval to verify the ready-state transition on both edges.
func TestWatchFlowOffloadReadinessRecovery(t *testing.T) {
	opts := testOptions(t)
	opts.OffloadPolicy = config.OffloadPolicyReadiness
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	offload := atomic.Bool{}
	offload.Store(true)
	d.detectFlowOffload = func() hook.FlowOffloadReport {
		if offload.Load() {
			return hook.FlowOffloadReport{Active: true, Detail: "flow add @ft"}
		}
		return hook.FlowOffloadReport{}
	}
	d.ready.Store(true) // simulate enforcement already up

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.watchFlowOffload(ctx, 10*time.Millisecond)

	waitReady := func(want bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for d.Ready() != want && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if d.Ready() != want {
			t.Fatalf("ready = %v, want %v", d.Ready(), want)
		}
	}

	waitReady(false) // offload active → not ready
	offload.Store(false)
	waitReady(true) // offload cleared → ready
	cancel()
}

// TestRunOffloadPolicyWarnDefault verifies the default policy keeps the
// existing behavior: warn + metric, but readiness becomes true (no
// gating).
func TestRunOffloadPolicyWarnDefault(t *testing.T) {
	opts := testOptions(t)
	opts.APIPort = 0
	if opts.OffloadPolicy != "" && opts.OffloadPolicy != config.OffloadPolicyWarn {
		t.Fatalf("default offload_policy = %q, want %q or empty", opts.OffloadPolicy, config.OffloadPolicyWarn)
	}
	if opts.OffloadPolicy == "" {
		opts.OffloadPolicy = config.OffloadPolicyWarn
	}
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	d.detectFlowOffload = func() hook.FlowOffloadReport {
		return hook.FlowOffloadReport{Active: true, Detail: "flow add @ft"}
	}
	d.installHook = func(rs hook.Ruleset) error { return nil }
	fq := newFakeQueue()
	d.openQueue = func(packet.Options) (Queue, error) { return fq, nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case <-fq.ran:
	case <-time.After(3 * time.Second):
		t.Fatal("queue never started")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !d.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !d.Ready() {
		t.Fatal("ready = false under warn policy")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit")
	}
}
