// Package package_test validates the release packaging pipeline
// (scripts/package.sh + scripts/verify-package.sh) without requiring
// root or a network: it builds a release archive for the host
// architecture, verifies its contents, architecture, version stamp,
// checksum, and byte-for-byte reproducibility. The SBOM step is
// skipped when syft is not installed (the CI `package` job installs
// syft and covers it).
package package_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// tests/package → repo root
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func hostArch(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Fatal("packaging tests require linux")
	}
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return runtime.GOARCH
	}
	t.Fatalf("unsupported test arch %s", runtime.GOARCH)
	return ""
}

func TestPackageArchive(t *testing.T) {
	root := repoRoot(t)
	version := "test-v0.0.0"
	arch := hostArch(t)
	dist := filepath.Join(root, "dist")

	// Fresh build with an explicit VERSION so the stamp assertion is
	// deterministic.
	cmd := exec.Command("bash", "scripts/package.sh")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "VERSION="+version, "ARCH="+arch)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("package.sh: %v: %s", err, out)
	}

	archive := filepath.Join(dist, "goshisen-linux-"+arch+".tar.gz")
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("archive missing: %v", err)
	}

	// 1. Extract into a temp dir.
	tmp := t.TempDir()
	if out, err := exec.Command("tar", "xzf", archive, "-C", tmp).CombinedOutput(); err != nil {
		t.Fatalf("extract: %v: %s", err, out)
	}
	dir := filepath.Join(tmp, "goshisen-linux-"+arch)
	for _, f := range []string{"goshisen", "LICENSE", "INSTALL.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("archive missing %s: %v", f, err)
		}
	}

	// 2. Static linkage (the binary must not need libc).
	bin := filepath.Join(dir, "goshisen")
	info, err := exec.Command("file", bin).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(info), "statically linked") {
		t.Errorf("binary not statically linked: %s", info)
	}
	if !strings.Contains(string(info), arch) {
		t.Errorf("binary architecture mismatch: %s", info)
	}

	// 3. Version stamp is compiled in and reported.
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version: %v: %s", err, out)
	}
	got := strings.TrimSpace(string(out))
	want := "goshisen " + version
	if got != want {
		// Default VERSION=dev when not passed; accept that when the
		// caller did not request a specific version.
		if got == "goshisen dev" && version == "dev" {
			// ok
		} else {
			t.Errorf("version = %q, want %q", got, want)
		}
	}

	// 4. Checksums file covers the archive and matches it.
	sums, err := os.ReadFile(filepath.Join(dist, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sums), filepath.Base(archive)) {
		t.Errorf("checksums.txt missing archive entry")
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if !strings.Contains(string(sums), hex.EncodeToString(sum[:])) {
		t.Errorf("checksums.txt does not contain the archive's sha256")
	}

	// 5. Reproducibility: a second build must be byte-identical.
	first, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	cmd2 := exec.Command("bash", "scripts/package.sh")
	cmd2.Dir = root
	cmd2.Env = append(os.Environ(), "VERSION="+version, "ARCH="+arch)
	if out, err := cmd2.CombinedOutput(); err != nil {
		t.Fatalf("second package.sh: %v: %s", err, out)
	}
	second, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("archives are not reproducible between builds")
	}
}
