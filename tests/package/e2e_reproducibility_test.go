package package_test

// Privileged e2e preflight state-machine tests (Prompt 20).
//
// The runner's preflight is deterministic and injectable: each gate
// (CONFIG -> PRIVILEGE -> DEPENDENCY -> RESOURCE -> READY) can be
// exercised via E2E_MOCK_* without depending on the host's real tools
// (e.g. the CI build runner has no qemu). These tests verify the state
// tokens and exit code 3 for the whole environment class.
import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runRunnerPreflight(t *testing.T, env ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "e2e-runner.sh"),
		"--artifacts", t.TempDir())
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	rc := 0
	if ee, ok := err.(*exec.ExitError); ok {
		rc = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("runner: %v", err)
	}
	return string(out), rc
}

func wantState(t *testing.T, out string, state string) {
	t.Helper()
	if !strings.Contains(out, "[e2e-runner] "+state+":") {
		t.Fatalf("expected state %q in output:\n%s", state, out)
	}
}

// BLOCKED: no sudo + missing qemu must report BLOCKED (privilege gate
// precedes dependency gate), regardless of host tools.
func TestPreflightNoSudoBlockedBeforeMissingTool(t *testing.T) {
	out, rc := runRunnerPreflight(t, "E2E_MOCK_SUDO=no")
	if rc != 3 {
		t.Fatalf("rc = %d, want 3:\n%s", rc, out)
	}
	wantState(t, out, "BLOCKED")
	if strings.Contains(out, "MISSING_DEPENDENCY") {
		t.Fatalf("must not report MISSING_DEPENDENCY when BLOCKED takes precedence:\n%s", out)
	}
}

// MISSING_DEPENDENCY: privilege ok, one tool missing (mocked), and the
// rest of the dependencies are not required to be present on the host.
func TestPreflightMissingToolAfterPrivilege(t *testing.T) {
	out, rc := runRunnerPreflight(t,
		"E2E_MOCK_SUDO=yes",
		"E2E_MOCK_MISSING_TOOLS=qemu-system-x86_64")
	if rc != 3 {
		t.Fatalf("rc = %d, want 3:\n%s", rc, out)
	}
	wantState(t, out, "MISSING_DEPENDENCY")
	if !strings.Contains(out, "qemu-system-x86_64") {
		t.Fatalf("must name the missing tool:\n%s", out)
	}
}

// READY: all gates pass with mocks (no real qemu/sudo needed).
func TestPreflightReady(t *testing.T) {
	out, rc := runRunnerPreflight(t,
		"E2E_MOCK_SUDO=yes",
		"E2E_MOCK_MISSING_TOOLS=",
		"E2E_PREFLIGHT_ONLY=1")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0:\n%s", rc, out)
	}
	wantState(t, out, "READY")
}

// RESOURCE: occupied port is a distinct state.
func TestPreflightPortOccupied(t *testing.T) {
	out, rc := runRunnerPreflight(t,
		"E2E_MOCK_SUDO=yes",
		"E2E_MOCK_PORT_OCCUPIED=8088")
	if rc != 3 {
		t.Fatalf("rc = %d, want 3:\n%s", rc, out)
	}
	wantState(t, out, "RESOURCE")
	if !strings.Contains(out, "8088") {
		t.Fatalf("must name the port:\n%s", out)
	}
}

// CONFIG_ERROR: malformed configuration input.
func TestPreflightConfigError(t *testing.T) {
	out, rc := runRunnerPreflight(t, "E2E_MOCK_CONFIG=bad")
	if rc != 3 {
		t.Fatalf("rc = %d, want 3:\n%s", rc, out)
	}
	wantState(t, out, "CONFIG_ERROR")
}

// Diagnostics: the failing gate is the last state line and carries the
// reason (actionable); no stderr of a later check overwrites it.
func TestPreflightDiagnosticsStable(t *testing.T) {
	out, rc := runRunnerPreflight(t,
		"E2E_MOCK_SUDO=yes",
		"E2E_MOCK_MISSING_TOOLS=nft")
	if rc != 3 {
		t.Fatalf("rc = %d, want 3:\n%s", rc, out)
	}
	wantState(t, out, "MISSING_DEPENDENCY")
	if !strings.Contains(out, "nft") {
		t.Fatalf("must name nft:\n%s", out)
	}
	// exactly one terminal state line
	n := strings.Count(out, "[e2e-runner] MISSING_DEPENDENCY:")
	if n != 1 {
		t.Fatalf("expected exactly one MISSING_DEPENDENCY line, got %d:\n%s", n, out)
	}
}

func e2eScript(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "scripts", name)
}

// TestEnvManifestRuns captures key record lines (read-only, any host).
func TestEnvManifestRuns(t *testing.T) {
	cmd := exec.Command("bash", e2eScript(t, "e2e-env-manifest.sh"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("manifest failed: %v\n%s", err, out)
	}
	s := string(out)
	for _, want := range []string{
		"timestamp:", "repo_commit:", "kernel:", "arch:", "uid:",
		"sudo_present:", "nfqueue_proc:", "image_pin:",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("manifest missing %q:\n%s", want, s)
		}
	}
}

// TestEnvManifestJSON validates the --json shape (balanced braces + real quotes).
func TestEnvManifestJSON(t *testing.T) {
	cmd := exec.Command("bash", e2eScript(t, "e2e-env-manifest.sh"), "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("manifest --json failed: %v\n%s", err, out)
	}
	s := strings.TrimSpace(string(out))
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		t.Fatalf("json not wrapped: %s", s)
	}
	if !strings.Contains(s, `"kernel":`) || !strings.Contains(s, `"timestamp":`) {
		t.Fatalf("json missing keys:\n%s", s)
	}
}

// TestManifestCapturesPins verifies --print-config is surfaced (image +
// kmod pins), so the run is traceable to checksums.
func TestManifestCapturesPins(t *testing.T) {
	cmd := exec.Command("bash", e2eScript(t, "e2e-env-manifest.sh"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("manifest failed: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "image_pin:") || !strings.Contains(s, "kmod_pins:") {
		t.Fatalf("manifest missing pins:\n%s", s)
	}
}

// TestLabScriptsCleanupSafety asserts the lab scripts implement the
// idempotent/safe cleanup contract.
func TestLabScriptsCleanupSafety(t *testing.T) {
	b, err := os.ReadFile(e2eScript(t, "openwrt-lab.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"qemu-system-[x]86_64.*gsh-owrt0",
		`grep -q "gsh-owrt0" "/proc/$pid/cmdline"`,
		`ip netns del "$NS"`,
		`ip link del "$TAP"`,
		"iptables -D FORWARD",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("openwrt-lab.sh missing cleanup element %q", want)
		}
	}
}

// TestLabScriptSyntax validates all three scripts parse cleanly.
func TestLabScriptSyntax(t *testing.T) {
	for _, name := range []string{"openwrt-lab.sh", "e2e-runner.sh", "e2e-env-manifest.sh"} {
		cmd := exec.Command("bash", "-n", e2eScript(t, name))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bash -n %s: %v: %s", name, err, out)
		}
	}
}
