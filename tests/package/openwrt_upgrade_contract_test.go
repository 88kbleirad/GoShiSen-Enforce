// OpenWrt package upgrade-behavior tests (issue #57).
//
// These run without root or an SDK and pin the two fixes:
//  1. The postinst body must perform a controlled restart on upgrade
//     (PKG_UPGRADE=1) so the new daemon binary actually runs, and
//     must NOT restart on a fresh install or when the service is
//     disabled (a disabled service has no daemon to restart).
//  2. goshisen-boot start must be idempotent: when the app-owned
//     table/chain already exists (installed by the running daemon
//     with a different declaration) it must NOT try to re-add the
//     chain (which errors) and must NOT delete/replace it (which
//     would open the fail-open window).
//
// The postinst body is tested by rendering it into a script and
// rewriting the absolute /etc/init.d paths to a sandboxed root with
// fake init scripts that record the commands they receive.
package package_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// postinstBody extracts the Package/goshisen/postinst body from the
// openwrt Makefile, dropping the `define ... endef` wrapper and Make
// `$$` escapes, returning ready-to-run shell.
func postinstBody(t *testing.T) string {
	t.Helper()
	mk, err := os.ReadFile(filepath.Join(repoRoot(t), "openwrt", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(mk)
	start := strings.Index(s, "define Package/goshisen/postinst")
	if start < 0 {
		t.Fatal("Package/goshisen/postinst not found in openwrt/Makefile")
	}
	start = strings.Index(s[start:], "\n") + start + 1
	end := strings.Index(s[start:], "endef")
	if end < 0 {
		t.Fatal("endef not found after Package/goshisen/postinst")
	}
	body := s[start : start+end]
	// Make `$${...}` -> `${...}` for shell.
	body = strings.ReplaceAll(body, "$$", "$")
	return body
}

// upTestEnv builds a sandbox with fake init scripts and a fake rc.d,
// returning the root path plus a log path that records invocations.
func upTestEnv(t *testing.T) (root, logPath string) {
	t.Helper()
	root = t.TempDir()
	initd := filepath.Join(root, "etc", "init.d")
	if err := os.MkdirAll(initd, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath = filepath.Join(root, "calls.log")
	fake := `#!/bin/sh
if [ "$1" = "enabled" ]; then
  [ -L "$GOSHISEN_RC_DIR/S90goshisen" ] && exit 0
  exit 1
fi
echo "$0:$1" >> "$GOSHISEN_TEST_LOG"
exit 0
`
	for _, name := range []string{"goshisen", "goshisen-boot"} {
		p := filepath.Join(initd, name)
		if err := os.WriteFile(p, []byte(fake), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	rc := filepath.Join(root, "etc", "rc.d")
	if err := os.MkdirAll(rc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../init.d/goshisen", filepath.Join(rc, "S90goshisen")); err != nil {
		t.Fatal(err)
	}
	// `enabled` is implemented by rc.common as a check of the rc.d
	// symlinks. Our fake init scripts must support `enabled`; the
	// body calls `<initd>/goshisen enabled`. Provide that in the fake.
	return root, logPath
}

func runUpBody(t *testing.T, body, sandboxRoot, logPath, pkgUpgrade string) (string, error) {
	t.Helper()
	// Rewrite absolute /etc/init.d paths to the sandbox, preserving
	// the semantics of the body (which always uses the absolute path).
	rendered := strings.ReplaceAll(body, "/etc/init.d/goshisen",
		filepath.Join(sandboxRoot, "etc", "init.d", "goshisen"))
	rendered = strings.ReplaceAll(rendered, "/etc/init.d/goshisen-boot",
		filepath.Join(sandboxRoot, "etc", "init.d", "goshisen-boot"))
	script := "export PKG_UPGRADE=" + pkgUpgrade + "\n" +
		"default_postinst() { :; }\n" +
		rendered + "\n"
	scriptPath := filepath.Join(sandboxRoot, "post-upgrade.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", scriptPath)
	cmd.Env = append(os.Environ(),
		"GOSHISEN_TEST_LOG="+logPath,
		"GOSHISEN_RC_DIR="+filepath.Join(sandboxRoot, "etc", "rc.d"),
		"PATH=/usr/bin:/bin",
		"PKG_UPGRADE="+pkgUpgrade,
		"IPKG_INSTROOT=",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func readCalls(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	var calls []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			calls = append(calls, line)
		}
	}
	return calls
}

func TestPostinstRestartsOnUpgradeWhenEnabled(t *testing.T) {
	root, log := upTestEnv(t)
	body := postinstBody(t)
	if _, err := runUpBody(t, body, root, log, "1"); err != nil {
		t.Fatalf("post-upgrade body failed: %v", err)
	}
	calls := readCalls(t, log)
	found := false
	for _, c := range calls {
		if strings.HasSuffix(c, ":restart") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected goshisen restart on enabled upgrade, calls: %v", calls)
	}
}

func TestPostinstNoRestartOnFreshInstall(t *testing.T) {
	root, log := upTestEnv(t)
	body := postinstBody(t)
	// Fresh install: PKG_UPGRADE unset (empty).
	if _, err := runUpBody(t, body, root, log, ""); err != nil {
		t.Fatalf("postinst body failed: %v", err)
	}
	for _, c := range readCalls(t, log) {
		if strings.HasSuffix(c, ":restart") {
			t.Fatalf("restart must not run on fresh install: %v", c)
		}
	}
}

func TestPostinstNoRestartWhenDisabled(t *testing.T) {
	root, log := upTestEnv(t)
	body := postinstBody(t)
	// Disabled: remove the rc.d symlink so `enabled` fails.
	if err := os.Remove(filepath.Join(root, "etc", "rc.d", "S90goshisen")); err != nil {
		t.Fatal(err)
	}
	if _, err := runUpBody(t, body, root, log, "1"); err != nil {
		t.Fatalf("post-upgrade body failed: %v", err)
	}
	for _, c := range readCalls(t, log) {
		if strings.HasSuffix(c, ":restart") {
			t.Fatalf("restart must not run when service disabled: %v", c)
		}
	}
}

// TestBootGuardStartIdempotent renders goshisen-boot's start() with a
// mocked nft that reports the table/chain present, and asserts no
// `add chain` attempt occurs (and no error).
func TestBootGuardStartIdempotent(t *testing.T) {
	boot := readPackagedFile(t, filepath.Join("etc", "init.d", "goshisen-boot"))

	// Extract the start() body.
	startIdx := strings.Index(boot, "start() {")
	if startIdx < 0 {
		t.Fatal("start() not found in goshisen-boot")
	}
	startIdx += len("start() {")
	endIdx := strings.Index(boot[startIdx:], "\n}")
	if endIdx < 0 {
		t.Fatal("end of start() not found")
	}
	startBody := boot[startIdx : startIdx+endIdx]
	// Rewrite the absolute /etc paths and nft to a sandbox.
	root := t.TempDir()
	initd := filepath.Join(root, "etc", "init.d")
	if err := os.MkdirAll(initd, 0o755); err != nil {
		t.Fatal(err)
	}

	// Mock nft: table and chain both exist -> list returns 0, add
	// would be recorded. We assert add never runs.
	log := filepath.Join(root, "nft.log")
	mockNft := `#!/bin/sh
arg="$1"
if [ "$arg" = "list" ]; then
  exit 0
elif [ "$arg" = "add" ]; then
  echo "ADD" >> "$NFT_LOG"
  exit 0
fi
exit 0
`
	nftPath := filepath.Join(root, "nft")
	if err := os.WriteFile(nftPath, []byte(mockNft), 0o755); err != nil {
		t.Fatal(err)
	}

	// service_enabled returns 0 (service enabled).
	script := "service_enabled() { return 0; }\n" +
		"nft() { PATH=\"$SANDBOX_DIR:$PATH\" \"$SANDBOX_DIR/nft\" \"$@\"; }\n" +
		"export NFT_LOG=" + log + "\n" +
		"export SANDBOX_DIR=" + root + "\n" +
		startBody + "\n"
	scriptPath := filepath.Join(root, "boot-start.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("sh", scriptPath).CombinedOutput()
	if err != nil {
		t.Fatalf("boot start with existing chain failed: %v: %s", err, out)
	}
	data, _ := os.ReadFile(log)
	if strings.Contains(string(data), "ADD") {
		t.Fatalf("boot start must not re-add an existing chain: %s", data)
	}
}

// TestBootGuardStartCreatesWhenAbsent asserts the chain is created when
// the table/chain do not exist (nft list fails).
func TestBootGuardStartCreatesWhenAbsent(t *testing.T) {
	boot := readPackagedFile(t, filepath.Join("etc", "init.d", "goshisen-boot"))
	startIdx := strings.Index(boot, "start() {")
	startIdx += len("start() {")
	endIdx := strings.Index(boot[startIdx:], "\n}")
	startBody := boot[startIdx : startIdx+endIdx]

	root := t.TempDir()
	log := filepath.Join(root, "nft.log")
	mockNft := `#!/bin/sh
case "$1" in
  list*) exit 1 ;;
  add*) echo "ADD:$*" >> "$NFT_LOG"; exit 0 ;;
  *) exit 0 ;;
esac
`
	nftPath := filepath.Join(root, "nft")
	if err := os.WriteFile(nftPath, []byte(mockNft), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "service_enabled() { return 0; }\n" +
		"nft() { PATH=\"$SANDBOX_DIR:$PATH\" \"$SANDBOX_DIR/nft\" \"$@\"; }\n" +
		"export NFT_LOG=" + log + "\n" +
		"export SANDBOX_DIR=" + root + "\n" +
		startBody + "\n"
	scriptPath := filepath.Join(root, "boot-start.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("boot start failed: %v: %s", err, out)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "ADD:add chain") {
		t.Fatalf("boot start should add the chain when absent: %q", data)
	}
}
