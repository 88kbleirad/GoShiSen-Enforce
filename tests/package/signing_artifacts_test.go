package package_test

// Signing & provenance verification tests (Prompt 12).
//
// Run the real scripts/sign-artifacts.sh + scripts/verify-signatures.sh with
// throwaway Ed25519 keys (no secrets) against small fixture artifacts. These
// cover: sign+verify success, wrong key, tampered artifact, missing signature,
// stale provenance.
import (
	"encoding/json"
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

func writeEd25519Key(t *testing.T, dir string) (priv, pub string) {
	t.Helper()
	priv = filepath.Join(dir, "key.pem")
	pub = filepath.Join(dir, "key.pub.pem")
	cmd := exec.Command("openssl", "genpkey", "-algorithm", "ed25519", "-out", priv)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("openssl genpkey: %v: %s", err, out)
	}
	cmd = exec.Command("openssl", "pkey", "-in", priv, "-pubout", "-out", pub)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("openssl pkey: %v: %s", err, out)
	}
	return priv, pub
}

func makeFixtureArtifacts(t *testing.T, dir string) {
	t.Helper()
	for _, f := range []string{"a.bin", "b.bin"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("fixture-"+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runSign(t *testing.T, dist, priv, pub, commit, tag string, sign bool) {
	t.Helper()
	args := []string{"--dist", dist, "--key", priv, "--public-key", pub,
		"--commit", commit, "--tag", tag, "--builder", "test", "--toolchain", "go1.25"}
	if sign {
		args = append(args, "--sign")
	}
	out, err := exec.Command("bash", append([]string{filepath.Join(repoRoot(t), "scripts", "sign-artifacts.sh")}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("sign-artifacts.sh: %v\n%s", err, out)
	}
}

func runVerify(t *testing.T, dist, pub, commit string, requireSign bool) (string, int) {
	t.Helper()
	args := []string{"--dist", dist, "--public-key", pub, "--commit", commit}
	if requireSign {
		args = append(args, "--require-sign")
	}
	out, err := exec.Command("bash", append([]string{filepath.Join(repoRoot(t), "scripts", "verify-signatures.sh")}, args...)...).CombinedOutput()
	rc := 0
	if ee, ok := err.(*exec.ExitError); ok {
		rc = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("verify-signatures.sh: %v", err)
	}
	return string(out), rc
}

func TestSignAndVerifySuccess(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	priv, pub := writeEd25519Key(t, dir)
	commit := "0123456789abcdef0123456789abcdef01234567"
	runSign(t, dist, priv, pub, commit, "v0.2.2-test", true)
	_, rc := runVerify(t, dist, pub, commit, true)
	if rc != 0 {
		t.Fatalf("verify rc=%d, want 0", rc)
	}
	for _, f := range []string{"a.bin", "b.bin"} {
		if _, err := os.Stat(filepath.Join(dist, f+".sig")); err != nil {
			t.Fatalf("missing signature for %s: %v", f, err)
		}
		if _, err := os.Stat(filepath.Join(dist, f+".provenance.json")); err != nil {
			t.Fatalf("missing provenance for %s: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dist, "public_key.pem")); err != nil {
		t.Fatalf("public key not published: %v", err)
	}
}

func TestVerifyWrongKeyFails(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	priv, pub := writeEd25519Key(t, dir)
	commit := "0123456789abcdef0123456789abcdef01234567"
	runSign(t, dist, priv, pub, commit, "v0.2.2-test", true)
	_, wrongPub := writeEd25519Key(t, dir)
	_, rc := runVerify(t, dist, wrongPub, commit, true)
	if rc == 0 {
		t.Fatal("verify with wrong key must fail")
	}
}

func TestVerifyTamperedArtifactFails(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	priv, pub := writeEd25519Key(t, dir)
	commit := "0123456789abcdef0123456789abcdef01234567"
	runSign(t, dist, priv, pub, commit, "v0.2.2-test", true)
	f, _ := os.OpenFile(filepath.Join(dist, "a.bin"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("evil")
	f.Close()
	_, rc := runVerify(t, dist, pub, commit, true)
	if rc == 0 {
		t.Fatal("tampered artifact must fail verification")
	}
}

func TestVerifyMissingSignatureFails(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	priv, pub := writeEd25519Key(t, dir)
	commit := "0123456789abcdef0123456789abcdef01234567"
	runSign(t, dist, priv, pub, commit, "v0.2.2-test", true)
	os.Remove(filepath.Join(dist, "a.bin.sig"))
	out, rc := runVerify(t, dist, pub, commit, true)
	if rc == 0 {
		t.Fatal("missing signature must fail with --require-sign")
	}
	if rc != 3 {
		t.Fatalf("expected exit 3 (missing-signature), got %d (%s)", rc, out)
	}
}

func resignFile(t *testing.T, priv, path string) {
	t.Helper()
	out, err := exec.Command("openssl", "pkeyutl", "-sign", "-rawin", "-inkey", priv,
		"-in", path, "-out", path+".sig").CombinedOutput()
	if err != nil {
		t.Fatalf("resign %s: %v: %s", path, err, out)
	}
}

// resignProvenance rewrites .sha256 in a provenance record and re-signs it with
// the same key so the signature stays valid but the recorded hash diverges.
func resignProvenance(t *testing.T, dist, artifact, priv, oldHash, newHash string) {
	t.Helper()
	pvPath := filepath.Join(dist, artifact+".provenance.json")
	data, err := os.ReadFile(pvPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), oldHash) {
		t.Fatalf("recorded hash %s not found in %s", oldHash, pvPath)
	}
	s := strings.Replace(string(data), oldHash, newHash, 1)
	if err := os.WriteFile(pvPath, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	resignFile(t, priv, pvPath)
}

func TestVerifyStaleProvenanceFails(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	priv, pub := writeEd25519Key(t, dir)
	commit := "0123456789abcdef0123456789abcdef01234567"
	runSign(t, dist, priv, pub, commit, "v0.2.2-test", true)
	resignProvenance(t, dist, "a.bin", priv, commit, "ffffffffffffffffffffffffffffffffffffffff")
	_, rc := runVerify(t, dist, pub, commit, true)
	if rc != 6 {
		t.Fatalf("expected exit 6 (stale-provenance), got %d", rc)
	}
}

func TestVerifyProvenanceMismatchFails(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	priv, pub := writeEd25519Key(t, dir)
	commit := "0123456789abcdef0123456789abcdef01234567"
	runSign(t, dist, priv, pub, commit, "v0.2.2-test", true)
	data, err := os.ReadFile(filepath.Join(dist, "a.bin.provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Sha256 string `json:"sha256"`
	}
	if err := json.Unmarshal(data, &recorded); err != nil {
		t.Fatal(err)
	}
	fake := strings.Repeat("0", len(recorded.Sha256))
	resignProvenance(t, dist, "a.bin", priv, recorded.Sha256, fake)
	_, rc := runVerify(t, dist, pub, commit, true)
	if rc != 5 {
		t.Fatalf("expected exit 5 (provenance-mismatch), got %d", rc)
	}
}

func TestVerifyMissingProvenanceFails(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	priv, pub := writeEd25519Key(t, dir)
	commit := "0123456789abcdef0123456789abcdef01234567"
	runSign(t, dist, priv, pub, commit, "v0.2.2-test", true)
	os.Remove(filepath.Join(dist, "a.bin.provenance.json"))
	_, rc := runVerify(t, dist, pub, commit, true)
	if rc != 8 {
		t.Fatalf("expected exit 8 (missing-provenance), got %d", rc)
	}
}

func TestVerifyMissingKeyFails(t *testing.T) {
	needCmd(t, "openssl")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	makeFixtureArtifacts(t, dist)
	_, rc := runVerify(t, dist, filepath.Join(dir, "nope.pem"), "", true)
	if rc != 2 {
		t.Fatalf("expected exit 2 (missing key), got %d", rc)
	}
}

func TestVerifyArchMismatchFails(t *testing.T) {
	needCmd(t, "openssl")
	needCmd(t, "tar")
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	os.MkdirAll(dist, 0o755)
	// amd64-named archive whose payload is an arm64 ELF.
	elf := filepath.Join(dir, "goshisen-linux-amd64", "goshisen")
	os.MkdirAll(filepath.Dir(elf), 0o755)
	if err := os.WriteFile(elf, []byte{
		// minimal ELF64 header, e_machine=183 (AArch64)
		0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		2, 0, 183, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 64, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	}, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("tar", "-czf", filepath.Join(dist, "goshisen-linux-amd64.tar.gz"),
		"-C", dir, "goshisen-linux-amd64")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tar: %v: %s", err, out)
	}
	priv, pub := writeEd25519Key(t, dir)
	commit := "0123456789abcdef0123456789abcdef01234567"
	runSign(t, dist, priv, pub, commit, "v0.2.2-test", true)
	_, rc := runVerify(t, dist, pub, commit, true)
	if rc != 7 {
		t.Fatalf("expected exit 7 (arch-mismatch), got %d", rc)
	}
}
