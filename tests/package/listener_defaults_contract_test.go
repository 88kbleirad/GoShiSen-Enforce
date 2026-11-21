// Listener-default contract tests (issue: "default state of the HTTP
// listeners" docs contradiction).
//
// These pin the two facts the docs now state, so the documentation
// cannot drift again:
//
//  1. The daemon's built-in defaults (internal/config/config.go) are
//     API 127.0.0.1:8080 and metrics 127.0.0.1:9090; an explicit 0
//     in any layer (file/env/flag) disables the listener; an unset
//     port falls back to the default.
//  2. The OpenWrt package ships `option api_port '0'` /
//     `option metrics_port '0'` and its init script renders those
//     values verbatim into the daemon config. On v0.2.5/v0.2.6 (no
//     presence tracking) that renders `api_port: 0`, which the file
//     layer ignores -> the listeners default to 8080/9090 loopback.
//     This is exactly why the packaged config must keep both options
//     present and set to '0': with the presence-tracking daemon
//     (post-v0.2.6) that renders an explicit 0 and disables them, and
//     a config *without* the options would re-enable the loopback
//     defaults.
//
// The tests are hermetic: they assert against the repo's own source
// files (openwrt/files/etc/config/goshisen, openwrt/files/etc/init.d/
// goshisen, internal/config/config.go) and never require root, QEMU,
// or a network. The live VM evidence this pins is recorded in the PR
// description and in docs/CHANGELOG.md.
package package_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// readRepoFile reads a file relative to the repo root.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// TestPackagedOpenWrtConfigSetsExplicitZero asserts the shipped UCI
// config keeps both listener options present and set to '0' (works with
// the post-v0.2.6 presence-tracking daemon; without them the daemon
// would bind 8080/9090 loopback).
func TestPackagedOpenWrtConfigSetsExplicitZero(t *testing.T) {
	cfg := readRepoFile(t, "openwrt/files/etc/config/goshisen")
	for _, opt := range []string{"api_port", "metrics_port"} {
		if !strings.Contains(cfg, "option "+opt+" '0'") {
			t.Errorf("packaged config missing `option %s '0'`", opt)
		}
	}
}

// TestInitScriptRendersPortsVerbatim asserts the procd init script
// renders each UCI port option into the daemon YAML, so the packaged '0'
// values reach the daemon as an explicit `api_port: 0` /
// `metrics_port: 0`. The value may be rendered straight from UCI or
// from the variable validate_uci normalizes it into; either way it comes
// from the option, never from a hardcoded default.
func TestInitScriptRendersPortsVerbatim(t *testing.T) {
	init := readRepoFile(t, "openwrt/files/etc/init.d/goshisen")
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*echo "api_port: \$(\(config_get main api_port\)|API_PORT)"$`),
		regexp.MustCompile(`(?m)^\s*echo "metrics_port: \$(\(config_get main metrics_port\)|METRICS_PORT)"$`),
	} {
		if !want.MatchString(init) {
			t.Errorf("init script does not render the port from its UCI option (want %s)", want)
		}
	}
	// The daemon must be started with the rendered config file, not
	// with env or flags (so the UCI values are the only source).
	for _, want := range []string{`--config "$CONFIG_PATH"`} {
		if !strings.Contains(init, want) {
			t.Errorf("init script must pass %s", want)
		}
	}
}

// TestBinaryDefaultsMatchDocs asserts the daemon's built-in defaults are
// exactly what the docs, README and advisory state: API 127.0.0.1:8080,
// metrics 127.0.0.1:9090.
func TestBinaryDefaultsMatchDocs(t *testing.T) {
	cfg := readRepoFile(t, "internal/config/config.go")
	checks := map[string]string{
		"DefaultAPIHost":     `DefaultAPIHost       = "127.0.0.1"`,
		"DefaultAPIPort":     "DefaultAPIPort       = 8080",
		"DefaultMetricsPort": "DefaultMetricsPort   = 9090",
	}
	for name, want := range checks {
		if !strings.Contains(cfg, want) {
			t.Errorf("config.go %s default missing: %q", name, want)
		}
	}
	// The docs state the disable knobs verbatim; the config comments
	// must carry the same contract.
	for _, want := range []string{
		"GOSHISEN_API_PORT",
		"GOSHISEN_METRICS_PORT",
		"--api-port",
		"--metrics-port",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config.go comments should mention %q", want)
		}
	}
}
