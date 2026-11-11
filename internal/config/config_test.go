package config

import (
	"os"
	"path/filepath"
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
