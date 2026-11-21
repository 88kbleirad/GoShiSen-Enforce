package store

import (
	"os"
	"path/filepath"
	"testing"
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
