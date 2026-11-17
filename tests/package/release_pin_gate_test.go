package package_test

// Release pin/version agreement gate tests (Item 1, issue #49).
//
// Each test builds a scratch git repository with a synthetic
// openwrt/Makefile and runtime tree, then runs scripts/release-pin-gate.sh
// against it (GATE_REPO points at the scratch repo). This keeps the
// gate deterministic and independent of the live repository's history.
//
// The gate must fail for the right reason in each negative case, and
// pass in the matching case.
import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	pinTip = "TIP" // pin = tag commit itself (matching)
	pinAnc = "ANC" // pin = first commit (ancestor, empty runtime diff)
	pinBad = "BAD" // pin = unknown commit (missing pin)
)

// git runs git in the scratch repo (dir) and fails the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// newGateRepo builds a scratch repo:
//
//	commit A  runtime tree (cmd/internal/go.mod)          <- pin candidate
//	commit B  optional: runtime change (sourceDiff) or
//	          non-runtime change (docs/) — only when extraCommit
//	commit C  openwrt/Makefile (pin chosen per pinChoice) <- tag target
//
// It tags v<version> at C and returns (dir, C commit, A commit SHA).
func newGateRepo(t *testing.T, version, release string, pinChoice string, sourceDiff, extraCommit bool) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "openwrt", "files", "etc", "goshisen"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "goshisen"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Gate Test")
	git(t, dir, "config", "commit.gpgsign", "false")

	// Commit A: runtime tree.
	if err := os.WriteFile(filepath.Join(dir, "cmd", "goshisen", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal", "app", "app.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module gate-test\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "runtime")
	first := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	// Commit B (optional).
	if extraCommit {
		if sourceDiff {
			if err := os.WriteFile(filepath.Join(dir, "cmd", "goshisen", "main.go"), []byte("package main\nfunc main() { println(1) }\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "docs", "note.md"), []byte("docs only\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-q", "-m", "follow-up")
	}

	// Commit C: Makefile with the chosen pin.
	pin := ""
	switch pinChoice {
	case pinTip:
		pin = strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	case pinAnc:
		pin = first
	case pinBad:
		pin = "deadbeef0000000000000000000000000000000000"
	default:
		t.Fatalf("unknown pinChoice %q", pinChoice)
	}
	makefileWith(t, dir, version, release, pin)
	git(t, dir, "add", "openwrt/Makefile")
	git(t, dir, "commit", "-q", "-m", "makefile")
	tip := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	// Annotated (not lightweight) so git verify-tag works on it: the
	// release tags this gate checks are annotated.
	git(t, dir, "tag", "-a", "v"+version, "-m", "v"+version)
	return dir, tip, first
}

// makefileWith writes a minimal openwrt/Makefile with the given variables.
func makefileWith(t *testing.T, dir, version, release, pin string) {
	t.Helper()
	content := "# synthetic package Makefile for gate tests\n" +
		"PKG_NAME:=goshisen\n" +
		"PKG_VERSION:=" + version + "\n" +
		"PKG_RELEASE:=" + release + "\n" +
		"PKG_SOURCE_VERSION:=" + pin + "\n"
	if err := os.WriteFile(filepath.Join(dir, "openwrt", "Makefile"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runGate runs the gate against the scratch repo with a synthetic payload
// binary that reports payloadVersion, and an apk of the given name.
func runPinGate(t *testing.T, dir, apkName, payloadVersion string) (string, error) {
	t.Helper()
	payload := filepath.Join(dir, "payload")
	content := "#!/bin/sh\necho \"goshisen " + payloadVersion + "\"\n"
	if err := os.WriteFile(payload, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "release-pin-gate.sh"),
		"--apk", apkName,
		"--tag", "v0.2.4",
		"--payload", payload)
	cmd.Env = append(os.Environ(), "GATE_REPO="+dir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestGateMatchingCasePasses(t *testing.T) {
	// Pin = tag commit; empty runtime diff; labels agree.
	dir, _, _ := newGateRepo(t, "0.2.4", "1", pinTip, false, false)
	out, err := runPinGate(t, dir, "goshisen-0.2.4-r1.apk", "0.2.4")
	if err != nil {
		t.Fatalf("expected PASS, got error: %v\n%s", err, out)
	}
	for _, want := range []string{"PASS", "PKG_VERSION=0.2.4", "payload --version matches", "apk filename matches"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got:\n%s", want, out)
		}
	}
}

func TestGateAncestorPinPasses(t *testing.T) {
	// Pin = ancestor commit A; follow-up commit touches docs only, so the
	// runtime diff pin..tag is empty and the gate passes.
	dir, _, _ := newGateRepo(t, "0.2.4", "1", pinAnc, false, true)
	out, err := runPinGate(t, dir, "goshisen-0.2.4-r1.apk", "0.2.4")
	if err != nil {
		t.Fatalf("expected PASS for ancestor pin, got: %v\n%s", err, out)
	}
	if !strings.Contains(out, "is ancestor/equal") {
		t.Fatalf("expected ancestor confirmation, got:\n%s", out)
	}
}

func TestGateSourceDiffFails(t *testing.T) {
	// Pin = ancestor commit A; follow-up commit changes cmd/ so the
	// runtime diff is non-empty.
	dir, _, _ := newGateRepo(t, "0.2.4", "1", pinAnc, true, true)
	out, err := runPinGate(t, dir, "goshisen-0.2.4-r1.apk", "0.2.4")
	if err == nil {
		t.Fatalf("expected failure on source diff, got success: %s", out)
	}
	if !strings.Contains(out, "source diff between pin and tag is NOT empty") {
		t.Fatalf("expected source-diff failure message, got:\n%s", out)
	}
}

func TestGateVersionMismatchFails(t *testing.T) {
	// PKG_VERSION=0.2.4 but payload --version reports 0.2.3.
	dir, _, _ := newGateRepo(t, "0.2.4", "1", pinTip, false, false)
	out, err := runPinGate(t, dir, "goshisen-0.2.4-r1.apk", "0.2.3")
	if err == nil {
		t.Fatalf("expected failure on version mismatch, got success: %s", out)
	}
	if !strings.Contains(out, "payload --version") {
		t.Fatalf("expected payload-version failure message, got:\n%s", out)
	}
}

func TestGateMissingPinFails(t *testing.T) {
	// PKG_SOURCE_VERSION references an unknown commit.
	dir, _, _ := newGateRepo(t, "0.2.4", "1", pinBad, false, false)
	out, err := runPinGate(t, dir, "goshisen-0.2.4-r1.apk", "0.2.4")
	if err == nil {
		t.Fatalf("expected failure on missing pin, got success: %s", out)
	}
	if !strings.Contains(out, "not found") {
		t.Fatalf("expected missing-pin failure message, got:\n%s", out)
	}
}

func TestGateApkFilenameMismatchFails(t *testing.T) {
	// Gate metadata correct but apk filename carries the wrong version.
	dir, _, _ := newGateRepo(t, "0.2.4", "1", pinTip, false, false)
	out, err := runPinGate(t, dir, "goshisen-0.2.3-r1.apk", "0.2.4")
	if err == nil {
		t.Fatalf("expected failure on apk filename mismatch, got success: %s", out)
	}
	if !strings.Contains(out, "apk filename") {
		t.Fatalf("expected apk-filename failure message, got:\n%s", out)
	}
}

// signTagSSH signs an existing tag in dir with a fresh ephemeral SSH key
// and returns (dir, allowedSignersPath). The allowed_signers file maps the
// principal to that key. The caller must run the gate with
// GATE_TAG_SIGN_CHECK=1 and ALLOWED_SIGNERS=<allowedSignersPath>.
func signTagSSH(t *testing.T, dir, tag string) string {
	t.Helper()
	key := filepath.Join(dir, "signer")
	out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	pub, err := exec.Command("ssh-keygen", "-y", "-f", key).Output()
	if err != nil {
		t.Fatalf("ssh-keygen -y: %v", err)
	}
	allowed := filepath.Join(dir, "allowed_signers")
	content := "maintainer@example.com " + strings.TrimSpace(string(pub)) + "\n"
	if err := os.WriteFile(allowed, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "config", "gpg.format", "ssh")
	git(t, dir, "config", "user.signingkey", key)
	git(t, dir, "tag", "-f", "-s", tag, "-m", "signed "+tag)
	return allowed
}

func TestGateTagSignatureCheckPasses(t *testing.T) {
	dir, _, _ := newGateRepo(t, "0.2.4", "1", pinTip, false, false)
	allowed := signTagSSH(t, dir, "v0.2.4")
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "release-pin-gate.sh"),
		"--apk", "goshisen-0.2.4-r1.apk",
		"--tag", "v0.2.4",
		"--payload", filepath.Join(dir, "payload"))
	cmd.Env = append(os.Environ(), "GATE_REPO="+dir, "GATE_TAG_SIGN_CHECK=1", "ALLOWED_SIGNERS="+allowed)
	payload := filepath.Join(dir, "payload")
	if err := os.WriteFile(payload, []byte("#!/bin/sh\necho \"goshisen 0.2.4\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected PASS, got error: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "tag signature verified") {
		t.Fatalf("expected 'tag signature verified' in output:\n%s", out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected PASS in output:\n%s", out)
	}
}

func TestGateTagSignatureCheckFailsUnsigned(t *testing.T) {
	dir, _, _ := newGateRepo(t, "0.2.4", "1", pinTip, false, false)
	// Tag is unsigned (as created by newGateRepo); force a valid
	// allowed_signers so the only failure is the missing signature.
	allowed := filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(allowed, []byte("maintainer@example.com ssh-ed25519 AAAA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "release-pin-gate.sh"),
		"--apk", "goshisen-0.2.4-r1.apk",
		"--tag", "v0.2.4",
		"--payload", filepath.Join(dir, "payload"))
	cmd.Env = append(os.Environ(), "GATE_REPO="+dir, "GATE_TAG_SIGN_CHECK=1", "ALLOWED_SIGNERS="+allowed)
	payload := filepath.Join(dir, "payload")
	if err := os.WriteFile(payload, []byte("#!/bin/sh\necho \"goshisen 0.2.4\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected failure on unsigned tag, got success:\n%s", out)
	}
	if !strings.Contains(string(out), "not signed") {
		t.Fatalf("expected 'not signed' failure message:\n%s", out)
	}
}

func TestGateTagSignatureCheckFailsForgedKey(t *testing.T) {
	// Tag signed with key A, but allowed_signers lists key B: the
	// signature is cryptographically valid yet from an unauthorized key
	// ("No principal matched.") — must fail when enforcement is on.
	dir, _, _ := newGateRepo(t, "0.2.4", "1", pinTip, false, false)
	// Sign with key A.
	keyA := filepath.Join(dir, "signerA")
	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", keyA).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen A: %v: %s", err, out)
	}
	git(t, dir, "config", "gpg.format", "ssh")
	git(t, dir, "config", "user.signingkey", keyA)
	git(t, dir, "tag", "-f", "-s", "v0.2.4", "-m", "signed v0.2.4")

	// allowed_signers lists key B (a different key).
	keyB := filepath.Join(dir, "signerB")
	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", keyB).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen B: %v: %s", err, out)
	}
	pubB, err := exec.Command("ssh-keygen", "-y", "-f", keyB).Output()
	if err != nil {
		t.Fatalf("ssh-keygen -y B: %v", err)
	}
	allowed := filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(allowed, []byte("maintainer@example.com "+strings.TrimSpace(string(pubB))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "release-pin-gate.sh"),
		"--apk", "goshisen-0.2.4-r1.apk",
		"--tag", "v0.2.4",
		"--payload", filepath.Join(dir, "payload"))
	cmd.Env = append(os.Environ(), "GATE_REPO="+dir, "GATE_TAG_SIGN_CHECK=1", "ALLOWED_SIGNERS="+allowed)
	payload := filepath.Join(dir, "payload")
	if err := os.WriteFile(payload, []byte("#!/bin/sh\necho \"goshisen 0.2.4\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected failure on forged/wrong-key signature, got success:\n%s", out)
	}
	if !strings.Contains(string(out), "not signed by an allowed signer") {
		t.Fatalf("expected 'not signed by an allowed signer' failure message:\n%s", out)
	}
}
