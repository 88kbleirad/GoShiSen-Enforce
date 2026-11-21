// OpenWrt init-script port-contract tests (item 2: "0 disables at
// every layer" for the packaged UCI -> daemon path).
//
// These run in a sandboxed sh with a stubbed `config_get`, so they are
// hermetic (no root, no UCI daemon, no network). They pin the exact
// behaviour of openwrt/files/etc/init.d/goshisen:
//
//   - an absent UCI option renders `api_port: 0` (listener disabled)
//     with a warning naming the option;
//   - an empty UCI option renders `api_port: 0` with the same warning;
//   - `option api_port '0'` renders `api_port: 0` without warning;
//   - a literal port renders that number (leading zeros stripped);
//   - a non-numeric or out-of-range value aborts the start with a
//     message naming the option (fail-closed: no daemon starts);
//   - the daemon is always started with `--config /var/run/goshisen/config.yaml`.
package package_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initScriptPath returns the repo's init script path.
func initScriptPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "openwrt", "files", "etc", "init.d", "goshisen")
}

// renderPorts runs the init script's validate_uci + render_config with a
// stubbed config_get and returns the rendered YAML plus stderr.
// uci is a map of option name -> value ("" = option present but empty;
// a missing key = option absent).
func renderPorts(t *testing.T, uci map[string]string) (yaml, stderr string, rc int) {
	t.Helper()
	// The real policy file must exist: validate_uci checks it with [ -f ].
	policy := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(policy, []byte("version: 1\ndefault_action: DROP\nrules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uci["policy_file"] = policy
	script := initScriptPath(t)
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	// Extract the whole file minus the `#!/bin/sh /etc/rc.common` shebang
	// and the service_* / procd stubs we replace.
	cmd := exec.Command("bash")
	cmd.Stdin = strings.NewReader(`
config_load() { :; }
# OpenWrt's config_get has two forms:
#   config_get <variable> <section> <option>   (assigns the variable)
#   config_get <section> <option>              (echoes the value)
config_get() {
	local var="" sec opt val=""
	if [ "$#" -ge 3 ]; then
		var="$1"; sec="$2"; opt="$3"
	else
		sec="$1"; opt="$2"
	fi
	case "$sec:$opt" in
` + stubCases(uci) + `
	esac
	if [ -n "$var" ]; then
		eval "$var=\$val"
	else
		printf '%s\n' "$val"
	fi
}
oci_err() { echo "goshisen: $*" >&2; }
uci_warn() { echo "goshisen: $*" >&2; }
logger() { :; }
mkdir() { :; }
` + body + `
CONFIG_PATH="$TEST_CONFIG_PATH"
validate_uci
rc=$?
echo "VUCIRC=$rc"
if [ "$rc" -ne 0 ]; then
	exit "$rc"
fi
render_config
cat "$CONFIG_PATH"
`)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	uciCfg := filepath.Join(t.TempDir(), "goshisen-uci-config")
	cmd.Env = append(os.Environ(),
		"PATH=/usr/bin:/bin",
		"TEST_CONFIG_PATH="+cfgPath,
		"UCI_CONFIG_PATH="+uciCfg,
	)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	rc = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			rc = ee.ExitCode()
		} else {
			t.Fatalf("run init script helpers: %v", err)
		}
	}
	// Drop the harness's own status line; what remains is the rendered YAML.
	var yamlOut strings.Builder
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "VUCIRC=") {
			continue
		}
		yamlOut.WriteString(line)
		yamlOut.WriteString("\n")
	}
	return yamlOut.String(), errb.String(), rc
}

func stubCases(uci map[string]string) string {
	var b strings.Builder
	for opt, val := range uci {
		if val == "" {
			b.WriteString("\t\tmain:" + opt + ") val='' ;;\n")
		} else {
			b.WriteString("\t\tmain:" + opt + ") val='" + val + "' ;;\n")
		}
	}
	b.WriteString("\t\t*) val='' ;;\n")
	return b.String()
}

