package package_test

// APK signing path tests (Prompt 35).
//
// Exercise scripts/apk-sign.sh + scripts/apk-verify.sh against a fixture
// zip/tar "apk" file with throwaway keys. They require an apk-tools v3
// binary (APK_TOOL env or PATH) because adbsign/verify are apk applets;
// skip cleanly when absent (CI/instabox have the SDK host apk).
import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func needApkTool(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("APK_TOOL"); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v
		}
	}
	if p, err := exec.LookPath("apk"); err == nil {
		return p
	}
	t.Skip("apk-tools v3 not available (set APK_TOOL)")
	return ""
}

// writePEMECKey writes an EC P-256 keypair (apk adbsign accepts PEM).
func writePEMECKey(t *testing.T, dir string) (priv, pub string) {
	t.Helper()
	priv = filepath.Join(dir, "ec.pem")
	pub = filepath.Join(dir, "ec.pub.pem")
	if out, err := exec.Command("openssl", "ecparam", "-name", "prime256v1", "-genkey", "-noout", "-out", priv).CombinedOutput(); err != nil {
		t.Fatalf("openssl ecparam: %v: %s", err, out)
	}
	if out, err := exec.Command("openssl", "ec", "-in", priv, "-pubout", "-out", pub).CombinedOutput(); err != nil {
		t.Fatalf("openssl ec pub: %v: %s", err, out)
	}
	return priv, pub
}

// makeFixtureApk writes a minimal v3 apk-like file. Real apk v3 packages
// are ADB tarballs; for unit tests we only need a file that sign/verify
// scripts can pass around — the real format validation happens in the lab
// with real apks. The verify script relies on `apk verify`, which requires
// a genuine package; so this test focuses on the fail-closed gates that
// run BEFORE apk (missing key, missing file, wrong usage) plus signing
// invocation with a real apk when one is provided via TEST_APK.
func fixtureApk(t *testing.T, dir string) string {
	t.Helper()
	if v := os.Getenv("TEST_APK"); v != "" {
		return v
	}
	p := filepath.Join(dir, "fixture.apk")
	if err := os.WriteFile(p, []byte("not a real apk payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestApkSignMissingKeyFails(t *testing.T) {
	dir := t.TempDir()
	apk := fixtureApk(t, dir)
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "apk-sign.sh"), "--apk", apk)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected failure with no key, got success: %s", out)
	} else if !strings.Contains(string(out), "signing key not provided") {
		t.Fatalf("expected 'signing key not provided', got: %s", out)
	}
}

func TestApkSignMissingFileFails(t *testing.T) {
	dir := t.TempDir()
	priv, _ := writePEMECKey(t, dir)
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "apk-sign.sh"),
		"--apk", filepath.Join(dir, "nope.apk"), "--key", priv)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected failure for missing apk, got success: %s", out)
	} else if !strings.Contains(string(out), "not found") {
		t.Fatalf("expected 'not found', got: %s", out)
	}
}

// TestApkVerifyUnsignedRejected uses a REAL signed-capable apk if
// TEST_APK points at one; otherwise it validates the missing-key gate.
func TestApkVerifyMissingAnchorFails(t *testing.T) {
	dir := t.TempDir()
	apk := fixtureApk(t, dir)
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "apk-verify.sh"),
		"--keys-dir", filepath.Join(dir, "missing-keys"), apk)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected failure with missing keys dir, got success: %s", out)
	} else if !strings.Contains(string(out), "keys dir") {
		t.Fatalf("expected 'keys dir' error, got: %s", out)
	}
}

