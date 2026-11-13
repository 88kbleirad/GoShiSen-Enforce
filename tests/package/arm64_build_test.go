package package_test

// arm64 runtime/build regression coverage (Prompt 17).
//
// Deterministic CI check: cross-compile the daemon for linux/arm64,
// assert the artifact is a statically-linked aarch64 ELF, and if a
// qemu-user binfmt handler is available (e.g. `qemu-aarch64-static`),
// run the smoke CLI under emulation. No root, no production secret,
// no network is required. The full runtime path is covered by the
// OpenWrt armsr/armv8 QEMU lab evidence (docs/release/prompt17-*).
import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func needCmd(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available: %v", name, err)
	}
}

func TestArm64CrossCompileStaticELF(t *testing.T) {
	needCmd(t, "go")
	buildDir := t.TempDir()
	out := filepath.Join(buildDir, "goshisen-arm64")
	root := repoRoot(t)

	cmd := exec.Command("go", "build",
		"-trimpath", "-o", out, "./cmd/goshisen")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS=linux",
		"GOARCH=arm64",
	)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("arm64 cross-build failed: %v\n%s", err, b)
	}

	// ELF metadata: aarch64 + statically linked.
	fileOut, err := exec.Command("file", out).CombinedOutput()
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	s := string(fileOut)
	if !strings.Contains(s, "aarch64") {
		t.Fatalf("arm64 build is not aarch64: %s", s)
	}
	if !strings.Contains(s, "statically linked") && !strings.Contains(s, "static") {
		t.Fatalf("arm64 build is not static: %s", s)
	}
}

func TestArm64SmokeUnderQemuUser(t *testing.T) {
	// Only run if a qemu-user arm64 binfmt handler exists.
	qemu, err := exec.LookPath("qemu-aarch64-static")
	if err != nil {
		qemu, err = exec.LookPath("qemu-aarch64")
	}
	if err != nil {
		t.Skip("qemu-aarch64(-static) not available; static-ELF check covers the build")
	}
	needCmd(t, "go")

	buildDir := t.TempDir()
	out := filepath.Join(buildDir, "goshisen-arm64")
	cmd := exec.Command("go", "build", "-trimpath", "-o", out, "./cmd/goshisen")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0", "GOOS=linux", "GOARCH=arm64")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("arm64 cross-build failed: %v\n%s", err, b)
	}

	// --version must run under emulation.
	ver := exec.Command(qemu, out, "--version")
	if b, err := ver.CombinedOutput(); err != nil {
		t.Fatalf("arm64 --version under qemu failed: %v\n%s", err, b)
	} else if !strings.Contains(string(b), "goshisen") {
		t.Fatalf("arm64 --version output missing goshisen: %s", b)
	}

	// offload status under emulation must exit 0 (read-only).
	st := exec.Command(qemu, out, "offload", "status")
	if b, err := st.CombinedOutput(); err != nil {
		t.Fatalf("arm64 offload status under qemu failed: %v\n%s", err, b)
	}
}
