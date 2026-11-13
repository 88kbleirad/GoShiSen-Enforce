// OpenWrt package surface tests: the procd init script and UCI config
// shipped under openwrt/files/.
//
// These run without root or an SDK: they validate the init script's
// shell syntax, the presence of the calls that make the service
// actually work (config_load before config_get, SIGHUP delivery via
// procd_send_signal), and that the UCI default interface is a kernel
// netdev name (not a firewall zone).
package package_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func openwrtFilesDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "openwrt", "files")
}

func readPackagedFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(openwrtFilesDir(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// TestOpenWrtInitScriptShellSyntax validates the script parses as a
// POSIX shell program (sh -n), so it cannot wedge the router's shell at
// boot if the package is installed.
func TestOpenWrtInitScriptShellSyntax(t *testing.T) {
	initd := filepath.Join(openwrtFilesDir(t), "etc", "init.d", "goshisen")
	cmd := exec.Command("sh", "-n", initd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sh -n %s: %v: %s", initd, err, out)
	}
}

// TestOpenWrtInitScriptLoadsUCI pins the fix where the init script must
// call config_load before any config_get: rc.common does not load UCI
// sections automatically, and without the load the rendered daemon
// config is empty (policy_file "", queue_num ""), so the daemon exits
// at startup. This was found on the phase 10.4 lab.
func TestOpenWrtInitScriptLoadsUCI(t *testing.T) {
	initd := readPackagedFile(t, filepath.Join("etc", "init.d", "goshisen"))
	if !strings.Contains(initd, "config_load goshisen") {
		t.Fatal("init script must run `config_load goshisen` before config_get")
	}
	// Validate the load happens before the first config_get.
	load := strings.Index(initd, "config_load goshisen")
	firstGet := strings.Index(initd, "config_get enabled main enabled")
	if load < 0 || firstGet < 0 || load > firstGet {
		t.Fatalf("config_load (idx %d) must precede first config_get (idx %d)", load, firstGet)
	}
}

// TestOpenWrtInitScriptSendsSignalPins the reload path: procd_kill
// deletes the service instance instead of signaling it. The reload must
// use procd_send_signal with the service basename (goshisen), so
// SIGHUP reaches the daemon and the policy reloads without restarting
// enforcement.
func TestOpenWrtInitScriptSendsSignal(t *testing.T) {
	initd := readPackagedFile(t, filepath.Join("etc", "init.d", "goshisen"))
	if !strings.Contains(initd, "procd_send_signal goshisen") {
		t.Fatal("reload_service must use procd_send_signal goshisen SIGHUP")
	}
	if strings.Contains(initd, "procd_kill") {
		t.Fatal("reload_service must not use procd_kill (it deletes the instance)")
	}
}

// TestOpenWrtInitScriptDefaultsDisabled pins that the packaged service
// refuses to start when UCI goshisen.main.enabled is not 1. The
// package installs disabled; an accidentally running daemon would
// enforce the default deny-all policy and cut off the router's
// forwarding without explicit user intent.
func TestOpenWrtInitScriptDefaultsDisabled(t *testing.T) {
	initd := readPackagedFile(t, filepath.Join("etc", "init.d", "goshisen"))
	if !strings.Contains(initd, "service_enabled") {
		t.Fatal("init script must gate start on service_enabled")
	}
	if !strings.Contains(initd, "[ \"$enabled\" = 1 ]") {
		t.Fatal("service_enabled must require enabled=1")
	}
	if !strings.Contains(initd, "service is disabled (goshisen.main.enabled=0)") {
		t.Fatal("disabled start must log and refuse")
	}
}

// TestOpenWrtUCIDefaultInterfaceIsNetdev pins that the packaged UCI
// default interface is a kernel device name (br-lan), not the fw4 zone
// name (lan): the daemon's host validation rejects zone names.
func TestOpenWrtUCIDefaultInterfaceIsNetdev(t *testing.T) {
	uci := readPackagedFile(t, filepath.Join("etc", "config", "goshisen"))
	if !strings.Contains(uci, "option interface 'br-lan'") {
		t.Fatalf("UCI default interface must be a netdev name, got:\n%s", uci)
	}
	if strings.Contains(uci, "option interface 'lan'") {
		t.Fatal("UCI default interface must not be the firewall zone name 'lan'")
	}
}

// TestOpenWrtUCIRendersOffloadPolicy pins that the UCI offload_policy
// option is validated and passed through to the rendered daemon YAML.
// Without the render, the OpenWrt package would silently ignore the
// opt-in flow-offload control the feature ships.
func TestOpenWrtUCIRendersOffloadPolicy(t *testing.T) {
	initd := readPackagedFile(t, filepath.Join("etc", "init.d", "goshisen"))
	uci := readPackagedFile(t, filepath.Join("etc", "config", "goshisen"))

	if !strings.Contains(uci, "option offload_policy 'warn'") {
		t.Fatal("UCI config must declare the offload_policy option (default warn)")
	}
	if !strings.Contains(initd, "config_get offload main offload_policy") {
		t.Fatal("init script must read offload_policy from UCI")
	}
	if !strings.Contains(initd, "warn|refuse|readiness") {
		t.Fatal("init script must accept warn|refuse|readiness for offload_policy")
	}
	if !strings.Contains(initd, `echo "offload_policy: $(config_get main offload_policy)"`) {
		t.Fatal("init script must render offload_policy into the daemon YAML")
	}
}
