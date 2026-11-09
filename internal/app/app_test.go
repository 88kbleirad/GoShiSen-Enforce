package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
