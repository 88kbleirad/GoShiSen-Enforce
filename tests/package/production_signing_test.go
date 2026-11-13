package package_test

// Production key bootstrap tests (Prompt 13).
//
// These verify the *committed production trust anchor* and the fail-closed
// release signing path using throwaway fixture keys only — never the real
// production private key (which lives solely in the GH_RELEASE_SIGNING_KEY
// GitHub Actions secret).
import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	wantPubFingerprint = "24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0"
	wantKeyID          = "f955ede984245d64"
)

func committedPubKey(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "keys", "goshisen-release.pub.pem")
}

// TestReleasePublicKeyValid asserts the committed trust anchor is a parseable
// Ed25519 public key whose fingerprint and key ID match the documented values.
func TestReleasePublicKeyValid(t *testing.T) {
	needCmd(t, "openssl")
	pub := committedPubKey(t)
	if _, err := os.Stat(pub); err != nil {
		t.Fatalf("committed public key missing: %v", err)
	}
	out, err := exec.Command("openssl", "pkey", "-pubin", "-in", pub, "-text", "-noout").CombinedOutput()
	if err != nil {
		t.Fatalf("openssl cannot parse committed public key: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ED25519") {
		t.Fatalf("committed key is not Ed25519: %s", out)
	}
	fp, err := exec.Command("openssl", "pkey", "-pubin", "-in", pub, "-outform", "DER").Output()
	if err != nil {
		t.Fatalf("openssl DER: %v", err)
	}
	fpHash := sha256Hex(fp)
	if fpHash != wantPubFingerprint {
		t.Fatalf("committed key fingerprint %s != documented %s", fpHash, wantPubFingerprint)
	}
	raw := rawPubHex(t, pub)
	if !strings.HasPrefix(raw, wantKeyID) {
		t.Fatalf("committed key ID %s != documented %s", raw[:16], wantKeyID)
	}
}

// TestReleaseKeyRejectsForeignSignature asserts a signature made by a different
// (throwaway) key fails verification against the committed production key.
func TestReleaseKeyRejectsForeignSignature(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	priv, _ := writeEd25519Key(t, dir) // throwaway, never production
	runSign(t, dist, priv, committedPubKey(t), "deadbeef", "v0.0.0-test", true)
	out, rc := runVerify(t, dist, committedPubKey(t), "deadbeef", true)
	if rc == 0 {
		t.Fatal("foreign-key signature must fail against the committed production key")
	}
	if rc != 4 {
		t.Fatalf("expected exit 4 (bad-signature), got %d (%s)", rc, out)
	}
}

// cleanSignEnv returns the ambient environment with all signing-related
// variables removed, so tests never inherit RELEASE_SIGN_KEY / RELEASE_PUB_KEY /
// RUNNER_TEMP from the caller's shell.
func cleanSignEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "RELEASE_SIGN_KEY=") ||
			strings.HasPrefix(kv, "RELEASE_PUB_KEY=") ||
			strings.HasPrefix(kv, "RUNNER_TEMP=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

// TestReleaseSignFailsWithoutSecret asserts release-sign.sh fails closed when
// the GH_RELEASE_SIGNING_KEY secret is absent.
func TestReleaseSignFailsWithoutSecret(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "release-sign.sh"),
		"--dist", dist, "--commit", "deadbeef", "--tag", "v0.0.0-test",
		"--builder", "test", "--toolchain", "go1.25")
	// NO RELEASE_SIGN_KEY in environment → must fail
	cmd.Env = cleanSignEnv()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("release-sign.sh must fail when secret is missing (got rc 0): %s", out)
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() != 1 {
		t.Fatalf("expected exit 1 (missing secret), got %d", ee.ExitCode())
	}
}

// TestReleaseSignFailsOnKeyMismatch asserts release-sign.sh refuses to sign
// when the secret key does not correspond to the committed trust anchor.
func TestReleaseSignFailsOnKeyMismatch(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	priv, _ := writeEd25519Key(t, dir) // throwaway ≠ production
	keyData, err := os.ReadFile(priv)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "release-sign.sh"),
		"--dist", dist, "--commit", "deadbeef", "--tag", "v0.0.0-test",
		"--builder", "test", "--toolchain", "go1.25")
	cmd.Env = cleanSignEnv("RELEASE_SIGN_KEY=" + string(keyData))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("release-sign.sh must fail on key mismatch (got rc 0): %s", out)
	}
	if !strings.Contains(string(out), "key mismatch") && !strings.Contains(string(out), "does not match") {
		t.Fatalf("expected key-mismatch failure, got: %s", out)
	}
}

