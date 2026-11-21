package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Fuzz targets for the config file layer. Each fuzzes one field (or a
// small group) through the real Load() pipeline so validation, the
// presence-tracking YAML decoder, and the path expander are all
// exercised. They came out of the v0.2.7 post-release self-test loop
// (see docs/release/proposal-v0.2.8-scope.md) and are kept so the config
// layer stays fuzzed in CI.
//
// Run one at a time — `-fuzz=FuzzConfig` matches several targets:
//   go test ./internal/config/ -fuzz='^FuzzConfigPorts$' -fuzztime=30s -run '^$'

// loadString writes the config text to a temp file and runs the real
// Load() pipeline (file layer + validation). Load must either succeed or
// return an error; it must never panic.
func loadString(t *testing.T, s string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _ = Load(p, func(string) (string, bool) { return "", false }, Overrides{})
}

// FuzzConfigQueueRanges fuzzes queue_num / queue_maxlen through the file
// layer (range checks 0..65534 and 1..65535, plus empty and non-numeric
// values).
func FuzzConfigQueueRanges(f *testing.F) {
	f.Add("100", "1024")
	f.Add("65534", "65535")
	f.Add("-1", "0")
	f.Add("65535", "65536")
	f.Add("abc", "xyz")
	f.Add("0", "1")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, queueNum, queueMaxlen string) {
		loadString(t, "queue_num: "+queueNum+"\nqueue_maxlen: "+queueMaxlen+"\npolicy_dir: /tmp/x\n")
	})
}

// FuzzConfigPorts fuzzes api_port / metrics_port parsing, the explicit
// zero semantics, and the api_port == metrics_port conflict check.
func FuzzConfigPorts(f *testing.F) {
	f.Add("0", "0")
	f.Add("8080", "9090")
	f.Add("-1", "65536")
	f.Add("abc", "")
	f.Add(" 5 ", "0")
	f.Add("99999999999999999999", "1")
	f.Fuzz(func(t *testing.T, apiPort, metricsPort string) {
		loadString(t, "api_port: "+apiPort+"\nmetrics_port: "+metricsPort+"\npolicy_dir: /tmp/x\n")
	})
}

// FuzzConfigMisc fuzzes policy_dir / interface / api_host handling
// (including the loopback guard and path expansion).
func FuzzConfigMisc(f *testing.F) {
	f.Add("/tmp", "eth0", "127.0.0.1")
	f.Add("", "", "")
	f.Add("/a/b/c", "br-lan", "::1")
	f.Add("~/x", "eth0", "localhost")
	f.Add("/tmp", "eth0", "0.0.0.0")
	f.Fuzz(func(t *testing.T, dir, iface, host string) {
		loadString(t, "policy_dir: "+dir+"\ninterface: "+iface+"\napi_host: "+host+"\n")
	})
}