// TestApkSignVerifyRoundtrip runs sign+verify with an ephemeral key when a
// real apk is provided (TEST_APK). Otherwise it skips (unit environment has
// no real apk build).
func TestApkSignVerifyRoundtrip(t *testing.T) {
	apkTool := needApkTool(t)
	realApk := os.Getenv("TEST_APK")
	if realApk == "" {
		t.Skip("TEST_APK not set; roundtrip requires a real apk build")
	}
	dir := t.TempDir()
	priv, pub := writePEMECKey(t, dir)
	apk := filepath.Join(dir, "pkg.apk")
	data, err := os.ReadFile(realApk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(apk, data, 0o644); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(), "APK_TOOL="+apkTool)
	sign := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "apk-sign.sh"), "--apk", apk, "--key", priv)
	sign.Env = env
	if out, err := sign.CombinedOutput(); err != nil {
		t.Fatalf("sign failed: %v: %s", err, out)
	}

	keysdir := filepath.Join(dir, "keys")
	if err := os.Mkdir(keysdir, 0o755); err != nil {
		t.Fatal(err)
	}
	anchor := filepath.Join(keysdir, "anchor.pub")
	if err := os.WriteFile(anchor, func() []byte { b, _ := os.ReadFile(pub); return b }(), 0o644); err != nil {
		t.Fatal(err)
	}
	// verify with the trust anchor (*) — apk verify keys are discovered by
	// content; we pass --keys-dir.
	verify := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "apk-verify.sh"),
		"--keys-dir", keysdir, apk)
	verify.Env = env
	if out, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("verify failed: %v: %s", err, out)
	}

	// Full-file integrity: --sha256 with a wrong recorded hash must fail
	// (tamper rejection beyond apk's internal control hashes).
	bad := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "apk-verify.sh"),
		"--keys-dir", keysdir,
		"--sha256", apk+"=0000000000000000000000000000000000000000000000000000000000000000",
		apk)
	bad.Env = env
	if out, err := bad.CombinedOutput(); err == nil {
		t.Fatalf("expected --sha256 mismatch failure, got success: %s", out)
	} else if !strings.Contains(string(out), "sha256 mismatch") {
		t.Fatalf("expected 'sha256 mismatch', got: %s", out)
	}
}

// ---- Prompt 36: release-gate orchestrator (fail-closed) ----

func runGate(t *testing.T, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(repoRoot(t), "scripts", "apk-release-gate.sh")}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestApkReleaseGateMissingSecretFails(t *testing.T) {
	dir := t.TempDir()
	_, pub := writePEMECKey(t, dir)
	apk := fixtureApk(t, dir)
	// no APK_SIGN_KEY in env -> gate must fail with BLOCKED message.
	out, err := runGate(t, nil,
		"--apk", apk, "--anchor", pub, "--fingerprint", "00",
		"--commit", "abc", "--outdir", filepath.Join(dir, "out"))
	if err == nil {
		t.Fatalf("expected failure with missing secret, got success: %s", out)
	}
	if !strings.Contains(out, "APK_SIGN_KEY") || !strings.Contains(out, "missing") {
		t.Fatalf("expected missing-secret BLOCKED message, got: %s", out)
	}
}

func TestApkReleaseGateFingerprintMismatchFails(t *testing.T) {
	dir := t.TempDir()
	_, pub := writePEMECKey(t, dir)
	apk := fixtureApk(t, dir)
	out, err := runGate(t, []string{"APK_SIGN_KEY=abc"},
		"--apk", apk, "--anchor", pub, "--fingerprint", "0000000000000000000000000000000000000000000000000000000000000000",
		"--commit", "abc", "--outdir", filepath.Join(dir, "out"))
	if err == nil {
		t.Fatalf("expected fingerprint mismatch failure, got success: %s", out)
	}
	if !strings.Contains(out, "fingerprint mismatch") {
		t.Fatalf("expected fingerprint mismatch message, got: %s", out)
	}
}

func TestApkReleaseGateMissingAnchorFails(t *testing.T) {
	dir := t.TempDir()
	apk := fixtureApk(t, dir)
	out, err := runGate(t, []string{"APK_SIGN_KEY=abc"},
		"--apk", apk, "--anchor", filepath.Join(dir, "nope.pub"), "--fingerprint", "00",
		"--commit", "abc", "--outdir", filepath.Join(dir, "out"))
	if err == nil {
		t.Fatalf("expected missing-anchor failure, got success: %s", out)
	}
	if !strings.Contains(out, "missing") {
		t.Fatalf("expected missing-anchor message, got: %s", out)
	}
}
