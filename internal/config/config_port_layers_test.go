package config

import (
	"testing"
)

// TestPortLayerMatrix is the full layer-combination contract for
// api_port / metrics_port: an explicit 0 disables the listener at every
// layer (file, env, flag), an unset port keeps the built-in loopback
// default (8080/9090), and the precedence is defaults < file < env <
// flags. It pins the exact resolved value AND the layer that supplied it
// (`config show` prints the same `Sources` values), covering the
// combinations measured in the OpenWrt lab on 2026-11-20 (see
// docs/release/listener-defaults-evidence-20261120.md).
//
// "empty" cases encode the documented empty-string semantics: an env
// var set to the empty string is treated as unset (the env layer skips
// it), while a YAML key present with no scalar (“api_port:“) is
// presence — an explicit 0 — and a YAML empty *string*
// (`api_port: ""`) is a type error.
func TestPortLayerMatrix(t *testing.T) {
	// Helper values for the flag layer.
	zero, flag9102, flag9099 := 0, 9102, 9099

	cases := []struct {
		name           string
		file           string            // "" = no config file at all
		env            map[string]string // nil = no env
		ov             Overrides
		wantAPI        int
		wantMetrics    int
		wantAPISource  string
		wantMetricsSrc string
	}{
		{
			name:           "defaults only",
			wantAPI:        8080,
			wantMetrics:    9090,
			wantAPISource:  "default",
			wantMetricsSrc: "default",
		},
		{
			name:           "file zeros disable",
			file:           "api_port: 0\nmetrics_port: 0\n",
			wantAPI:        0,
			wantMetrics:    0,
			wantAPISource:  "file",
			wantMetricsSrc: "file",
		},
		{
			name:           "file values win over defaults",
			file:           "api_port: 9100\nmetrics_port: 9091\n",
			wantAPI:        9100,
			wantMetrics:    9091,
			wantAPISource:  "file",
			wantMetricsSrc: "file",
		},
		{
			name:           "file api zero only",
			file:           "api_port: 0\n",
			wantAPI:        0,
			wantMetrics:    9090,
			wantAPISource:  "file",
			wantMetricsSrc: "default",
		},
		{
			name:           "file metrics zero only",
			file:           "metrics_port: 0\n",
			wantAPI:        8080,
			wantMetrics:    0,
			wantAPISource:  "default",
			wantMetricsSrc: "file",
		},
		{
			name:           "file empty scalar is an explicit zero",
			file:           "api_port:\nmetrics_port:\n",
			wantAPI:        0,
			wantMetrics:    0,
			wantAPISource:  "file",
			wantMetricsSrc: "file",
		},
		{
			name:           "env zeros disable",
			env:            map[string]string{"API_PORT": "0", "METRICS_PORT": "0"},
			wantAPI:        0,
			wantMetrics:    0,
			wantAPISource:  "env",
			wantMetricsSrc: "env",
		},
		{
			name:           "env wins over file values",
			file:           "api_port: 9100\nmetrics_port: 9091\n",
			env:            map[string]string{"API_PORT": "0", "METRICS_PORT": "9092"},
			wantAPI:        0,
			wantMetrics:    9092,
			wantAPISource:  "env",
			wantMetricsSrc: "env",
		},
		{
			name:           "env zero wins over file zero",
			file:           "api_port: 0\nmetrics_port: 0\n",
			env:            map[string]string{"API_PORT": "9101", "METRICS_PORT": "0"},
			wantAPI:        9101,
			wantMetrics:    0,
			wantAPISource:  "env",
			wantMetricsSrc: "env",
		},
		{
			name:           "empty env is unset",
			file:           "api_port: 9100\n",
			env:            map[string]string{"API_PORT": "", "METRICS_PORT": ""},
			wantAPI:        9100,
			wantMetrics:    9090,
			wantAPISource:  "file",
			wantMetricsSrc: "default",
		},
		{
			name:           "flag zeros disable",
			ov:             Overrides{APIPort: &zero, MetricsPort: &zero},
			wantAPI:        0,
			wantMetrics:    0,
			wantAPISource:  "flag",
			wantMetricsSrc: "flag",
		},
		{
			name:           "flag zero overrides a file value",
			file:           "api_port: 9100\nmetrics_port: 9091\n",
			ov:             Overrides{APIPort: &zero},
			wantAPI:        0,
			wantMetrics:    9091,
			wantAPISource:  "flag",
			wantMetricsSrc: "file",
		},
		{
			name:           "flag value overrides a file zero",
			file:           "api_port: 0\nmetrics_port: 0\n",
			ov:             Overrides{APIPort: &flag9102},
			wantAPI:        9102,
			wantMetrics:    0,
			wantAPISource:  "flag",
			wantMetricsSrc: "file",
		},
		{
			name:           "flag wins over env",
			env:            map[string]string{"API_PORT": "0", "METRICS_PORT": "9092"},
			ov:             Overrides{APIPort: &flag9099, MetricsPort: &zero},
			wantAPI:        9099,
			wantMetrics:    0,
			wantAPISource:  "flag",
			wantMetricsSrc: "flag",
		},
		{
			name:           "flag wins over file and env",
			file:           "api_port: 9100\nmetrics_port: 9091\n",
			env:            map[string]string{"API_PORT": "9101", "METRICS_PORT": "9092"},
			ov:             Overrides{APIPort: &flag9102, MetricsPort: &zero},
			wantAPI:        9102,
			wantMetrics:    0,
			wantAPISource:  "flag",
			wantMetricsSrc: "flag",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfgPath := ""
			if c.file != "" {
				cfgPath = writeTemp(t, "goshisen.yaml", "policy_file: /etc/goshisen/policy.yaml\n"+c.file)
			}
			vals := map[string]string{}
			for k, v := range c.env {
				vals[EnvPrefix+k] = v
			}
			o, err := Load(cfgPath, policyEnv(vals), c.ov)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if o.APIPort != c.wantAPI || o.MetricsPort != c.wantMetrics {
				t.Errorf("ports = %d/%d, want %d/%d", o.APIPort, o.MetricsPort, c.wantAPI, c.wantMetrics)
			}
			if got := o.Source("api_port"); got != c.wantAPISource {
				t.Errorf("api_port source = %q, want %q", got, c.wantAPISource)
			}
			if got := o.Source("metrics_port"); got != c.wantMetricsSrc {
				t.Errorf("metrics_port source = %q, want %q", got, c.wantMetricsSrc)
			}
		})
	}
}