func TestInitRenderPorts(t *testing.T) {
	mk := func(extra ...string) map[string]string {
		m := map[string]string{
			"enabled": "1", "queue_num": "100", "queue_maxlen": "1024",
			"hook_scope": "openwrt", "log_level": "info",
			"offload_policy": "warn", "policy_file": "/etc/goshisen/policy.yaml",
			"interface": "br-lan",
		}
		for i := 0; i+1 < len(extra); i += 2 {
			m[extra[i]] = extra[i+1]
		}
		return m
	}
	cases := []struct {
		name        string
		uci         map[string]string
		wantAPI     string
		wantMetrics string
		wantLog     string // substring expected in stderr, "" = none
		wantRC      int
	}{
		{"packaged defaults", mk("api_port", "0", "metrics_port", "0"), "0", "0", "", 0},
		{"absent options -> zero + warning", mk(), "0", "0", "api_port is not set", 0},
		{"explicit empty -> zero + warning", mk("api_port", "", "metrics_port", ""), "0", "0", "api_port is not set", 0},
		{"explicit ports", mk("api_port", "8080", "metrics_port", "9090"), "8080", "9090", "", 0},
		{"leading zeros strip (octal trap)", mk("api_port", "08080", "metrics_port", "09090"), "8080", "9090", "", 0},
		{"one disabled one enabled", mk("api_port", "0", "metrics_port", "9090"), "0", "9090", "", 0},
		{"non-numeric aborts", mk("api_port", "abc"), "", "", "api_port 'abc' is not a port", 1},
		{"out-of-range aborts", mk("metrics_port", "65536"), "", "", "metrics_port '65536' is out of range", 1},
		{"negative aborts", mk("api_port", "-1"), "", "", "api_port '-1' is not a port", 1},
		{"metrics disabled only", mk("api_port", "8080", "metrics_port", "0"), "8080", "0", "", 0},
		{"whitespace value aborts", mk("api_port", " 8080"), "", "", "api_port ' 8080' is not a port", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			yaml, errb, rc := renderPorts(t, c.uci)
			if rc != c.wantRC {
				t.Errorf("rc = %d, want %d (stderr: %s)", rc, c.wantRC, errb)
			}
			if c.wantLog != "" && !strings.Contains(errb, c.wantLog) {
				t.Errorf("stderr missing %q:\n%s", c.wantLog, errb)
			}
			if c.wantRC != 0 {
				// A rejected value must not produce a daemon config at all.
				if strings.TrimSpace(yaml) != "" {
					t.Errorf("a rejected port still rendered a config:\n%s", yaml)
				}
				return
			}
			if !strings.Contains(yaml, "api_port: "+c.wantAPI+"\n") {
				t.Errorf("rendered YAML missing api_port: %s\n---\n%s", c.wantAPI, yaml)
			}
			if !strings.Contains(yaml, "metrics_port: "+c.wantMetrics+"\n") {
				t.Errorf("rendered YAML missing metrics_port: %s\n---\n%s", c.wantMetrics, yaml)
			}
		})
	}
}

