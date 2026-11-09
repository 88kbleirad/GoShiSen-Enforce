// Queue overflow integration tests (Phase 7).
//
// These verify that when the kernel NFQUEUE queue fills up (or the
// userspace listener cannot keep up), the kernel drops the excess
// packets and the daemon observes + exports the overflow. The packets
// are dropped by the kernel — fail-closed by construction, never
// bypassed. Requires root (CAP_NET_ADMIN); otherwise skip.
package integration

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// readQueueStats reads the kernel per-queue stats inside the router
// namespace, where the daemon's queue lives.
func readQueueStats(t *testing.T) string {
	t.Helper()
	out, err := runNs(t, "gsh_fw", "cat", "/proc/net/netfilter/nfnetlink_queue")
	if err != nil {
		t.Fatalf("read queue stats: %v (%s)", err, out)
	}
	return out
}

// TestQueueOverflowKernelDrop forces a kernel-side queue overflow by
// stopping the daemon (SIGSTOP — it stays alive, its netlink socket
// stops draining) and flooding UDP. The kernel must drop the excess
// packets (queue_dropped grows) rather than accept them. Then the
// daemon resumes and its watcher must have recorded the overflow, and
// policy must still be enforced (fail-closed).
func TestQueueOverflowKernelDrop(t *testing.T) {
	if skip := checkPrereqs(t); skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)

	// Policy: allow TCP 8080 only; UDP is denied (default DROP). The
	// flood is UDP, so every queued packet would be DROPped by the
	// engine — but the queue overflows before the engine sees it.
	startStateProbe(t, "gsh_server", probeBin, "listen-echo", "tcp", "8080")
	fp := startFirewallFull(t, `
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
`, "--queue-maxlen", "4")

	// Sanity: the bound queue appears in the stats.
	before := readQueueStats(t)
	if !strings.Contains(before, "  0") {
		t.Fatalf("queue 0 not found in stats:\n%s", before)
	}

	// Stop the daemon WITHOUT killing it (SIGSTOP): its socket stops
	// draining, so the kernel queue fills and spills. SIGSTOP is a
	// test-only tool; a real crash (SIGKILL) is covered by the
	// crash/restart tests — here we need the process alive to later
	// observe its metric counters and logs.
	if out, err := runNs(t, "gsh_fw", "pkill", "-STOP", "-f", "goshisen"); err != nil {
		t.Fatalf("SIGSTOP daemon: %v (%s)", err, out)
	}
	time.Sleep(300 * time.Millisecond)

	// Flood: 2000 UDP datagrams to the server. With queue_maxlen=4
	// and no userspace draining, the kernel drops most of them. The
	// flood sender itself does not wait for replies and exits 0.
	code, out := runStateProbe(t, "flood", "udp", "10.99.1.2", "9999", "2000")
	if code != 0 {
		t.Fatalf("flood failed: %d %s", code, out)
	}
	time.Sleep(500 * time.Millisecond)

	// queue_dropped must have grown while the daemon was stopped.
	after := readQueueStats(t)
	if kernelDropped := parseQueueDropped(t, after); kernelDropped == 0 {
		t.Fatalf("queue_dropped did not increase after flood:\nbefore:\n%s\nafter:\n%s\n", before, after)
	}

	// Resume the daemon and give the watcher a tick to observe and log
	// the overflow.
	if out, err := runNs(t, "gsh_fw", "pkill", "-CONT", "-f", "goshisen"); err != nil {
		t.Fatalf("SIGCONT daemon: %v (%s)", err, out)
	}
	time.Sleep(1500 * time.Millisecond)

	// The daemon log must contain the overflow warning.
	logData, err := os.ReadFile(fp.logPath)
	if err != nil {
		t.Fatalf("read daemon log: %v", err)
	}
	if !strings.Contains(string(logData), "kernel queue dropped packets before userspace") {
		t.Errorf("daemon log missing overflow warning:\n%s", logData)
	}

	// Fail-closed proof: the firewall still enforces policy after the
	// overflow — allowed TCP works, denied UDP stays dropped.
	code, out = runStateProbe(t, "connect-echo", "tcp", "10.99.1.2", "8080")
	if code != 0 {
		t.Fatalf("allowed TCP after overflow failed: %d %s", code, out)
	}
	code, out = runStateProbe(t, "connect", "udp", "10.99.1.2", "9999")
	if code == 0 {
		t.Fatal("denied UDP after overflow unexpectedly succeeded")
	}
}

// parseQueueDropped extracts the queue_dropped field (seq_show field
// 6, index 5) for queue 0.
func parseQueueDropped(t *testing.T, stats string) int {
	t.Helper()
	for _, line := range strings.Split(stats, "\n") {
		f := strings.Fields(line)
		if len(f) < 8 || f[0] != "0" {
			continue
		}
		n := 0
		if _, err := fmt.Sscanf(f[5], "%d", &n); err != nil {
			t.Fatalf("parse queue_dropped %q: %v", f[5], err)
		}
		return n
	}
	return 0
}

var _ = time.Second
