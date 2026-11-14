package package_test

// Privileged e2e preflight state-machine tests (Prompt 21).
//
// The runner's preflight is a deterministic, typed state machine
// (CONFIG_ERROR -> BLOCKED -> MISSING_DEPENDENCY -> RESOURCE -> READY).
// Every gate consumes an injected environment view (see the environment
// abstraction in scripts/e2e-runner.sh), so these unit tests run against
// a hermetic fixture file: they never read the host's PATH, uid, kernel
// modules, /proc, or ports, and they never execute a real system tool.
// The suite is therefore identical on a machine with or without qemu,
// nft, sudo, or kernel modules.
import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fixtureAllPresent returns a fixture describing a machine where every
// dependency is satisfied (useful, passwordless sudo, all tools present,
// all kernel modules present, ports free). Tests mutate this baseline.
func fixtureAllPresent() string {
	return `tool curl: present
tool wget: present
tool python3: present
tool qemu-system-x86_64: present
tool nft: present
tool iptables: present
tool ip: present
tool sudo: present
uid: nonroot
sudo: yes
module nf_conntrack: present
nfqueue_proc: absent
port 5555: free
port 8088: free
`
}

// fixtureWith applies overrides (key -> value) on top of the all-present
// baseline and returns the path of a temporary fixture file.
func fixtureWith(t *testing.T, overrides map[string]string) string {
	t.Helper()
	content := fixtureAllPresent()
	for key, value := range overrides {
		re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `: .*$`)
		if !re.MatchString(content) {
			t.Fatalf("override key %q has no matching fixture line", key)
		}
		content = re.ReplaceAllString(content, key+": "+value)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "preflight.fixture")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runRunnerPreflight(t *testing.T, fixture string, envAndArgs ...string) (string, int) {
	t.Helper()
	args := []string{
		filepath.Join(repoRoot(t), "scripts", "e2e-runner.sh"),
		"--artifacts", t.TempDir(),
		"--fixture", fixture,
	}
	env := []string{}
	for _, e := range envAndArgs {
		if strings.Contains(e, "=") {
			env = append(env, e)
		} else {
			args = append(args, e)
		}
	}
	cmd := exec.Command("bash", args...)
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

var terminalStateRe = regexp.MustCompile(`\[e2e-runner\] (CONFIG_ERROR|BLOCKED|MISSING_DEPENDENCY|RESOURCE|READY):`)

func terminalStates(out string) []string {
	ms := terminalStateRe.FindAllStringSubmatch(out, -1)
	states := make([]string, 0, len(ms))
	for _, m := range ms {
		states = append(states, m[1])
	}
	return states
}

func wantState(t *testing.T, out string, state string) {
	t.Helper()
	if !strings.Contains(out, "[e2e-runner] "+state+":") {
		t.Fatalf("expected state %q in output:\n%s", state, out)
	}
}

func wantNotState(t *testing.T, out string, state string) {
	t.Helper()
	if strings.Contains(out, "[e2e-runner] "+state+":") {
		t.Fatalf("must not report %q:\n%s", state, out)
	}
}

// READY: a fully satisfied fixture yields READY / exit 0, regardless of
// what is (or is not) installed on the host.
func TestPreflightReady(t *testing.T) {
	out, rc := runRunnerPreflight(t, fixtureWith(t, nil), "E2E_PREFLIGHT_ONLY=1")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0:\n%s", rc, out)
	}
	wantState(t, out, "READY")
	if got := terminalStates(out); len(got) != 1 || got[0] != "READY" {
		t.Fatalf("terminal states = %v, want exactly [READY]:\n%s", got, out)
	}
}

// RESOURCE: occupied port is a distinct state even when every dependency
// is present (must not be shadowed by a MISSING_DEPENDENCY).
func TestPreflightPortOccupied(t *testing.T) {
	fixture := fixtureWith(t, map[string]string{"port 5555": "occupied"})
	out, rc := runRunnerPreflight(t, fixture)
	if rc != 3 {
		t.Fatalf("rc = %d, want 3:\n%s", rc, out)
	}
	wantState(t, out, "RESOURCE")
	wantNotState(t, out, "MISSING_DEPENDENCY")
	if !strings.Contains(out, "5555") {
		t.Fatalf("must name the port:\n%s", out)
	}
}

