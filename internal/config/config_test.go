package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// envStub returns a fixed set of environment values.
func envStub(vals map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := vals[name]
		return v, ok
	}
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func strPtr(s string) *string { return &s }

func policyEnv(vals map[string]string) func(string) (string, bool) {
	if vals == nil {
		vals = map[string]string{}
	}
	vals[EnvPrefix+"POLICY_FILE"] = "/etc/goshisen/policy.yaml"
	return envStub(vals)
}

func TestLoadDefaults(t *testing.T) {
	o, err := Load("", policyEnv(nil), Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if o.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", o.LogLevel)
	}
	if o.LogJSON {
		t.Error("LogJSON = true, want false")
	}
	if o.QueueMaxlen != 1024 {
		t.Errorf("QueueMaxlen = %d, want 1024", o.QueueMaxlen)
	}
	if o.Source("log_level") != "default" {
		t.Errorf("log_level source = %q, want default", o.Source("log_level"))
	}
}

func TestLoadFileOverridesDefaults(t *testing.T) {
	p := writeTemp(t, "goshisen.yaml", `
interface: eth0
policy_file: /etc/goshisen/policy.yaml
log_level: debug
`)
	o, err := Load(p, envStub(nil), Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if o.Interface != "eth0" || o.PolicyFile != "/etc/goshisen/policy.yaml" || o.LogLevel != "debug" {
		t.Errorf("file layer not applied: %+v", o.File)
	}
	if o.Source("interface") != "file" {
		t.Errorf("interface source = %q, want file", o.Source("interface"))
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	p := writeTemp(t, "goshisen.yaml", `
interface: eth0
policy_file: /etc/goshisen/policy.yaml
log_level: warn
`)
	env := envStub(map[string]string{
		EnvPrefix + "INTERFACE":   "eth1",
		EnvPrefix + "POLICY_FILE": "/tmp/policy.yaml",
		EnvPrefix + "LOG_LEVEL":   "debug",
	})
	o, err := Load(p, env, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if o.Interface != "eth1" || o.PolicyFile != "/tmp/policy.yaml" || o.LogLevel != "debug" {
		t.Errorf("env layer not applied: %+v", o.File)
	}
	if o.Source("interface") != "env" {
		t.Errorf("interface source = %q, want env", o.Source("interface"))
	}
}

func TestLoadFlagsOverrideEverything(t *testing.T) {
	p := writeTemp(t, "goshisen.yaml", `
interface: eth0
policy_file: /etc/goshisen/policy.yaml
log_level: warn
`)
	lvl := "error"
	ov := Overrides{LogLevel: &lvl}
	o, err := Load(p, envStub(map[string]string{EnvPrefix + "LOG_LEVEL": "debug"}), ov)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if o.LogLevel != "error" {
		t.Errorf("LogLevel = %q, want error (flag wins)", o.LogLevel)
	}
	if o.Source("log_level") != "flag" {
		t.Errorf("log_level source = %q, want flag", o.Source("log_level"))
	}
}

func TestLoadLogJSONEnv(t *testing.T) {
	env := policyEnv(map[string]string{EnvPrefix + "LOG_JSON": "true"})
	o, err := Load("", env, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !o.LogJSON {
		t.Error("LogJSON = false, want true")
	}
	if o.Source("log_json") != "env" {
		t.Errorf("log_json source = %q, want env", o.Source("log_json"))
	}
	// Flag disables env-enabled JSON.
	noJSON := false
	ov := Overrides{LogJSON: &noJSON}
	o, err = Load("", env, ov)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if o.LogJSON {
		t.Error("LogJSON = true, want flag-disabled false")
	}
}

func TestLoadInvalidEnvValueErrors(t *testing.T) {
	env := policyEnv(map[string]string{EnvPrefix + "LOG_JSON": "maybe"})
	if _, err := Load("", env, Overrides{}); err == nil {
		t.Fatal("Load succeeded, want error for invalid LOG_JSON")
	} else if !strings.Contains(err.Error(), "LOG_JSON") {
		t.Errorf("error %q does not mention LOG_JSON", err)
	}
}

func TestLoadRejectsUnknownFileFields(t *testing.T) {
	p := writeTemp(t, "goshisen.yaml", "interface: eth0\nprocol: tcp\n")
	if _, err := Load(p, envStub(nil), Overrides{}); err == nil {
		t.Fatal("Load succeeded, want error for unknown field")
	}
}

func TestLoadRejectsMalformedYAML(t *testing.T) {
	p := writeTemp(t, "goshisen.yaml", "interface: [unclosed\n")
	if _, err := Load(p, envStub(nil), Overrides{}); err == nil {
		t.Fatal("Load succeeded, want error for malformed YAML")
	}
}

func TestLoadMissingFileErrors(t *testing.T) {
	if _, err := Load("/nonexistent/goshisen.yaml", envStub(nil), Overrides{}); err == nil {
		t.Fatal("Load succeeded, want error for missing config file")
	}
}

func TestValidateMissingPolicyFile(t *testing.T) {
	o, err := Load("", envStub(nil), Overrides{})
	if err == nil {
		t.Fatal("Load succeeded, want error for empty policy_file")
	}
	if o != nil {
		t.Errorf("Load returned options %+v alongside error", o)
	}
	if !strings.Contains(err.Error(), "policy_file") {
		t.Errorf("error %q does not mention policy_file", err)
	}
}

func TestValidateBadLogLevel(t *testing.T) {
	env := policyEnv(map[string]string{EnvPrefix + "LOG_LEVEL": "verbose"})
	if _, err := Load("", env, Overrides{}); err == nil {
		t.Fatal("Load succeeded, want error for bad log_level")
	}
}

func TestValidateBadInterface(t *testing.T) {
	env := policyEnv(map[string]string{EnvPrefix + "INTERFACE": "eth 0"})
	if _, err := Load("", env, Overrides{}); err == nil {
		t.Fatal("Load succeeded, want error for whitespace interface")
	}
}

func TestSourceSummaryStable(t *testing.T) {
	o, err := Load("", policyEnv(nil), Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sum := o.SourceSummary()
	for _, f := range []string{"interface", "policy_file", "log_level", "log_json", "queue_maxlen"} {
		if !strings.Contains(sum, f+"=") {
			t.Errorf("SourceSummary %q missing %s", sum, f)
		}
	}
}

func TestLoadQueueMaxlen(t *testing.T) {
	p := writeTemp(t, "goshisen.yaml", `
policy_file: /etc/goshisen/policy.yaml
queue_maxlen: 64
`)
	o, err := Load(p, envStub(nil), Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if o.QueueMaxlen != 64 {
		t.Errorf("QueueMaxlen = %d, want 64", o.QueueMaxlen)
	}
	if o.Source("queue_maxlen") != "file" {
		t.Errorf("queue_maxlen source = %q, want file", o.Source("queue_maxlen"))
	}

	env := policyEnv(map[string]string{EnvPrefix + "QUEUE_MAXLEN": "32"})
	o, err = Load("", env, Overrides{})
	if err != nil {
		t.Fatalf("Load env: %v", err)
	}
	if o.QueueMaxlen != 32 {
		t.Errorf("QueueMaxlen env = %d, want 32", o.QueueMaxlen)
	}

	q := 4
	o, err = Load("", policyEnv(nil), Overrides{QueueMaxlen: &q})
	if err != nil {
		t.Fatalf("Load flag: %v", err)
	}
	if o.QueueMaxlen != 4 {
		t.Errorf("QueueMaxlen flag = %d, want 4", o.QueueMaxlen)
	}
}

func TestValidateQueueMaxlen(t *testing.T) {
	for _, bad := range []int{0, -1, 65536} {
		o := &Options{File: File{PolicyFile: "/etc/goshisen/policy.yaml", PolicyDir: "x", QueueMaxlen: bad}}
		if err := o.validate(); err == nil || !strings.Contains(err.Error(), "queue_maxlen") {
			t.Errorf("validate queue_maxlen=%d: err=%v, want queue_maxlen error", bad, err)
		}
	}
}

func TestLoadQueueNumAndHookScope(t *testing.T) {
	p := writeTemp(t, "goshisen.yaml", `
interface: eth0
policy_file: /etc/goshisen/policy.yaml
queue_num: 100
hook_scope: openwrt
`)
	o, err := Load(p, envStub(nil), Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if o.QueueNum != 100 {
		t.Errorf("QueueNum = %d, want 100", o.QueueNum)
	}
	if o.HookScope != HookScopeOpenWrt {
		t.Errorf("HookScope = %q, want openwrt", o.HookScope)
	}
	if o.Source("queue_num") != "file" || o.Source("hook_scope") != "file" {
		t.Errorf("sources not set: queue_num=%q hook_scope=%q",
			o.Source("queue_num"), o.Source("hook_scope"))
	}
}

func TestLoadHookScopeEnv(t *testing.T) {
	o, err := Load("", envStub(map[string]string{
		"GOSHISEN_POLICY_FILE": "policies/examples/allow-lab.yaml",
		"GOSHISEN_QUEUE_NUM":   "7",
		"GOSHISEN_HOOK_SCOPE":  "openwrt",
	}), Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if o.QueueNum != 7 || o.HookScope != HookScopeOpenWrt {
		t.Errorf("env not applied: queue_num=%d hook_scope=%q", o.QueueNum, o.HookScope)
	}
}

func TestValidateHookScope(t *testing.T) {
	p := writeTemp(t, "goshisen.yaml", `
policy_file: /etc/goshisen/policy.yaml
hook_scope: bogus
`)
	if _, err := Load(p, envStub(nil), Overrides{}); err == nil {
		t.Fatal("Load accepted invalid hook_scope")
	}
	// openwrt + nat must be rejected (firewall4 owns NAT).
	p2 := writeTemp(t, "goshisen.yaml", `
policy_file: /etc/goshisen/policy.yaml
hook_scope: openwrt
nat:
  enabled: true
`)
	if _, err := Load(p2, envStub(nil), Overrides{}); err == nil {
		t.Fatal("Load accepted openwrt scope with NAT enabled")
	}
}

func TestValidateQueueNum(t *testing.T) {
	p := writeTemp(t, "goshisen.yaml", `
policy_file: /etc/goshisen/policy.yaml
queue_num: 70000
`)
	if _, err := Load(p, envStub(nil), Overrides{}); err == nil {
		t.Fatal("Load accepted queue_num out of range")
	}
}

// TestValidateQueueNumBoundaries: every accepted value must be usable by
// the hook (IPv4 queue N, IPv6 queue N+1, both u16) and every rejected
// value must make the daemon refuse to start (config.Load returns an
// error -> app.RunCLI aborts). Values are never wrapped, truncated, or
// defaulted: 65535 is rejected because the IPv6 rule would render
// "queue to 65536" (outside u16), 65536 and negative values are outside
// the valid range, and non-numeric input never parses.
func TestValidateQueueNumBoundaries(t *testing.T) {
	// Accepted: 0 (default) and 65534 (max usable; IPv6 rule -> 65535).
	for _, good := range []string{"0", "65534"} {
		p := writeTemp(t, "goshisen.yaml", fmt.Sprintf(`
policy_file: /etc/goshisen/policy.yaml
queue_num: %s
`, good))
		o, err := Load(p, envStub(nil), Overrides{})
		if err != nil {
			t.Fatalf("Load rejected queue_num %s: %v", good, err)
		}
		if o.QueueNum != mustParseInt(t, good) {
			t.Fatalf("QueueNum = %d, want %s", o.QueueNum, good)
		}
	}
	// Rejected: negative, reserved 65535, >u16 65536, far out of range,
	// and non-numeric text. Each must fail validation (fail closed).
	for _, bad := range []string{"-1", "65535", "65536", "70000", "abc"} {
		p := writeTemp(t, "goshisen.yaml", fmt.Sprintf(`
policy_file: /etc/goshisen/policy.yaml
queue_num: %s
`, bad))
		if _, err := Load(p, envStub(nil), Overrides{}); err == nil {
			t.Fatalf("Load accepted queue_num %s", bad)
		}
	}
	// Rejected via the environment layer (GOSHISEN_QUEUE_NUM): the same
	// fail-closed rule applies, never a wrap/truncate/default. (An empty
	// value is treated as unset by design — identical to every other env
	// var in this package — and falls back to the default 0.)
	for _, bad := range []string{"-1", "65535", "65536", "70000", "abc"} {
		env := envStub(map[string]string{"GOSHISEN_QUEUE_NUM": bad, "GOSHISEN_POLICY_FILE": "/etc/goshisen/policy.yaml"})
		if _, err := Load("", env, Overrides{}); err == nil {
			t.Fatalf("Load accepted env QUEUE_NUM=%q", bad)
		}
	}
	// Env round-trip: 0 and 65534 are accepted there too.
	for _, good := range []string{"0", "65534"} {
		env := envStub(map[string]string{"GOSHISEN_QUEUE_NUM": good, "GOSHISEN_POLICY_FILE": "/etc/goshisen/policy.yaml"})
		o, err := Load("", env, Overrides{})
		if err != nil {
			t.Fatalf("Load rejected env QUEUE_NUM=%s: %v", good, err)
		}
		if o.QueueNum != mustParseInt(t, good) {
			t.Fatalf("QueueNum = %d, want %s", o.QueueNum, good)
		}
	}
}

func mustParseInt(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("bad fixture %q: %v", s, err)
	}
	return n
}

func TestValidateOffloadPolicy(t *testing.T) {
	for _, bad := range []string{"bogus", "refuse-now"} {
		p := writeTemp(t, "goshisen.yaml", fmt.Sprintf(`
policy_file: /etc/goshisen/policy.yaml
offload_policy: %s
`, bad))
		if _, err := Load(p, envStub(nil), Overrides{}); err == nil {
			t.Fatalf("Load accepted invalid offload_policy %q", bad)
		}
	}
	for _, good := range []string{"warn", "refuse", "readiness", "enforce"} {
		p := writeTemp(t, "goshisen.yaml", fmt.Sprintf(`
policy_file: /etc/goshisen/policy.yaml
offload_policy: %s
`, good))
		if _, err := Load(p, envStub(nil), Overrides{}); err != nil {
			t.Fatalf("Load rejected valid offload_policy %q: %v", good, err)
		}
	}
	// allow_bypass is only valid with enforce.
	p := writeTemp(t, "goshisen.yaml", `
policy_file: /etc/goshisen/policy.yaml
offload_policy: warn
allow_bypass: true
`)
	if _, err := Load(p, envStub(nil), Overrides{}); err == nil {
		t.Fatal("Load accepted allow_bypass with offload_policy=warn")
	}
	good := writeTemp(t, "goshisen.yaml", `
policy_file: /etc/goshisen/policy.yaml
offload_policy: enforce
allow_bypass: true
`)
	if _, err := Load(good, envStub(nil), Overrides{}); err != nil {
		t.Fatalf("Load rejected allow_bypass with offload_policy=enforce: %v", err)
	}
}

func TestLoadNAT(t *testing.T) {
	p := writeTemp(t, "goshisen.yaml", `
policy_file: /etc/goshisen/policy.yaml
nat:
  enabled: true
  masquerade:
    enabled: true
    out_iface: eth1
  dnat:
    - name: web
      protocol: tcp
      pub_ip: 203.0.113.10
      pub_port: 80
      target_ip: 10.0.0.10
      target_port: 8080
`)
	o, err := Load(p, envStub(nil), Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !o.Nat.Enabled || !o.Nat.Masquerade.Enabled {
		t.Fatalf("nat not loaded: %+v", o.Nat)
	}
	if o.Nat.Masquerade.OutIface != "eth1" {
		t.Errorf("out_iface = %q", o.Nat.Masquerade.OutIface)
	}
	if len(o.Nat.Dnat) != 1 {
		t.Fatalf("dnat rules = %d", len(o.Nat.Dnat))
	}
	d := o.Nat.Dnat[0]
	if d.Name != "web" || d.Protocol != "tcp" || d.PublicPort != 80 || d.TargetPort != 8080 {
		t.Errorf("dnat rule = %+v", d)
	}
}

func TestLoadNATValidation(t *testing.T) {
	cases := map[string]string{
		"masq missing iface": `policy_file: /etc/goshisen/policy.yaml
nat:
  enabled: true
  masquerade:
    enabled: true
`,
		"bad dnat protocol": `policy_file: /etc/goshisen/policy.yaml
nat:
  enabled: true
  dnat:
    - name: w
      protocol: sctp
      pub_ip: 203.0.113.10
      pub_port: 80
      target_ip: 10.0.0.10
      target_port: 8080
`,
		"bad pub ip": `policy_file: /etc/goshisen/policy.yaml
nat:
  enabled: true
  dnat:
    - name: w
      protocol: tcp
      pub_ip: not-an-ip
      pub_port: 80
      target_ip: 10.0.0.10
      target_port: 8080
`,
		"ports out of range": `policy_file: /etc/goshisen/policy.yaml
nat:
  enabled: true
  dnat:
    - name: w
      protocol: tcp
      pub_ip: 203.0.113.10
      pub_port: 0
      target_ip: 10.0.0.10
      target_port: 8080
`,
		"duplicate dnat names": `policy_file: /etc/goshisen/policy.yaml
nat:
  enabled: true
  dnat:
    - name: w
      protocol: tcp
      pub_ip: 203.0.113.10
      pub_port: 80
      target_ip: 10.0.0.10
      target_port: 8080
    - name: w
      protocol: udp
      pub_ip: 203.0.113.10
      pub_port: 53
      target_ip: 10.0.0.10
      target_port: 53
`,
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := writeTemp(t, "cfg.yaml", tc)
			if _, err := Load(p, envStub(nil), Overrides{}); err == nil {
				t.Fatal("Load succeeded, want NAT validation error")
			}
		})
	}
}

func TestExpandPath(t *testing.T) {
	if got := ExpandPath("plain/path"); got != "plain/path" {
		t.Errorf("ExpandPath(plain) = %q", got)
	}
	home, _ := os.UserHomeDir()
	if got := ExpandPath("~/x.yaml"); got != home+"/x.yaml" {
		t.Errorf("ExpandPath(~/x.yaml) = %q, want %s/x.yaml", got, home)
	}
}

// TestLoadRejectsTrailingGarbagePortEnv locks the strictness of the
// API/METRICS port environment parsing: fmt.Sscanf's "%d" stops at the
// first non-digit, so GOSHISEN_API_PORT=8080x would silently bind 8080
// and GOSHISEN_METRICS_PORT=9090evil would bind 9090 instead of failing
// the daemon at startup.
func TestLoadRejectsTrailingGarbagePortEnv(t *testing.T) {
	for _, bad := range []string{"8080x", "9090evil", "12.5", "1e3"} {
		env := policyEnv(map[string]string{EnvPrefix + "API_PORT": bad})
		if _, err := Load("", env, Overrides{}); err == nil {
			t.Fatalf("Load accepted %sAPI_PORT=%q; want validation error", EnvPrefix, bad)
		}
		env = policyEnv(map[string]string{EnvPrefix + "METRICS_PORT": bad})
		if _, err := Load("", env, Overrides{}); err == nil {
			t.Fatalf("Load accepted %sMETRICS_PORT=%q; want validation error", EnvPrefix, bad)
		}
	}
	// Valid values must still load.
	env := policyEnv(map[string]string{
		EnvPrefix + "API_PORT":     "8080",
		EnvPrefix + "METRICS_PORT": "9090",
	})
	o, err := Load("", env, Overrides{})
	if err != nil {
		t.Fatalf("Load rejected valid ports: %v", err)
	}
	if o.APIPort != 8080 || o.MetricsPort != 9090 {
		t.Fatalf("ports = %d/%d, want 8080/9090", o.APIPort, o.MetricsPort)
	}
}

// TestValidateQueueNumIPv6Reserved: queue_num 65535 is accepted by the
// old range check but can never be installed — the hook always renders
// an IPv6 queue rule at queue_num+1, so 65535 would emit "queue to
// 65536" (outside the u16 queue-number range) and the nft install fails
// at every start. 65534 is the largest usable value.
func TestValidateQueueNumIPv6Reserved(t *testing.T) {
	p := writeTemp(t, "goshisen.yaml", `
policy_file: /etc/goshisen/policy.yaml
queue_num: 65535
`)
	if _, err := Load(p, envStub(nil), Overrides{}); err == nil {
		t.Fatal("Load accepted queue_num 65535; queue+1 IPv6 rule would overflow u16")
	}
	// 65534 is fine: the IPv6 rule becomes 65535.
	p = writeTemp(t, "goshisen.yaml", `
policy_file: /etc/goshisen/policy.yaml
queue_num: 65534
`)
	if _, err := Load(p, envStub(nil), Overrides{}); err != nil {
		t.Fatalf("Load rejected queue_num 65534: %v", err)
	}
}

// TestLoadFileZeroPortsDisable: an explicit `api_port: 0` /
// `metrics_port: 0` in the config file must disable the listener. The
// file layer used to skip zero values, so the shipped OpenWrt config
// (`option api_port '0'`) fell back to 8080/9090 loopback.
func TestLoadFileZeroPortsDisable(t *testing.T) {
	p := writeTemp(t, "goshisen.yaml", `
policy_file: /etc/goshisen/policy.yaml
api_port: 0
metrics_port: 0
`)
	o, err := Load(p, envStub(nil), Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if o.APIPort != 0 || o.MetricsPort != 0 {
		t.Fatalf("ports = %d/%d, want 0/0 (explicit 0 disables)", o.APIPort, o.MetricsPort)
	}
	if got := o.Source("api_port"); got != "file" {
		t.Errorf("api_port source = %q, want file", got)
	}
	if got := o.Source("metrics_port"); got != "file" {
		t.Errorf("metrics_port source = %q, want file", got)
	}
}

// TestLoadFilePortPresence: only the ports the file mentions are applied.
// An omitted port keeps the built-in default; an explicit non-zero port
// wins over it.
func TestLoadFilePortPresence(t *testing.T) {
	cases := []struct {
		name           string
		file           string
		wantAPI        int
		wantMetrics    int
		wantAPISource  string
		wantMetaSource string
	}{
		{"unset", "policy_file: /etc/goshisen/policy.yaml\n", 8080, 9090, "default", "default"},
		{"api zero only", "policy_file: /etc/goshisen/policy.yaml\napi_port: 0\n", 0, 9090, "file", "default"},
		{"metrics zero only", "policy_file: /etc/goshisen/policy.yaml\nmetrics_port: 0\n", 8080, 0, "default", "file"},
		{"explicit ports", "policy_file: /etc/goshisen/policy.yaml\napi_port: 9000\nmetrics_port: 9001\n", 9000, 9001, "file", "file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := writeTemp(t, "goshisen.yaml", c.file)
			o, err := Load(p, envStub(nil), Overrides{})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if o.APIPort != c.wantAPI || o.MetricsPort != c.wantMetrics {
				t.Fatalf("ports = %d/%d, want %d/%d", o.APIPort, o.MetricsPort, c.wantAPI, c.wantMetrics)
			}
			if got := o.Source("api_port"); got != c.wantAPISource {
				t.Errorf("api_port source = %q, want %q", got, c.wantAPISource)
			}
			if got := o.Source("metrics_port"); got != c.wantMetaSource {
				t.Errorf("metrics_port source = %q, want %q", got, c.wantMetaSource)
			}
		})
	}
}

// TestLoadFlagZeroPortDisables: --api-port 0 / --metrics-port 0 reach the
// config layer as explicit overrides and must disable, not fall back.
func TestLoadFlagZeroPortDisables(t *testing.T) {
	zero := 0
	o, err := Load("", policyEnv(nil), Overrides{APIPort: &zero, MetricsPort: &zero})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if o.APIPort != 0 || o.MetricsPort != 0 {
		t.Fatalf("ports = %d/%d, want 0/0", o.APIPort, o.MetricsPort)
	}
	if o.Source("api_port") != "flag" || o.Source("metrics_port") != "flag" {
		t.Errorf("sources = %q/%q, want flag/flag", o.Source("api_port"), o.Source("metrics_port"))
	}
	// A file value of 0 is overridden by a non-zero flag.
	p := writeTemp(t, "goshisen.yaml", "policy_file: /etc/goshisen/policy.yaml\napi_port: 0\n")
	port := 8081
	o, err = Load(p, envStub(nil), Overrides{APIPort: &port})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if o.APIPort != 8081 {
		t.Fatalf("APIPort = %d, want 8081 (flag beats file)", o.APIPort)
	}
}

// TestLoadFilePortRejectsNonScalar keeps the strict decode: api_port
// must be a scalar integer, not a list/mapping.
func TestLoadFilePortRejectsNonScalar(t *testing.T) {
	for _, body := range []string{"api_port: [8080]\n", "api_port:\n  port: 8080\n"} {
		p := writeTemp(t, "goshisen.yaml", "policy_file: /etc/goshisen/policy.yaml\n"+body)
		if _, err := Load(p, envStub(nil), Overrides{}); err == nil {
			t.Fatalf("Load accepted api_port %q; want error", strings.TrimSpace(body))
		}
	}
}
