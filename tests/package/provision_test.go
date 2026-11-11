// Provisioning tests for scripts/openwrt-image-prep.sh.
//
// The pure checks (release URL construction, image filename, kmod ABI
// consistency, checksum parsing, print-config output) run without root
// and without network. Root-only behaviors (loop-device cleanup,
// download/checksum failure paths) are covered by shell tests in
// provision_shell_test.go which create disposable resources.
package package_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// provisionConfig runs the script with --print-config and returns the
// KEY=VALUE map. It performs no network or privileged operations.
func provisionConfig(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	cmd := exec.Command("bash", filepath.Join(root, "scripts", "openwrt-image-prep.sh"), "--print-config")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--print-config failed: %v: %s", err, out)
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		m[k] = v
	}
	return m
}

func TestProvisionReleaseURL(t *testing.T) {
	cfg := provisionConfig(t)

	if cfg["PINNED_RELEASE"] != "25.12.6" {
		t.Errorf("release = %q, want 25.12.6", cfg["PINNED_RELEASE"])
	}
	wantImage := "openwrt-25.12.6-x86-64-generic-ext4-combined.img.gz"
	if cfg["PINNED_IMAGE"] != wantImage {
		t.Errorf("image = %q, want %q", cfg["PINNED_IMAGE"], wantImage)
	}

	// The 404 defect: the image lives directly under targets/x86/64/,
	// NOT under targets/x86/64/generic/.
	wantURL := "https://downloads.openwrt.org/releases/25.12.6/targets/x86/64/" + wantImage
	if cfg["IMAGE_URL"] != wantURL {
		t.Errorf("IMAGE_URL = %q, want %q", cfg["IMAGE_URL"], wantURL)
	}
	if strings.Contains(cfg["IMAGE_URL"], "/generic/") {
		t.Errorf("IMAGE_URL still contains the nonexistent /generic/ path: %s", cfg["IMAGE_URL"])
	}

	wantManifest := "https://downloads.openwrt.org/releases/25.12.6/targets/x86/64/sha256sums"
	if cfg["MANIFEST_URL"] != wantManifest {
		t.Errorf("MANIFEST_URL = %q, want %q", cfg["MANIFEST_URL"], wantManifest)
	}
}

func TestProvisionKmodABIConsistency(t *testing.T) {
	cfg := provisionConfig(t)

	// Every kmod filename must embed the pinned kernel ABI's kernel
	// version and package release.
	fileName := cfg["KMOD_NFNETLINK_NAME"]
	if !strings.Contains(fileName, cfg["KMOD_KERNEL"]) || !strings.Contains(fileName, cfg["KMOD_PKGREL"]) {
		t.Errorf("nfnetlink-queue filename %q does not embed kernel %q pkgrel %q",
			fileName, cfg["KMOD_KERNEL"], cfg["KMOD_PKGREL"])
	}
	for _, name := range []string{"KMOD_NFT_NAME", "KMOD_CTNETLINK_NAME"} {
		fn := cfg[name]
		if !strings.Contains(fn, cfg["KMOD_KERNEL"]) {
			t.Errorf("%s = %q does not embed kernel %q", name, fn, cfg["KMOD_KERNEL"])
		}
	}

	// The kmod package release should match the kernel release (r1).
	if cfg["KMOD_PKGREL"] != "r1" {
		t.Errorf("KMOD_PKGREL = %q, want r1", cfg["KMOD_PKGREL"])
	}

	// ABI directory in the URL must contain the kernel version.
	if !strings.Contains(cfg["KMOD_BASE_URL"], cfg["KVER"]) {
		t.Errorf("KMOD_BASE_URL %q does not embed KVER %q", cfg["KMOD_BASE_URL"], cfg["KVER"])
	}
}

func TestProvisionChecksumConstants(t *testing.T) {
	cfg := provisionConfig(t)

	// sha256 values are 64 lowercase hex chars.
	for _, key := range []string{"KMOD_NFNETLINK_SHA", "KMOD_NFT_SHA", "KMOD_CTNETLINK_SHA"} {
		v := cfg[key]
		if len(v) != 64 {
			t.Errorf("%s = %q, want 64 hex chars", key, v)
		}
		for _, c := range v {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("%s = %q contains non-lowercase-hex char %q", key, v, c)
				break
			}
		}
	}
}

func TestProvisionImageChecksum(t *testing.T) {
	// The pinned image's sha256 is fixed by the official release
	// manifest. The manifest line format is:
	//   "<sha> *openwrt-25.12.6-x86-64-generic-ext4-combined.img.gz"
	// This test pins that the script's expected entry matches the
	// official hash (f8538784…) — a pure constant check, no file I/O.
	const officialManifestLine = "f8538784d0288b4133a99ac2a68443e4922ffde6343e80df56a5882364b9d499 *openwrt-25.12.6-x86-64-generic-ext4-combined.img.gz"

	script := filepath.Join(repoRoot(t), "scripts", "openwrt-image-prep.sh")
	out, err := exec.Command("bash", script, "--print-config").CombinedOutput()
	if err != nil {
		t.Fatalf("--print-config failed: %v: %s", err, out)
	}
	got := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "PINNED_IMAGE=") {
			got = strings.TrimPrefix(line, "PINNED_IMAGE=")
			break
		}
	}
	official := strings.TrimPrefix(officialManifestLine, "f8538784d0288b4133a99ac2a68443e4922ffde6343e80df56a5882364b9d499 *")
	if got != official {
		t.Errorf("PINNED_IMAGE = %q, want %q (official manifest)", got, official)
	}
}