// Diagnostics: the failing gate is the single terminal state line and
// carries the dependency name plus an actionable remediation; no later
// check noise overwrites it.
func TestPreflightDiagnosticsStable(t *testing.T) {
	fixture := fixtureWith(t, map[string]string{"tool nft": "absent"})
	out, rc := runRunnerPreflight(t, fixture)
	if rc != 3 {
		t.Fatalf("rc = %d, want 3:\n%s", rc, out)
	}
	states := terminalStates(out)
	if len(states) != 1 || states[0] != "MISSING_DEPENDENCY" {
		t.Fatalf("terminal states = %v, want exactly [MISSING_DEPENDENCY]:\n%s", states, out)
	}
	if !strings.Contains(out, "nft") {
		t.Fatalf("must name nft:\n%s", out)
	}
	if !strings.Contains(out, "apt-get install") {
		t.Fatalf("must carry install remediation:\n%s", out)
	}
}

// BLOCKED: no sudo always wins over missing tools, so the runner reports
// the privilege problem and never the dependency one.
func TestRunnerPreflightBlockedNoSudo(t *testing.T) {
	fixture := fixtureWith(t, map[string]string{
		"sudo":                    "no",
		"tool qemu-system-x86_64": "absent",
	})
	out, rc := runRunnerPreflight(t, fixture)
	if rc != 3 {
		t.Fatalf("rc = %d, want 3:\n%s", rc, out)
	}
	wantState(t, out, "BLOCKED")
	wantNotState(t, out, "MISSING_DEPENDENCY")
	wantNotState(t, out, "READY")
}

// MISSING_DEPENDENCY: with privilege satisfied, an absent tool is named
// exactly and the rest of the environment does not matter.
func TestPreflightMissingToolAfterPrivilege(t *testing.T) {
	fixture := fixtureWith(t, map[string]string{"tool qemu-system-x86_64": "absent"})
	out, rc := runRunnerPreflight(t, fixture)
	if rc != 3 {
		t.Fatalf("rc = %d, want 3:\n%s", rc, out)
	}
	wantState(t, out, "MISSING_DEPENDENCY")
	if !strings.Contains(out, "qemu-system-x86_64") {
		t.Fatalf("must name the missing tool:\n%s", out)
	}
}

// CONFIG_ERROR: malformed configuration input.
func TestPreflightConfigError(t *testing.T) {
	out, rc := runRunnerPreflight(t, fixtureWith(t, nil), "--daemon", "")
	if rc != 3 {
		t.Fatalf("rc = %d, want 3:\n%s", rc, out)
	}
	wantState(t, out, "CONFIG_ERROR")
}