// TestReleaseSignSucceedsWithMatchingKey runs the full production path with a
// throwaway key that *matches* a throwaway trust anchor (i.e. the same logic
// as a real release, minus production material).
func TestReleaseSignSucceedsWithMatchingKey(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	priv, pub := writeEd25519Key(t, dir)
	keyData, err := os.ReadFile(priv)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "release-sign.sh"),
		"--dist", dist, "--commit", "deadbeef", "--tag", "v0.0.0-test",
		"--builder", "test", "--toolchain", "go1.25")
	scratch := filepath.Join(dir, "scratch")
	os.MkdirAll(scratch, 0o755)
	cmd.Env = cleanSignEnv(
		"RELEASE_SIGN_KEY="+string(keyData),
		"RELEASE_PUB_KEY="+pub,
		"RUNNER_TEMP="+scratch,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("release-sign.sh with matching key failed: %v\n%s", err, out)
	}
	// Confirm the private key file is gone after the run.
	if _, err := os.Stat(filepath.Join(scratch, "goshisen-release-key.pem")); err == nil {
		t.Fatalf("private key material must be removed after signing")
	}
}

// TestReleaseWorkflowIsTagOnlyAndUsesSecret asserts the release workflow only
// triggers on tags (never PRs/forks) and references the production secret via
// env, so forks can never exercise it.
func TestReleaseWorkflowIsTagOnlyAndUsesSecret(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yml := string(b)
	if strings.Contains(yml, "pull_request") {
		t.Fatal("release.yml must NOT run on pull requests")
	}
	if !strings.Contains(yml, "tags:") || !strings.Contains(yml, `- "v*"`) {
		t.Fatal("release.yml must trigger only on v* tags")
	}
	if !strings.Contains(yml, "GH_RELEASE_SIGNING_KEY") {
		t.Fatal("release.yml must reference GH_RELEASE_SIGNING_KEY secret")
	}
	if !strings.Contains(yml, "release-sign.sh") {
		t.Fatal("release.yml must run the fail-closed signing wrapper")
	}
	// Per-artifact provenance (JSON + sig) must be uploaded so consumers can
	// verify commit/tag/arch/checksum provenance against the downloaded assets.
	for _, p := range []string{
		"goshisen-linux-amd64.tar.gz.provenance.json",
		"goshisen-linux-arm64.tar.gz.provenance.json",
		"checksums.txt.provenance.json",
		"sbom.spdx.json.provenance.json",
		"goshisen-linux-amd64.tar.gz.provenance.json.sig",
		"goshisen-linux-arm64.tar.gz.provenance.json.sig",
		"checksums.txt.provenance.json.sig",
		"sbom.spdx.json.provenance.json.sig",
	} {
		if !strings.Contains(yml, p) {
			t.Fatalf("release.yml upload must include %s", p)
		}
	}
}

// TestVerifySingleArtifactRejectsUnsignedAndTampered covers the installer-side
// gate: unsigned or tampered artifacts are rejected before install.
func TestVerifySingleArtifactRejectsUnsignedAndTampered(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	priv, pub := writeEd25519Key(t, dir)
	commit := "0123456789abcdef0123456789abcdef01234567"
	runSign(t, dist, priv, pub, commit, "v0.0.0-test", true)
	verifySingle := filepath.Join(repoRoot(t), "scripts", "verify-single-artifact.sh")

	// unsigned artifact (signature removed) → fail
	os.Remove(filepath.Join(dist, "a.bin.sig"))
	cmd := exec.Command("bash", verifySingle, filepath.Join(dist, "a.bin"),
		filepath.Join(dist, "a.bin.sig"), pub)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("unsigned artifact must be rejected: %s", out)
	}

	// tampered artifact → fail
	f, _ := os.OpenFile(filepath.Join(dist, "b.bin"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("evil")
	f.Close()
	cmd = exec.Command("bash", verifySingle, filepath.Join(dist, "b.bin"),
		filepath.Join(dist, "b.bin.sig"), pub)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("tampered artifact must be rejected: %s", out)
	}
}
