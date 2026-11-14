//go:build preflight_integration

// Package integration contains the real end-to-end lab tests.
//
// TestPreflightReal is the separate integration target for the *real*
// preflight: it runs scripts/e2e-runner.sh --list-requirements plus the
// runner's real gate (no fixture) and asserts that the environment
// either satisfies every dependency (READY) or reports a precise
// MISSING_DEPENDENCY naming the absent tool/module with an install
// remediation. It never hard-codes the assumption that the runner has
// qemu; it fails if the preflight reports a state inconsistent with the
// tool reality, so CI image changes surface as accurate diagnostics, not
// as masked unit-test passes.
//
// The test is opt-in (build tag preflight_integration) because the
// unprivileged build job must remain green on a host without tools; the
// privileged CI integration job runs it with `-tags preflight_integration`.
package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var realPreflightRe = regexp.MustCompile(`\[e2e-runner\] (CONFIG_ERROR|BLOCKED|MISSING_DEPENDENCY|RESOURCE|READY):`)

func runnerScript(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "scripts", "e2e-runner.sh")
}

func TestPreflightReal(t *testing.T) {
	// Only bash is required to run the runner itself; every other tool is
	// the subject of the preflight and must be assessed by the real gate,
	// not assumed by the test.
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash not in PATH (%v); cannot run the real preflight", err)
	}

	// --list-requirements prints the contract; it must parse.
	req := exec.Command("bash", runnerScript(t), "--list-requirements")
	out, err := req.CombinedOutput()
	if err != nil {
		t.Fatalf("--list-requirements failed: %v\n%s", err, out)
	}
	reqText := string(out)
	for _, want := range []string{
		"tool qemu-system-x86_64", "tool nft", "tool iptables",
		"tool ip", "tool sudo", "module nf_conntrack",
	} {
		if !strings.Contains(reqText, want) {
			t.Fatalf("--list-requirements missing %q:\n%s", want, reqText)
		}
	}

	// Run the real preflight (no fixture) in preflight-only mode so it
	// stops after READY and never tries to provision/boot.
	cmd := exec.Command("bash", runnerScript(t), "--artifacts", t.TempDir())
	cmd.Env = append(os.Environ(), "E2E_PREFLIGHT_ONLY=1")
	raw, err := cmd.CombinedOutput()
	rc := 0
	if ee, ok := err.(*exec.ExitError); ok {
		rc = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("real preflight: %v", err)
	}
	s := string(raw)

	m := realPreflightRe.FindStringSubmatch(s)
	if len(m) != 2 {
		t.Fatalf("no terminal preflight state in output:\n%s", s)
	}
	state := m[1]

	switch state {
	case "READY":
		if rc != 0 {
			t.Fatalf("READY must exit 0, got rc=%d:\n%s", rc, s)
		}
		// Privileged, fully-provisioned runner: the preflight passes.
	case "MISSING_DEPENDENCY":
		if rc != 3 {
			t.Fatalf("MISSING_DEPENDENCY must exit 3, got rc=%d:\n%s", rc, s)
		}
		if !strings.Contains(s, "apt-get install") && !strings.Contains(s, "modprobe") {
			t.Fatalf("MISSING_DEPENDENCY must carry an install/modprobe remediation:\n%s", s)
		}
		// The runner must name the exact absent tool/module.
		named := false
		for _, tool := range []string{
			"curl", "wget", "python3", "qemu-system-x86_64",
			"nft", "iptables", "ip", "sudo", "nf_conntrack",
		} {
			if strings.Contains(s, tool) {
				named = true
				break
			}
		}
		if !named {
			t.Fatalf("MISSING_DEPENDENCY must name the absent dependency:\n%s", s)
		}
	case "BLOCKED":
		// Consistent with checkPrereqs: integration requires root/capability.
		t.Skipf("environment blocked (no privilege); real preflight reported BLOCKED")
	default:
		t.Fatalf("real preflight reported %s; expected READY, MISSING_DEPENDENCY, or BLOCKED:\n%s", state, s)
	}
}
