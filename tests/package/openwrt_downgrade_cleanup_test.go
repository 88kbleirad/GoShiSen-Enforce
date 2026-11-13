package package_test

// OpenWrt package downgrade cleanup tests (Prompt 18).
//
// These exercise openwrt/files/lib/goshisen-lifecycle.sh (the shell
// helper sourced by prerm) against a sandboxed rc.d root
// (GOSHISEN_RC_ROOT), without root privileges. They cover: present
// package-owned links (removed), absent links (idempotent no-op),
// foreign files/symlinks with the same name (left alone + failure),
// malformed/broken links, repeated cleanup, and path-traversal-safe
// unlink behavior.
import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runLifecycle sources the library in a fresh sh and runs fn, with the
// given rc.d root. Returns combined output.
func runLifecycle(t *testing.T, rcRoot, fn string, args ...string) (string, error) {
	t.Helper()
	lib := filepath.Join(openwrtFilesDir(t), "lib", "goshisen-lifecycle.sh")
	script := ". " + lib + "\n" + fn + " " + strings.Join(args, " ") + "\n"
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "GOSHISEN_RC_ROOT="+rcRoot)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mkLink(t *testing.T, rcRoot, name, target string) {
	t.Helper()
	if err := os.Symlink(target, filepath.Join(rcRoot, name)); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleRemovesOwnedLinks(t *testing.T) {
	rcRoot := t.TempDir()
	mkLink(t, rcRoot, "S12goshisen-boot", "../init.d/goshisen-boot")
	mkLink(t, rcRoot, "K10goshisen-boot", "../init.d/goshisen-boot")
	mkLink(t, rcRoot, "S90goshisen", "../init.d/goshisen")
	mkLink(t, rcRoot, "K10goshisen", "../init.d/goshisen")

	out, err := runLifecycle(t, rcRoot, "goshisen_remove_links")
	if err != nil {
		t.Fatalf("remove_links failed: %v: %s", err, out)
	}
	for _, name := range []string{"S12goshisen-boot", "K10goshisen-boot", "S90goshisen", "K10goshisen"} {
		if _, err := os.Lstat(filepath.Join(rcRoot, name)); err == nil {
			t.Fatalf("%s still present after cleanup", name)
		}
	}
}

func TestLifecycleIdempotentAbsent(t *testing.T) {
	rcRoot := t.TempDir()
	for i := 0; i < 3; i++ {
		out, err := runLifecycle(t, rcRoot, "goshisen_remove_links")
		if err != nil {
			t.Fatalf("run %d failed: %v: %s", i, err, out)
		}
	}
}

func TestLifecycleLeavesForeignFile(t *testing.T) {
	rcRoot := t.TempDir()
	// A regular file (not ours) with the same name must not be removed.
	if err := os.WriteFile(filepath.Join(rcRoot, "S90goshisen"), []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runLifecycle(t, rcRoot, "goshisen_remove_links")
	if err == nil {
		t.Fatalf("expected failure for foreign file, got success: %s", out)
	}
	if !strings.Contains(out, "refusing") {
		t.Fatalf("expected refusing message, got: %s", out)
	}
	if _, err := os.Stat(filepath.Join(rcRoot, "S90goshisen")); err != nil {
		t.Fatalf("foreign file removed: %v", err)
	}
}

func TestLifecycleLeavesForeignSymlink(t *testing.T) {
	rcRoot := t.TempDir()
	// A symlink pointing outside the package's init.d must be left alone.
	mkLink(t, rcRoot, "S90goshisen", "../init.d/other-service")
	out, err := runLifecycle(t, rcRoot, "goshisen_remove_links")
	if err == nil {
		t.Fatalf("expected failure for foreign symlink, got success: %s", out)
	}
	if _, err := os.Lstat(filepath.Join(rcRoot, "S90goshisen")); err != nil {
		t.Fatalf("foreign symlink removed: %v", err)
	}
}

func TestLifecycleBrokenLinkRemoved(t *testing.T) {
	rcRoot := t.TempDir()
	// Broken symlink (target missing) with our prefix must be removed.
	mkLink(t, rcRoot, "S12goshisen-boot", "../init.d/goshisen-boot")
	out, err := runLifecycle(t, rcRoot, "goshisen_remove_links")
	if err != nil {
		t.Fatalf("cleanup failed: %v: %s", err, out)
	}
	if _, err := os.Lstat(filepath.Join(rcRoot, "S12goshisen-boot")); err == nil {
		t.Fatal("broken package link not removed")
	}
}

func TestLifecycleNgPathTraversal(t *testing.T) {
	rcRoot := t.TempDir()
	// A link whose target escapes (../..) is foreign and must be left.
	mkLink(t, rcRoot, "S12goshisen-boot", "../../etc/passwd")
	out, err := runLifecycle(t, rcRoot, "goshisen_remove_links")
	if err == nil {
		t.Fatalf("expected failure for traversal target, got success: %s", out)
	}
	if _, err := os.Lstat(filepath.Join(rcRoot, "S12goshisen-boot")); err != nil {
		t.Fatalf("traversal symlink removed: %v", err)
	}
}

func TestLifecycleMalformedLinkTarget(t *testing.T) {
	rcRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(rcRoot, "K10goshisen"), []byte("dangling"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runLifecycle(t, rcRoot, "goshisen_remove_links")
	if err == nil {
		t.Fatalf("expected failure for malformed entry, got success: %s", out)
	}
	if !strings.Contains(out, "refusing") {
		t.Fatalf("expected refusing message, got: %s", out)
	}
}

func TestOpenWrtMakefileInstallsCleanup(t *testing.T) {
	mk, err := os.ReadFile(filepath.Join(repoRoot(t), "openwrt", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(mk)
	if !strings.Contains(s, "lib/goshisen/goshisen-lifecycle.sh") {
		t.Fatal("Makefile must install lib/goshisen/goshisen-lifecycle.sh")
	}
	if !strings.Contains(s, "goshisen_remove_links") {
		t.Fatal("Makefile prerm must call goshisen_remove_links")
	}
	if strings.Contains(s, "rm -rf") {
		t.Fatal("cleanup must not use rm -rf")
	}

	// The library itself must use unlink (never follow symlinks).
	lib, err := os.ReadFile(filepath.Join(openwrtFilesDir(t), "lib", "goshisen-lifecycle.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lib), "unlink") {
		t.Fatal("library must use unlink (no follow)")
	}
}
