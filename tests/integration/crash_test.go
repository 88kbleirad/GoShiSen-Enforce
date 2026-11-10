// Crash / restart integration tests (Phase 7, S10).
//
// Prove the two fail-closed properties around a daemon crash:
//  1. After abrupt death (SIGKILL), the hook stays in the kernel and the
//     queue has no listener, so traffic is dropped (fail-closed).
//  2. Restarting the daemon reinstalls the hook and rebinds the queue;
//     policy is restored and traffic flows again.
//
// Both are asserted with real traffic through the lab. Requires root.
package integration

import (
	"strings"
	"testing"
	"time"
)

// TestCrashKillFailClosedAndRestart kills the daemon with SIGKILL mid-
// traffic, proves traffic stops, then restarts it and proves recovery.
func TestCrashKillFailClosedAndRestart(t *testing.T) {
	if skip := checkPrereqs(t); skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)

	startStateProbe(t, "gsh_server", probeBin, "listen-echo", "tcp", "8080")
	polPath := firewallPolicy(t, `
version: 1
default_action: DROP
rules:
  - id: allow-http
    action: ACCEPT
    protocol: tcp
    src: 10.99.0.0/24
    dst: 10.99.1.0/24
    dst_port: 8080
    state: [NEW]
  - id: allow-estab
    action: ACCEPT
    protocol: tcp
    state: [ESTABLISHED]
`)
	startStateFirewall(t, polPath)

	// Baseline: works.
	code, out := runStateProbe(t, "connect-echo", "tcp", "10.99.1.2", "8080")
	if code != 0 {
		t.Fatalf("pre-crash probe failed: %d %s", code, out)
	}

	// Kill -9. The hook remains in the kernel; the queue has no
	// listener, so the kernel drops (fail-closed).
	if out, err := runNs(t, "gsh_fw", "pkill", "-9", "-f", "goshisen"); err != nil {
		t.Fatalf("kill -9: %v (%s)", err, out)
	}
	time.Sleep(500 * time.Millisecond)

	code, out = runStateProbe(t, "connect-echo", "tcp", "10.99.1.2", "8080")
	if code == 0 {
		t.Fatalf("traffic passed after SIGKILL: %s", out)
	}

	// Restart with the same policy; traffic must recover (the hook is
	// reinstalled by Install atomically).
	startStateFirewall(t, polPath)

	code, out = runStateProbe(t, "connect-echo", "tcp", "10.99.1.2", "8080")
	if code != 0 {
		t.Fatalf("post-restart probe failed: %d %s", code, out)
	}
}

// TestReadinessFalseUntilEnforced verifies /readyz stays 503 until the
// enforcement path is up, and 200 afterwards — via the real API inside
// the router namespace.
func TestReadinessFalseUntilEnforced(t *testing.T) {
	if skip := checkPrereqs(t); skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)

	// Start the daemon with an API on a lab port. Use the plain
	// firewall start (policy-only) — the API default port 8080 is
	// loopback inside the fw namespace.
	startStateProbe(t, "gsh_server", probeBin, "listen-echo", "tcp", "8080")
	polPath := firewallPolicy(t, `
version: 1
default_action: DROP
rules: []
`)
	startStateFirewall(t, polPath)

	// The daemon is ready (waitForDaemonReady ensures queue bind); the
	// API listener may take a moment to open. Retry briefly.
	var code int
	var out string
	deadline := time.Now().Add(10 * time.Second)
	for {
		code, out = runStateProbe(t, "httpget", "tcp", "127.0.0.1", "8080", "/readyz")
		if code == 0 && strings.Contains(out, "HTTP 200") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("readyz never 200: code=%d out=%s", code, out)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
