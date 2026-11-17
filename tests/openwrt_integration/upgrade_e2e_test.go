// OpenWrt apk upgrade e2e (issue #57): runs
// scripts/openwrt-upgrade-e2e.sh against the slirp lab
// (scripts/openwrt-lab.sh up) with an apk-managed daemon. The lab
// image is provisioned in --no-daemon mode (runner mode upgrade), and
// the upgrade e2e installs the public signed old apk, starts the
// daemon under procd, upgrades to the new apk while continuous deny
// probes run, and asserts the running daemon is the new binary with no
// manual restart (see the script header).
//
// Required environment in upgrade mode:
//
//	E2E_LAB_MODE=upgrade
//	E2E_UPGRADE_OLD_APK=<public signed old apk>
//	E2E_UPGRADE_NEW_APK=<new apk (unsigned pre-release test build)>
//	E2E_UPGRADE_ANCHOR=<public anchor pem>
//	E2E_UPGRADE_ALLOW_UNTRUSTED=1  (unsigned test build only)
//
// The test requires:
//   - the lab to be up (scripts/openwrt-lab.sh up), daemon NOT yet
//     installed (the upgrade script installs it from the old apk),
//   - root for the client-namespace curl,
//   - python3 + curl + wget on the host.
//
// When E2E_LAB_MODE != upgrade the test is a no-op (same pattern as
// TestOpenWrtE2EV6): the slirp CI job must not see a SKIP.
package openwrt_integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOpenWrtE2EUpgrade(t *testing.T) {
	if os.Getenv("E2E_LAB_MODE") != "upgrade" {
		return
	}
	if reason := checkTools(); reason != "" {
		t.Skip(reason)
	}
	if reason := labUp(); reason != "" {
		t.Skip(reason)
	}

	old := os.Getenv("E2E_UPGRADE_OLD_APK")
	new := os.Getenv("E2E_UPGRADE_NEW_APK")
	anchor := os.Getenv("E2E_UPGRADE_ANCHOR")
	if old == "" || new == "" || anchor == "" {
		t.Fatalf("E2E_UPGRADE_OLD_APK/NEW_APK/ANCHOR must be set in upgrade mode")
	}

	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "openwrt-upgrade-e2e.sh")
	args := []string{script, "--old-apk", old, "--new-apk", new, "--anchor", anchor}
	if os.Getenv("E2E_UPGRADE_ALLOW_UNTRUSTED") == "1" {
		args = append(args, "--allow-untrusted")
	}
	cmd := exec.Command("bash", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("openwrt upgrade e2e failed: %v\n%s", err, out)
	}
	t.Logf("openwrt upgrade e2e output:\n%s", out)
}