// TestPortFileEmptyStringRejected documents the one YAML shape that is
// not "unset" and not "explicit 0": a quoted empty string is a type
// error, so a config with `api_port: ""` refuses to load (the daemon
// then exits before installing the hook — fail closed by absence).
func TestPortFileEmptyStringRejected(t *testing.T) {
	for _, body := range []string{"api_port: \"\"\n", "metrics_port: \"\"\n"} {
		p := writeTemp(t, "goshisen.yaml", "policy_file: /etc/goshisen/policy.yaml\n"+body)
		if _, err := Load(p, policyEnv(nil), Overrides{}); err == nil {
			t.Fatalf("Load accepted %q; want a type error", body)
		}
	}
}

// TestPortUCI RenderedConfigShape pins the exact YAML the OpenWrt init
// script renders for the packaged config: `api_port: 0` /
// `metrics_port: 0`, both present, so the resolved values are explicit
// zeros with source "file" (listeners disabled) and never the loopback
// defaults.
func TestPortUCIRenderedConfigShape(t *testing.T) {
	rendered := "policy_file: /etc/goshisen/policy.yaml\n" +
		"queue_num: 100\n" +
		"queue_maxlen: 1024\n" +
		"hook_scope: openwrt\n" +
		"log_level: info\n" +
		"offload_policy: warn\n" +
		"allow_bypass: false\n" +
		"interface: br-lan\n" +
		"api_port: 0\n" +
		"metrics_port: 0\n"
	p := writeTemp(t, "config.yaml", rendered)
	o, err := Load(p, policyEnv(nil), Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if o.APIPort != 0 || o.MetricsPort != 0 {
		t.Fatalf("resolved ports = %d/%d, want 0/0 (packaged UCI renders explicit zeros)", o.APIPort, o.MetricsPort)
	}
	if o.Source("api_port") != "file" || o.Source("metrics_port") != "file" {
		t.Errorf("sources = %q/%q, want file/file", o.Source("api_port"), o.Source("metrics_port"))
	}
}
