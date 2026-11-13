package app

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/hook"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/metrics"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/packet"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
)

// TestReloadFromFileMalformedKeepsOld verifies the SIGHUP reload path is
// fail-closed: an unparsable or semantically invalid policy file leaves
// both the live ruleset (king) and the durable store on the previous
// policy, and records a failure metric.
func TestReloadFromFileMalformedKeepsOld(t *testing.T) {
	opts := testOptions(t)
	opts.APIPort = 0
	pol := opts.PolicyFile
	if err := os.WriteFile(pol, []byte("version: 1\nrules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	d.installHook = func(rs hook.Ruleset) error { return nil }
	// Initial publish (as Run would do at startup).
	snap, err := d.loadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pol)
	if err != nil {
		t.Fatal(err)
	}
	v, err := d.publish(snap, data, "test")
	if err != nil {
		t.Fatalf("initial publish: %v", err)
	}
	if v != "1" {
		t.Fatalf("initial version = %s, want 1", v)
	}

	// Malformed YAML.
	if err := os.WriteFile(pol, []byte("version: [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.reloadFromFile()
	if got := d.king.Snapshot().Len(); got != 0 {
		t.Errorf("king rules = %d, want 0 (old policy)", got)
	}
	active, _ := d.store.Active()
	if active != v {
		t.Errorf("store active = %s, want %s (old policy)", active, v)
	}
	assertUpdateMetric(t, d, metrics.PolicyUpdateFailure, 1)
	assertUpdateMetric(t, d, metrics.PolicyUpdateSuccess, 1)

	// Semantically invalid (bad state).
	if err := os.WriteFile(pol, []byte("version: 1\nrules:\n  - id: x\n    action: ACCEPT\n    state: [BOGUS]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.reloadFromFile()
	if got := d.king.Snapshot().Len(); got != 0 {
		t.Errorf("king rules = %d, want 0 (old policy)", got)
	}
	active, _ = d.store.Active()
	if active != v {
		t.Errorf("store active = %s, want %s (old policy)", active, v)
	}
	assertUpdateMetric(t, d, metrics.PolicyUpdateFailure, 2)
}

// TestReloadFromFileValidPublishes verifies a valid SIGHUP reload
// publishes a new durable version and swaps the king, with the version
// gauge and success metric updated.
func TestReloadFromFileValidPublishes(t *testing.T) {
	opts := testOptions(t)
	opts.APIPort = 0
	pol := opts.PolicyFile
	if err := os.WriteFile(pol, []byte("version: 1\nrules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := d.loadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(pol)
	if _, err := d.publish(snap, data, "test"); err != nil {
		t.Fatalf("initial publish: %v", err)
	}

	if err := os.WriteFile(pol, []byte("version: 1\nrules:\n  - id: a\n    action: ACCEPT\n    protocol: tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.reloadFromFile()
	if got := d.king.Snapshot().Len(); got != 1 {
		t.Errorf("king rules = %d, want 1", got)
	}
	active, _ := d.store.Active()
	if active != "2" {
		t.Errorf("store active = %s, want 2", active)
	}
	assertUpdateMetric(t, d, metrics.PolicyUpdateSuccess, 2)
}

// TestPublishPartialFailureKeepsOld verifies that a durable write
// failure aborts the publish before the king swap: the live ruleset and
// the store stay on the previous policy.
func TestPublishPartialFailureKeepsOld(t *testing.T) {
	opts := testOptions(t)
	opts.APIPort = 0
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := policy.Compile([]byte("version: 1\nrules: []\n"))
	if _, err := d.publish(snap, []byte("version: 1\nrules: []\n"), "test"); err != nil {
		t.Fatalf("initial publish: %v", err)
	}

	// Simulate a partial failure: pre-create the next version file so
	// WritePolicy fails on rename (immutable: refuses to overwrite).
	// WritePolicy uses O_EXCL temp + rename; to force a failure we make
	// the store directory read-only for the next write.
	storeDir := d.store.Dir()
	if err := os.Chmod(storeDir, 0o500); err != nil {
		t.Fatal(err)
	}
	// Restore perms at the end.
	defer os.Chmod(storeDir, 0o755)

	newSnap, _ := policy.Compile([]byte("version: 1\nrules:\n  - id: x\n    action: ACCEPT\n"))
	_, err = d.publish(newSnap, []byte("version: 1\nrules:\n  - id: x\n    action: ACCEPT\n"), "test")
	if err == nil {
		t.Fatal("publish succeeded despite read-only store")
	}
	if got := d.king.Snapshot().Len(); got != 0 {
		t.Errorf("king rules = %d, want 0 (old policy)", got)
	}
	active, _ := d.store.Active()
	if active != "1" {
		t.Errorf("store active = %s, want 1", active)
	}
	assertUpdateMetric(t, d, metrics.PolicyUpdateFailure, 1)
	assertUpdateMetric(t, d, metrics.PolicyUpdateSuccess, 1)
}

// TestPublishConcurrentSerializes verifies concurrent publishes never
// tear: each publish lands as a distinct durable version, and the store
// and king agree at the end. Without the shared mutex, two writers
// could compute the same version.
func TestPublishConcurrentSerializes(t *testing.T) {
	opts := testOptions(t)
	opts.APIPort = 0
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := policy.Compile([]byte("version: 1\nrules: []\n"))
	if _, err := d.publish(snap, []byte("version: 1\nrules: []\n"), "test"); err != nil {
		t.Fatalf("initial publish: %v", err)
	}

	const n = 10
	done := make(chan error, n)
	for i := range n {
		go func(i int) {
			pol := fmt.Sprintf("version: 1\nrules:\n  - id: r%d\n    action: ACCEPT\n", i)
			s, _ := policy.Compile([]byte(pol))
			_, err := d.publish(s, []byte(pol), "test")
			done <- err
		}(i)
	}
	for range n {
		if err := <-done; err != nil {
			t.Fatalf("concurrent publish: %v", err)
		}
	}
	active, _ := d.store.Active()
	if active != fmt.Sprintf("%d", n+1) {
		t.Errorf("store active = %s, want %d", active, n+1)
	}
	versions, err := d.store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != n+1 {
		t.Errorf("versions = %d, want %d", len(versions), n+1)
	}
	// King must reference the same active version's rules: the last
	// publish won; check the store's active policy compiles to the same
	// rule count as the king.
	data, err := d.store.ReadPolicy(active)
	if err != nil {
		t.Fatal(err)
	}
	asnap, err := policy.Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	if asnap.Len() != d.king.Snapshot().Len() {
		t.Errorf("king rules=%d != store active rules=%d", d.king.Snapshot().Len(), asnap.Len())
	}
	assertUpdateMetric(t, d, metrics.PolicyUpdateSuccess, int64(n+1))
}

// TestPublishPolicyIPv6 verifies IPv6 rules survive a publish and decide
// correctly in both families (the policy engine supports IPv6 prefixes;
// the reload path must not disturb that).
func TestPublishPolicyIPv6(t *testing.T) {
	opts := testOptions(t)
	opts.APIPort = 0
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	pol := `
version: 1
default_action: DROP
rules:
  - id: allow-v6
    action: ACCEPT
    protocol: tcp
    src: fd70:1::/64
    dst: fd70:2::2
    dst_port: 443
  - id: allow-v4
    action: ACCEPT
    protocol: tcp
    src: 10.0.0.0/8
    dst: 192.168.1.1
    dst_port: 80
`
	snap, err := policy.Compile([]byte(pol))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.publish(snap, []byte(pol), "test"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	v6 := policy.Packet{Protocol: policy.ProtoTCP, Src: mustIP("fd70:1::5"), Dst: mustIP("fd70:2::2"), SrcPort: 1000, DstPort: 443}
	v4 := policy.Packet{Protocol: policy.ProtoTCP, Src: mustIP("10.1.2.3"), Dst: mustIP("192.168.1.1"), SrcPort: 2000, DstPort: 80}
	v4Deny := policy.Packet{Protocol: policy.ProtoTCP, Src: mustIP("10.1.2.3"), Dst: mustIP("192.168.1.1"), SrcPort: 2000, DstPort: 81}
	if v := d.king.Process(v6); v.Action != policy.ActionAccept {
		t.Errorf("v6 verdict = %v, want ACCEPT", v.Action)
	}
	if v := d.king.Process(v4); v.Action != policy.ActionAccept {
		t.Errorf("v4 verdict = %v, want ACCEPT", v.Action)
	}
	if v := d.king.Process(v4Deny); v.Action != policy.ActionDrop {
		t.Errorf("v4 deny verdict = %v, want DROP", v.Action)
	}
}

// TestRunSIGHUPReload is the end-to-end reload test: it runs the daemon
// with a fake queue, rewrites the policy file, sends SIGHUP to the
// process, and verifies the store and king converge to the new policy
// while the daemon keeps running.
func TestRunSIGHUPReload(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short")
	}
	opts := testOptions(t)
	opts.APIPort = 0
	pol := opts.PolicyFile
	if err := os.WriteFile(pol, []byte("version: 1\nrules:\n  - id: a\n    action: ACCEPT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
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

	// Rewrite the policy and SIGHUP ourselves (the process's own signal
	// handler is registered inside Run; we must signal the test process,
	// not a child). The reloaded policy is observably different: it
	// accepts UDP instead of TCP.
	if err := os.WriteFile(pol, []byte("version: 1\nrules:\n  - id: b\n    action: ACCEPT\n    protocol: udp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		active, _ := d.store.Active()
		if active == "1" && d.king.Snapshot().Len() == 1 && d.king.Snapshot().Rules()[0].ID == "b" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	active, _ := d.store.Active()
	if active != "1" {
		t.Errorf("store active = %s, want 1 after SIGHUP", active)
	}
	rules := d.king.Snapshot().Rules()
	if len(rules) != 1 || rules[0].ID != "b" {
		t.Errorf("king rules = %v, want [b] after SIGHUP", rules)
	}
	// The new policy must actually be enforced.
	udp := policy.Packet{Protocol: policy.ProtoUDP, Src: mustIP("10.0.0.1"), Dst: mustIP("10.0.0.2"), SrcPort: 1000, DstPort: 53}
	tcp := policy.Packet{Protocol: policy.ProtoTCP, Src: mustIP("10.0.0.1"), Dst: mustIP("10.0.0.2"), SrcPort: 1000, DstPort: 80}
	if v := d.king.Process(udp); v.Action != policy.ActionAccept {
		t.Errorf("udp verdict = %v, want ACCEPT after SIGHUP", v.Action)
	}
	if v := d.king.Process(tcp); v.Action != policy.ActionDrop {
		t.Errorf("tcp verdict = %v, want DROP after SIGHUP", v.Action)
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

func assertUpdateMetric(t *testing.T, d *Daemon, outcome metrics.PolicyUpdateOutcome, want int64) {
	t.Helper()
	var buf bytes.Buffer
	d.metrics.WritePrometheus(&buf)
	line := fmt.Sprintf("goshisen_policy_updates_total{outcome=%q} %d", outcome, want)
	if !strings.Contains(buf.String(), line) {
		t.Errorf("metrics missing %s:\n%s", line, buf.String())
	}
}

func mustIP(s string) (ip netip.Addr) {
	ip = netip.MustParseAddr(s)
	return
}