// TestInitDiagnRenderedConfig pins the full command line the procd
// instance gets: /usr/sbin/goshisen --config /var/run/goshisen/config.yaml.
func TestInitDaemonCommandLine(t *testing.T) {
	data, err := os.ReadFile(initScriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		`procd_set_param command "$GOSHISEN_BIN"`,
		`procd_append_param command --config "$CONFIG_PATH"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("init script missing %s", want)
		}
	}
}

// TestInitRenderToken pins the UCI api_token path (issue #103):
//   - an absent token renders no api_token line and no warning (reads
//     stay open, mutations 401 — the daemon's documented behavior);
//   - a set token renders `api_token: <value>`;
//   - the rendered config is chmod 600 (never world-readable).
func TestInitRenderToken(t *testing.T) {
	mk := func(extra ...string) map[string]string {
		m := map[string]string{
			"enabled": "1", "queue_num": "100", "queue_maxlen": "1024",
			"hook_scope": "openwrt", "log_level": "info",
			"offload_policy": "warn", "policy_file": "/etc/goshisen/policy.yaml",
			"interface": "br-lan", "api_port": "8080",
			"metrics_port": "9090",
		}
		for i := 0; i+1 < len(extra); i += 2 {
			m[extra[i]] = extra[i+1]
		}
		return m
	}

	t.Run("absent token omitted from config", func(t *testing.T) {
		yaml, _, rc := renderPorts(t, mk())
		if rc != 0 {
			t.Fatalf("rc = %d, want 0", rc)
		}
		if strings.Contains(yaml, "api_token:") {
			t.Errorf("rendered config unexpectedly contains api_token:\n%s", yaml)
		}
	})

	t.Run("token rendered verbatim", func(t *testing.T) {
		yaml, _, rc := renderPorts(t, mk("api_token", "s3cret"))
		if rc != 0 {
			t.Fatalf("rc = %d, want 0", rc)
		}
		if !strings.Contains(yaml, "api_token: s3cret\n") {
			t.Errorf("rendered config missing api_token:\n%s", yaml)
		}
	})

	t.Run("rendered config is 0600", func(t *testing.T) {
		policy := filepath.Join(t.TempDir(), "policy.yaml")
		if err := os.WriteFile(policy, []byte("version: 1\ndefault_action: DROP\nrules: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfgPath := filepath.Join(t.TempDir(), "config.yaml")
		script := initScriptPath(t)
		data, err := os.ReadFile(script)
		if err != nil {
			t.Fatal(err)
		}
		body := string(data)
		cmd := exec.Command("bash")
		cmd.Stdin = strings.NewReader(`
config_load() { :; }
config_get() {
	local var="" sec opt val=""
	if [ "$#" -ge 3 ]; then
		var="$1"; sec="$2"; opt="$3"
	else
		sec="$1"; opt="$2"
	fi
	case "$sec:$opt" in
` + stubCases(mk("api_token", "s3cret", "policy_file", policy)) + `
	esac
	if [ -n "$var" ]; then
		eval "$var=\$val"
	else
		printf '%s\n' "$val"
	fi
}
oci_err() { echo "goshisen: $*" >&2; }
uci_warn() { echo "goshisen: $*" >&2; }
logger() { :; }
mkdir() { :; }
` + body + `
CONFIG_PATH="$TEST_CONFIG_PATH"
validate_uci
rc=$?
if [ "$rc" -ne 0 ]; then
	exit "$rc"
fi
render_config
`)
		cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin", "TEST_CONFIG_PATH="+cfgPath)
		var out, errb strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = &errb
		if err := cmd.Run(); err != nil {
			t.Fatalf("run init script: %v: %s", err, errb.String())
		}
		st, err := os.Stat(cfgPath)
		if err != nil {
			t.Fatalf("stat rendered config: %v", err)
		}
		if got := st.Mode().Perm(); got != 0o600 {
			t.Errorf("rendered config perms = %o, want 600", got)
		}
		content, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "api_token: s3cret\n") {
			t.Errorf("rendered config missing api_token:\n%s", content)
		}
	})
}

// TestInitRejectBadToken pins the fail-closed api_token validation: a
// token with whitespace/control characters must abort the start naming
// the option (it would corrupt the rendered YAML otherwise).
func TestInitRejectBadToken(t *testing.T) {
	mk := func(extra ...string) map[string]string {
		m := map[string]string{
			"enabled": "1", "queue_num": "100", "queue_maxlen": "1024",
			"hook_scope": "openwrt", "log_level": "info",
			"offload_policy": "warn", "policy_file": "/etc/goshisen/policy.yaml",
			"interface": "br-lan", "api_port": "8080",
			"metrics_port": "9090",
		}
		for i := 0; i+1 < len(extra); i += 2 {
			m[extra[i]] = extra[i+1]
		}
		return m
	}
	for _, tok := range []string{"has space", "colon:sep", "hash#tag", "quote\"q", "brace{", "semi;"} {
		yaml, errb, rc := renderPorts(t, mk("api_token", tok))
		if rc != 1 {
			t.Errorf("token %q: rc = %d, want 1 (yaml=%q err=%q)", tok, rc, yaml, errb)
		}
		if !strings.Contains(errb, "api_token") {
			t.Errorf("token %q: stderr does not name api_token: %q", tok, errb)
		}
		if strings.TrimSpace(yaml) != "" {
			t.Errorf("token %q: reject still rendered a config: %q", tok, yaml)
		}
	}
}

// TestInitRenderConfigModeAtCreation guards the umask-077 fix: the
// rendered config must be 600 from the moment it is created (not 644
// until a later chmod). Also asserts the init script restores the
// caller's umask afterwards.
func TestInitRenderConfigModeAtCreation(t *testing.T) {
	policy := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(policy, []byte("version: 1\ndefault_action: DROP\nrules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	script := initScriptPath(t)
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	cmd := exec.Command("bash")
	cmd.Stdin = strings.NewReader(`
config_load() { :; }
config_get() {
	local var="" sec opt val=""
	if [ "$#" -ge 3 ]; then
		var="$1"; sec="$2"; opt="$3"
	else
		sec="$1"; opt="$2"
	fi
	case "$sec:$opt" in
` + stubCases(map[string]string{
		"api_token": "s3cret", "policy_file": policy,
		"queue_num": "100", "hook_scope": "openwrt", "log_level": "info",
		"offload_policy": "warn", "interface": "br-lan",
		"api_port": "8080", "metrics_port": "9090",
	}) + `
	esac
	if [ -n "$var" ]; then
		eval "$var=\$val"
	else
		printf '%s\n' "$val"
	fi
}
oci_err() { echo "goshisen: $*" >&2; }
uci_warn() { echo "goshisen: $*" >&2; }
logger() { :; }
mkdir() { :; }
` + body + `
CONFIG_PATH="$TEST_CONFIG_PATH"
validate_uci
rc=$?
if [ "$rc" -ne 0 ]; then exit "$rc"; fi
owner_umask=$(umask)
render_config
restored_umask=$(umask)
echo "OWNER_UMASK=$owner_umask RESTORED_UMASK=$restored_umask"
`)
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin", "TEST_CONFIG_PATH="+cfgPath)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("run init script: %v: %s", err, errb.String())
	}
	owner, restored := "", ""
	for _, f := range strings.Fields(out.String()) {
		if v, ok := strings.CutPrefix(f, "OWNER_UMASK="); ok {
			owner = v
		}
		if v, ok := strings.CutPrefix(f, "RESTORED_UMASK="); ok {
			restored = v
		}
	}
	if owner == "" || restored != owner {
		t.Errorf("umask not restored (owner=%q restored=%q): output=%q", owner, restored, out.String())
	}
	// The file must exist with mode 600.
	st, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("rendered config perms = %o, want 600", got)
	}
}

// TestInitRestrictsUCIConfigMode pins the token-hygiene fix: when an
// api_token is set, the UCI config file (which holds the token in
// plaintext) is restricted to root. `uci commit` preserves the mode, so
// this stays effective after later uci writes; it is best-effort (chmod
// failure is ignored) and the rendered /var/run config stays 0600.
func TestInitRestrictsUCIConfigMode(t *testing.T) {
	policy := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(policy, []byte("version: 1\ndefault_action: DROP\nrules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")

	run := func(token string) (string, os.FileMode) {
		t.Helper()
		uciCfg := filepath.Join(t.TempDir(), "goshisen-uci")
		if err := os.WriteFile(uciCfg, []byte("config goshisen 'main'\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		script := initScriptPath(t)
		data, err := os.ReadFile(script)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("bash")
		cmd.Stdin = strings.NewReader(`
config_load() { :; }
config_get() {
	local var="" sec opt val=""
	if [ "$#" -ge 3 ]; then
		var="$1"; sec="$2"; opt="$3"
	else
		sec="$1"; opt="$2"
	fi
	case "$sec:$opt" in
` + stubCases(map[string]string{
			"api_token": token, "policy_file": policy,
			"queue_num": "100", "hook_scope": "openwrt", "log_level": "info",
			"offload_policy": "warn", "interface": "br-lan",
			"api_port": "8080", "metrics_port": "9090",
		}) + `
	esac
	if [ -n "$var" ]; then
		eval "$var=\$val"
	else
		printf '%s\n' "$val"
	fi
}
oci_err() { echo "goshisen: $*" >&2; }
uci_warn() { echo "goshisen: $*" >&2; }
logger() { :; }
mkdir() { :; }
` + string(data) + `
CONFIG_PATH="$TEST_CONFIG_PATH"
validate_uci
`)
		cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin",
			"TEST_CONFIG_PATH="+cfgPath, "UCI_CONFIG_PATH="+uciCfg)
		var out, errb strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = &errb
		if err := cmd.Run(); err != nil {
			t.Fatalf("run init script: %v: %s", err, errb.String())
		}
		st, err := os.Stat(uciCfg)
		if err != nil {
			t.Fatalf("stat uci config: %v", err)
		}
		return errb.String(), st.Mode().Perm()
	}

	// Token set → the UCI file is restricted to root.
	if _, mode := run("s3cret"); mode != 0o600 {
		t.Errorf("with token: UCI config mode = %o, want 600", mode)
	}
	// No token → mode is left alone (no secret to protect).
	if _, mode := run(""); mode != 0o644 {
		t.Errorf("without token: UCI config mode = %o, want 644 (untouched)", mode)
	}
}

// TestInitRendersD1OptIns pins the UCI -> YAML rendering of the D1
// hardening opt-ins: both are always rendered as explicit booleans
// (default false), and 1/on/true enables them.
func TestInitRendersD1OptIns(t *testing.T) {
	mk := func(extra ...string) map[string]string {
		m := map[string]string{
			"enabled": "1", "queue_num": "100", "queue_maxlen": "1024",
			"hook_scope": "openwrt", "log_level": "info",
			"offload_policy": "warn", "policy_file": "/etc/goshisen/policy.yaml",
			"interface": "br-lan", "api_port": "8080", "metrics_port": "9090",
		}
		for i := 0; i+1 < len(extra); i += 2 {
			m[extra[i]] = extra[i+1]
		}
		return m
	}
	cases := []struct {
		name                   string
		uci                    map[string]string
		wantReads, wantMetrics string
	}{
		{"absent -> explicit false", mk(), "false", "false"},
		{"0 -> false", mk("require_token_for_reads", "0", "metrics_token_required", "0"), "false", "false"},
		{"1 -> true", mk("require_token_for_reads", "1", "metrics_token_required", "1"), "true", "true"},
		{"on/true spellings", mk("require_token_for_reads", "on", "metrics_token_required", "true"), "true", "true"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			yaml, errb, rc := renderPorts(t, c.uci)
			if rc != 0 {
				t.Fatalf("rc = %d, want 0 (stderr %s)", rc, errb)
			}
			if want := "require_token_for_reads: " + c.wantReads + "\n"; !strings.Contains(yaml, want) {
				t.Errorf("rendered YAML missing %q:\n%s", want, yaml)
			}
			if want := "metrics_token_required: " + c.wantMetrics + "\n"; !strings.Contains(yaml, want) {
				t.Errorf("rendered YAML missing %q:\n%s", want, yaml)
			}
		})
	}
}
