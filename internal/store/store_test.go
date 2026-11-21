package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestOpenRestrictedPerms guards the store directory/file permissions:
// the store holds operator policy state and must be private to the
// daemon's user (CWE-276 hardening). The dir is 0700; version files and
// active.json are 0600 (the rename in atomicWrite keeps the temp file's
// mode).
func TestOpenRestrictedPerms(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "policy_store")

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Errorf("store dir perms = %o, want 700", got)
	}

	// WritePolicy temp->rename must leave a 0600 policy file.
	if err := s.WritePolicy("1", []byte("version: 1\ndefault_action: DROP\nrules: []\n")); err != nil {
		t.Fatalf("WritePolicy: %v", err)
	}
	pi, err := os.Stat(s.VersionPath("1"))
	if err != nil {
		t.Fatalf("stat policy: %v", err)
	}
	if got := pi.Mode().Perm(); got != 0o600 {
		t.Errorf("policy file perms = %o, want 600", got)
	}

	// active.json is written via atomicWrite -> 0600.
	if err := s.SetActive("1"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	ai, err := os.Stat(filepath.Join(dir, "active.json"))
	if err != nil {
		t.Fatalf("stat active: %v", err)
	}
	if got := ai.Mode().Perm(); got != 0o600 {
		t.Errorf("active.json perms = %o, want 600", got)
	}
}

// TestReadsRejectNonRegular guards the store reads against a planted
// non-regular file: a local writer with access to the store directory
// could otherwise make an API read (GET /v1/policy, activate) block
// forever (FIFO) or exhaust memory (device). Regression from the v0.2.7
// post-publish anomaly sweep.
func TestReadsRejectNonRegular(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Version file as a FIFO: ReadPolicy must fail fast, not block.
	fifo := s.VersionPath("1")
	if err := exec.Command("mkfifo", fifo).Run(); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.ReadPolicy("1")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ReadPolicy(fifo) succeeded; want error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReadPolicy(fifo) blocked; non-regular files must be rejected before opening")
	}

	// active.json as a character device: Active must fail, not stream.
	if _, err := os.Stat("/dev/urandom"); err == nil {
		if err := os.Remove(filepath.Join(dir, "active.json")); err == nil {
			t.Fatal("active.json unexpectedly existed")
		}
		if err := os.Symlink("/dev/urandom", filepath.Join(dir, "active.json")); err != nil {
			t.Skipf("symlink: %v", err)
		}
		done2 := make(chan error, 1)
		go func() {
			_, err := s.Active()
			done2 <- err
		}()
		select {
		case err := <-done2:
			if err == nil {
				t.Fatal("Active(device) succeeded; want error")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("Active(device) blocked; non-regular files must be rejected")
		}
	}
}
