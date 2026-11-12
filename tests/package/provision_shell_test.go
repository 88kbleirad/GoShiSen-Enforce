// Root-only behavioral tests for scripts/openwrt-image-prep.sh.
//
// These verify failure-safe download/checksum handling and scoped
// loop-device cleanup using disposable, isolated resources. They skip
// without root.
package package_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func rootAvailable() bool {
	if os.Geteuid() == 0 {
		return true
	}
	return exec.Command("sudo", "-n", "true").Run() == nil
}

// lookPathSbin finds a tool in PATH or the standard sbin directories
// (losetup/sfdisk/mkfs.ext4 live in /sbin on many distributions).
func lookPathSbin(tool string) string {
	if p, err := exec.LookPath(tool); err == nil {
		return p
	}
	for _, dir := range []string{"/sbin", "/usr/sbin", "/usr/local/sbin"} {
		p := filepath.Join(dir, tool)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

func runAsRoot(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	if os.Geteuid() != 0 {
		args = append([]string{"-n", name}, args...)
		name = "sudo"
	}
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// testScript writes a copy of the image-prep script into a temp dir and
// returns its path plus the dir, so tests can run it in isolation.
func testScript(t *testing.T) (string, string) {
	t.Helper()
	root := repoRoot(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "openwrt-image-prep.sh")
	data, err := os.ReadFile(filepath.Join(root, "scripts", "openwrt-image-prep.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return dst, dir
}

// TestProvisionDownloadFailureDoesNotCorrupt asserts download failure
// modes against a local HTTP server (no internet): a missing image must
// fail clearly, and a corrupt image must fail checksum verification.
// Both must leave no temporary directories behind.
func TestProvisionDownloadFailureDoesNotCorrupt(t *testing.T) {
	script, dir := testScript(t)
	imageName := "openwrt-25.12.6-x86-64-generic-ext4-combined.img.gz"

	// Local server: /sha256sums always present; the image is 404 unless
	// serving=="ok", in which case it returns bytes that do NOT match
	// the manifest (corrupt download).
	var serving atomic.Value
	serving.Store("missing")
	body := []byte("this is not the pinned image")
	sha := "0000000000000000000000000000000000000000000000000000000000000000"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state, _ := serving.Load().(string)
		switch {
		case strings.HasSuffix(r.URL.Path, "/sha256sums"):
			fmt.Fprintf(w, "%s *%s\n", sha, imageName)
		case strings.HasSuffix(r.URL.Path, imageName):
			if state == "missing" {
				http.NotFound(w, r)
				return
			}
			w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	daemon := filepath.Join(dir, "daemon")
	if err := os.WriteFile(daemon, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	run := func() (string, error) {
		cmd := exec.Command("bash", script,
			"--download", "--daemon", daemon,
			"--output", filepath.Join(dir, "out.img"))
		cmd.Env = append(os.Environ(), "OPENWRT_BASE_URL="+srv.URL)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// Case 1: missing image (HTTP 404).
	out, err := run()
	if err == nil {
		t.Fatalf("missing image: expected failure, got success:\n%s", out)
	}
	if !strings.Contains(out, "download failed") {
		t.Errorf("missing image: unclear error:\n%s", out)
	}

	// Case 2: present but corrupt image → checksum mismatch.
	serving.Store("corrupt")
	out, err = run()
	if err == nil {
		t.Fatalf("corrupt image: expected failure, got success:\n%s", out)
	}
	if !strings.Contains(out, "checksum") {
		t.Errorf("corrupt image: error does not mention checksum:\n%s", out)
	}

	// Failure-safe cleanup: no temporary directories left behind.
	if m, _ := filepath.Glob("/tmp/gsh-image-prep-*"); len(m) != 0 {
		t.Errorf("leftover prep temporaries: %v", m)
	}
}

// TestProvisionLoopCleanupScoped verifies that when provisioning fails
// after allocating a loop device (here: the partition is not a usable
// filesystem), the script detaches exactly its own loop device, leaves
// pre-existing loop devices untouched, and removes its temporaries.
// It uses a tiny synthetic gzip base: no network, no real image.
func TestProvisionLoopCleanupScoped(t *testing.T) {
	if !rootAvailable() {
		t.Skip("loop cleanup test requires root")
	}
	for _, tool := range []string{"sfdisk", "gzip", "zcat"} {
		if lookPathSbin(tool) == "" {
			t.Skipf("required tool %q not in PATH", tool)
		}
	}
	script, dir := testScript(t)

	// A pre-existing, unrelated loop device must survive the run.
	other := filepath.Join(dir, "other.img")
	if out, err := exec.Command("truncate", "-s", "1M", other).CombinedOutput(); err != nil {
		t.Fatalf("truncate: %v: %s", err, out)
	}
	otherLo, err := runAsRoot(t, "losetup", "--find", "--show", other)
	if err != nil {
		t.Fatalf("losetup other: %v: %s", err, otherLo)
	}
	otherLo = strings.TrimSpace(otherLo)
	defer func() { _, _ = runAsRoot(t, "losetup", "-d", otherLo) }()

	before := runLoopDevices(t)

	// Synthetic base: 32 MiB, two MBR partitions, but partition 2 holds
	// no filesystem, so the script's mount must fail and its cleanup
	// path must run.
	raw := filepath.Join(dir, "base.raw")
	if out, err := exec.Command("truncate", "-s", "32M", raw).CombinedOutput(); err != nil {
		t.Fatalf("truncate raw: %v: %s", err, out)
	}
	sfdisk := lookPathSbin("sfdisk")
	cmd := exec.Command("bash", "-c", sfdisk+" -q "+raw)
	cmd.Stdin = strings.NewReader("label: dos\n4096,2048,83,*\n6144,53248,83\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sfdisk: %v: %s", err, out)
	}
	base := filepath.Join(dir, "base.img.gz")
	f, err := os.Create(base)
	if err != nil {
		t.Fatal(err)
	}
	gz := exec.Command("gzip", "-c", raw)
	gz.Stdout = f
	if err := gz.Run(); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	f.Close()

	daemon := filepath.Join(dir, "daemon")
	if err := os.WriteFile(daemon, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	outImg := filepath.Join(dir, "out.img")
	o, err := runAsRoot(t, "bash", script,
		"--base", base, "--daemon", daemon, "--output", outImg)
	if err == nil {
		t.Fatalf("provision unexpectedly succeeded on a filesystem-less base:\n%s", o)
	}
	if !strings.Contains(o, "mount") {
		t.Errorf("expected a mount failure message, got:\n%s", o)
	}

	after := runLoopDevices(t)
	// The pre-existing device must still be attached to other.img.
	still := loopDeviceForBacking(t, other)
	if still == "" {
		t.Errorf("pre-existing loop device for %s was detached (before %s, after %v)",
			other, otherLo, after)
	}
	for dev := range after {
		if !before[dev] {
			t.Errorf("loop device %s leaked after failed provision", dev)
		}
	}
	// No leftover temp mount/prep directories.
	if m, _ := filepath.Glob("/tmp/gsh-owrt-mnt-*"); len(m) != 0 {
		t.Errorf("leftover mount temporaries: %v", m)
	}
	if m, _ := filepath.Glob("/tmp/gsh-image-prep-*"); len(m) != 0 {
		t.Errorf("leftover prep temporaries: %v", m)
	}
}

// TestProvisionNoGlobalLoopTeardown pins that the script never performs
// a global loop-device teardown (which would detach unrelated devices).
func TestProvisionNoGlobalLoopTeardown(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "openwrt-image-prep.sh"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if strings.Contains(body, "losetup -D") || strings.Contains(body, "losetup --detach-all") {
		t.Error("script uses a global loop-device teardown")
	}
	if !strings.Contains(body, `losetup -d "$LO"`) {
		t.Error(`script does not detach its own loop device via losetup -d "$LO"`)
	}
}

func runLoopDevices(t *testing.T) map[string]bool {
	t.Helper()
	out, err := runAsRoot(t, "losetup", "-a")
	if err != nil {
		t.Fatalf("losetup -a: %v: %s", err, out)
	}
	m := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 {
			m[f[0]] = true
		}
	}
	return m
}

// loopDeviceForBacking returns the loop device attached to path, or ""
// if none is.
func loopDeviceForBacking(t *testing.T, path string) string {
	t.Helper()
	out, err := runAsRoot(t, "losetup", "-j", path)
	if err != nil {
		t.Fatalf("losetup -j: %v: %s", err, out)
	}
	return strings.TrimSpace(strings.Split(string(out), ":")[0])
}
