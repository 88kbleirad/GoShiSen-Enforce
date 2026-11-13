package openwrt_integration

// Shell-level regression tests for the OpenWrt e2e reload gate
// (Prompt 09). These run the real pattern used by
// scripts/openwrt-e2e.sh through grep(1), so a change to the ERE that
// would silently break the gate (or make it too permissive) fails the
// ordinary CI "build, vet, test" job without needing the QEMU lab.

import (
	"os/exec"
	"strings"
	"testing"
)

// gateCount runs the exact grep invocation the e2e script performs
// inside the VM, against the supplied log text, and returns the count.
func gateCount(t *testing.T, want string, log string) int {
	t.Helper()
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skipf("grep not available: %v", err)
	}
	// Mirrors: grep -Ec 'policy (reloaded|published)" .*rules=$want([^0-9]|$)' log
	pattern := `policy (reloaded|published)" .*source=sighup .*rules=` + want + `([^0-9]|$)`
	cmd := exec.Command("grep", "-Ec", pattern)
	cmd.Stdin = strings.NewReader(log)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return 0 // grep: no match
		}
		t.Fatalf("grep failed: %v", err)
	}
	n := 0
	for _, c := range strings.TrimSpace(string(out)) {
		if c < '0' || c > '9' {
			t.Fatalf("unexpected grep -c output %q", out)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

const publishLogLine = `time=2026-11-13T10:18:07.134Z level=INFO msg="policy published" version=1 source=sighup rules=1 default_action=DROP`

func TestShellGatePublishLineMatches(t *testing.T) {
	if n := gateCount(t, "1", publishLogLine+"\n"); n != 1 {
		t.Fatalf("clean publish line must match once, got %d", n)
	}
}

func TestShellGateReloadRequestedDoesNotMatch(t *testing.T) {
	line := `time=2026-11-13T10:18:07.000Z level=INFO msg="policy reload requested (SIGHUP)"`
	if n := gateCount(t, "1", line+"\n"); n != 0 {
		t.Fatalf("reload-requested must not match, got %d", n)
	}
}

func TestShellGateRulesBoundary(t *testing.T) {
	if n := gateCount(t, "1", `msg="policy published" version=1 source=sighup rules=10`+"\n"); n != 0 {
		t.Fatalf("rules=10 must not satisfy rules=1, got %d", n)
	}
	if n := gateCount(t, "1", `msg="policy published" version=1 source=sighup rules=2 default_action=DROP`+"\n"); n != 0 {
		t.Fatalf("rules=2 must not satisfy rules=1, got %d", n)
	}
}

func TestShellGateGarbageBytesBeforeLine(t *testing.T) {
	// The CI transcript embedded invalid bytes (0xff 0xfe 0xfd) before the
	// log tail; the gate must still see the publish line.
	raw := "\xff\xfe\xfd\n" + publishLogLine + "\n"
	if n := gateCount(t, "1", raw); n != 1 {
		t.Fatalf("invalid bytes must not hide the publish line, got %d", n)
	}
}

func TestShellGateOldEventStillCounts(t *testing.T) {
	old := `time=2026-11-13T09:00:00Z level=INFO msg="policy published" version=1 source=sighup rules=1 default_action=DROP`
	if n := gateCount(t, "1", old+"\n"+publishLogLine+"\n"); n != 2 {
		t.Fatalf("both events must count (harness compares before/after), got %d", n)
	}
}
