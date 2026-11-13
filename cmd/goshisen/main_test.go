package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/cli"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/config"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/store"
)

// TestRunPolicyDryRunReadOnly verifies `policy dry-run` previews the
// publish the daemon would perform without writing anything to the
// store: an empty store shows "first policy"; a store with an active
// version shows the next version and the rules delta.
func TestRunPolicyDryRunReadOnly(t *testing.T) {
	storeDir := t.TempDir()
	pol := filepath.Join(t.TempDir(), "p.yaml")
	polData := "version: 1\nrules:\n  - id: a\n    action: ACCEPT\n  - id: b\n    action: DROP\n"
	if err := os.WriteFile(pol, []byte(polData), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-seed the store with an active v1 (one rule) to simulate a
	// running daemon.
	st, err := store.Open(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WritePolicy("1", []byte("version: 1\nrules:\n  - id: old\n    action: ACCEPT\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetActive("1"); err != nil {
		t.Fatal(err)
	}

	polFile := pol
	policyDir := storeDir
	opts := &cli.Options{
		Command:    "policy",
		Subcommand: "dry-run",
		Overrides: config.Overrides{
			PolicyFile: &polFile,
			PolicyDir:  &policyDir,
		},
	}
	snapshot, err := policy.Compile([]byte(polData))
	if err != nil {
		t.Fatal(err)
	}

	// Capture stdout.
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = runPolicyDryRun(opts, pol, []byte(polData), snapshot)
	w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"would publish version 2",
		"active: 1",
		"rules: 1 -> 2 (+1)",
		"default action: DROP -> DROP",
		"nothing written",
	} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}

	// The store must be untouched.
	active, err := st.Active()
	if err != nil {
		t.Fatal(err)
	}
	if active != "1" {
		t.Errorf("store active changed to %s, want 1", active)
	}
	versions, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Errorf("store versions = %v, want [1]", versions)
	}
}

// TestRunPolicyDryRunMissingStore verifies dry-run fails cleanly when
// the policy store does not exist (it must never create it).
func TestRunPolicyDryRunMissingStore(t *testing.T) {
	pol := filepath.Join(t.TempDir(), "p.yaml")
	polData := "version: 1\nrules: []\n"
	if err := os.WriteFile(pol, []byte(polData), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	opts := &cli.Options{
		Command:    "policy",
		Subcommand: "dry-run",
		Overrides: config.Overrides{
			PolicyFile: &pol,
			PolicyDir:  &missing,
		},
	}
	err := runPolicyDryRun(opts, pol, []byte(polData), nil)
	if err == nil {
		t.Fatal("dry-run succeeded with missing store")
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Errorf("dry-run created the store directory (write side effect)")
	}
}