// TestPreflightStateMachineTable enumerates every state, its exit code,
// and the fixed gate-precedence order using fixtures with simultaneous
// faults.
func TestPreflightStateMachineTable(t *testing.T) {
	tests := []struct {
		name     string
		fixture  map[string]string
		env      []string
		args     []string
		want     string
		wantRC   int
		wantName string
		notWants []string
	}{
		{
			name:     "ready_all_present",
			fixture:  map[string]string{},
			env:      []string{"E2E_PREFLIGHT_ONLY=1"},
			want:     "READY",
			wantRC:   0,
			notWants: []string{"CONFIG_ERROR", "BLOCKED", "MISSING_DEPENDENCY", "RESOURCE"},
		},
		{
			name:     "blocked_no_sudo",
			fixture:  map[string]string{"sudo": "no"},
			want:     "BLOCKED",
			wantRC:   3,
			notWants: []string{"MISSING_DEPENDENCY", "RESOURCE", "READY"},
		},
		{
			name: "blocked_no_sudo_with_missing_tool",
			fixture: map[string]string{
				"sudo":                    "no",
				"tool qemu-system-x86_64": "absent",
			},
			want:     "BLOCKED",
			wantRC:   3,
			notWants: []string{"MISSING_DEPENDENCY", "READY"},
		},
		{
			name:     "missing_qemu",
			fixture:  map[string]string{"tool qemu-system-x86_64": "absent"},
			want:     "MISSING_DEPENDENCY",
			wantRC:   3,
			wantName: "qemu-system-x86_64",
			notWants: []string{"BLOCKED", "RESOURCE", "READY"},
		},
		{
			name:     "missing_nft",
			fixture:  map[string]string{"tool nft": "absent"},
			want:     "MISSING_DEPENDENCY",
			wantRC:   3,
			wantName: "nft",
			notWants: []string{"BLOCKED", "RESOURCE", "READY"},
		},
		{
			name:     "missing_module_nf_conntrack",
			fixture:  map[string]string{"module nf_conntrack": "absent"},
			want:     "MISSING_DEPENDENCY",
			wantRC:   3,
			wantName: "nf_conntrack",
			notWants: []string{"BLOCKED", "RESOURCE", "READY"},
		},
		{
			name:     "resource_port_5555",
			fixture:  map[string]string{"port 5555": "occupied"},
			want:     "RESOURCE",
			wantRC:   3,
			wantName: "5555",
			notWants: []string{"CONFIG_ERROR", "BLOCKED", "MISSING_DEPENDENCY", "READY"},
		},
		{
			name:     "resource_port_8088",
			fixture:  map[string]string{"port 8088": "occupied"},
			want:     "RESOURCE",
			wantRC:   3,
			wantName: "8088",
			notWants: []string{"MISSING_DEPENDENCY", "READY"},
		},
		{
			name:     "config_error_bad_args",
			fixture:  map[string]string{},
			args:     []string{"--daemon", ""},
			want:     "CONFIG_ERROR",
			wantRC:   3,
			notWants: []string{"BLOCKED", "MISSING_DEPENDENCY", "RESOURCE", "READY"},
		},
		{
			name: "precedence_config_first",
			fixture: map[string]string{
				"sudo":                    "no",
				"tool qemu-system-x86_64": "absent",
				"port 5555":               "occupied",
			},
			args:     []string{"--daemon", ""},
			want:     "CONFIG_ERROR",
			wantRC:   3,
			notWants: []string{"BLOCKED", "MISSING_DEPENDENCY", "RESOURCE", "READY"},
		},
		{
			name: "precedence_blocked_before_tool",
			fixture: map[string]string{
				"sudo":                    "no",
				"tool qemu-system-x86_64": "absent",
				"port 5555":               "occupied",
			},
			want:     "BLOCKED",
			wantRC:   3,
			notWants: []string{"CONFIG_ERROR", "MISSING_DEPENDENCY", "RESOURCE", "READY"},
		},
		{
			name: "precedence_tool_before_resource",
			fixture: map[string]string{
				"tool qemu-system-x86_64": "absent",
				"port 5555":               "occupied",
			},
			want:     "MISSING_DEPENDENCY",
			wantRC:   3,
			wantName: "qemu-system-x86_64",
			notWants: []string{"RESOURCE", "READY"},
		},
		{
			name: "precedence_module_before_resource",
			fixture: map[string]string{
				"module nf_conntrack": "absent",
				"port 5555":           "occupied",
			},
			want:     "MISSING_DEPENDENCY",
			wantRC:   3,
			wantName: "nf_conntrack",
			notWants: []string{"RESOURCE", "READY"},
		},
		{
			name: "precedence_tool_before_module",
			fixture: map[string]string{
				"tool qemu-system-x86_64": "absent",
				"module nf_conntrack":     "absent",
			},
			want:     "MISSING_DEPENDENCY",
			wantRC:   3,
			wantName: "qemu-system-x86_64",
			notWants: []string{"READY"},
		},
		{
			name:     "precedence_resource_last",
			fixture:  map[string]string{"port 5555": "occupied"},
			want:     "RESOURCE",
			wantRC:   3,
			wantName: "5555",
			notWants: []string{"MISSING_DEPENDENCY", "READY"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := fixtureWith(t, tc.fixture)
			out, rc := runRunnerPreflight(t, fixture, append(tc.env, tc.args...)...)
			if rc != tc.wantRC {
				t.Fatalf("rc = %d, want %d:\n%s", rc, tc.wantRC, out)
			}
			wantState(t, out, tc.want)
			if tc.wantName != "" && !strings.Contains(out, tc.wantName) {
				t.Fatalf("expected %q in output:\n%s", tc.wantName, out)
			}
			for _, not := range tc.notWants {
				wantNotState(t, out, not)
			}
		})
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
