package package_test

// Privileged e2e reproducibility tests (Prompt 19).
//
// These exercise the environment manifest and the runner's fail-early
// classification WITHOUT requiring a privileged host: they run the
// manifests (read-only) and simulate the runner's preflight conditions
// via an isolated PATH/sudo shim. The full privileged run is covered by
// CI's openwrt-integration job and the local privileged lab.
import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

// TestEnvManifestJSON validates the --json shape (balanced braces).
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

// TestRunnerPreflightMissingTool: with an extra required tool that does
// not exist anywhere, the runner must exit 3 (ENV) with an actionable
// message — before any privileged work.
func TestRunnerPreflightMissingTool(t *testing.T) {
	cmd := exec.Command("bash", e2eScript(t, "e2e-runner.sh"),
		"--artifacts", t.TempDir())
	cmd.Env = append(os.Environ(),
		"E2E_REQUIRED_TOOLS=definitely-not-a-real-tool-xyz")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("runner succeeded with missing tool: %s", out)
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() != 3 {
		t.Fatalf("expected exit 3 (ENV), got %d: %s", ee.ExitCode(), out)
	}
	if !strings.Contains(string(out), "definitely-not-a-real-tool-xyz") {
		t.Fatalf("missing tool message must name the tool:\n%s", out)
	}
}

// TestRunnerPreflightBlockedNoSudo: with sudo shim failing, runner must
// exit 3 BLOCKED (no fake pass/skip).
func TestRunnerPreflightBlockedNoSudo(t *testing.T) {
	bin := t.TempDir()
	// Fake sudo that always fails (no passwordless).
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Provide required tools via symlink to real ones so preflight
	// passes tool presence, then fails on sudo.
	for _, tool := range []string{"curl", "wget", "python3", "qemu-system-x86_64", "nft", "iptables", "ip"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			continue
		}
		_ = os.Symlink(path, filepath.Join(bin, tool))
	}
	env := append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd := exec.Command("bash", e2eScript(t, "e2e-runner.sh"), "--artifacts", t.TempDir())
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("runner succeeded without passwordless sudo: %s", out)
	}
	if !strings.Contains(string(out), "BLOCKED") {
		t.Fatalf("expected BLOCKED message:\n%s", out)
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
	if !strings.Contains(s, "image_pin:") {
		t.Fatalf("manifest missing image_pin:\n%s", s)
	}
	if !strings.Contains(s, "kmod_pins:") {
		t.Fatalf("manifest missing kmod_pins:\n%s", s)
	}
}

// TestLabScriptsCleanupSafety asserts the lab scripts implement the
// idempotent/safe cleanup contract: `down` kills all lab QEMU (not just
// the pidfile), removes the netns/tap/bridge, and `up` guards against a
// stale-but-alive pidfile.
func TestLabScriptsCleanupSafety(t *testing.T) {
	b, err := os.ReadFile(e2eScript(t, "openwrt-lab.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"qemu-system-[x]86_64.*gsh-owrt0",          // kills lab qemu broadly (bracket-safe)
		`grep -q "gsh-owrt0" "/proc/$pid/cmdline"`, // stale-pidfile guard
		`ip netns del "$NS"`,
		`ip link del "$TAP"`,
		"iptables -D FORWARD",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("openwrt-lab.sh missing cleanup element %q", want)
		}
	}
}

// TestLabScriptSyntax validates both lab scripts parse cleanly.
func TestLabScriptSyntax(t *testing.T) {
	for _, name := range []string{"openwrt-lab.sh", "e2e-runner.sh", "e2e-env-manifest.sh"} {
		cmd := exec.Command("bash", "-n", e2eScript(t, name))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bash -n %s: %v: %s", name, err, out)
		}
	}
}
