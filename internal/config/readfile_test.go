package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadFileBoundedRejectsNonRegular reproduces the anomaly: a policy
// file pointing at a character device (/dev/urandom) previously made
// os.ReadFile loop forever and balloon memory (~1 GiB/s). The bounded
// reader must reject non-regular files fast. (Regression from the
// v0.2.7 post-publish anomaly sweep.)
func TestReadFileBoundedRejectsNonRegular(t *testing.T) {
	if _, err := os.Stat("/dev/urandom"); err != nil {
		t.Skip("/dev/urandom not available")
	}
	if _, err := ReadFileBounded("/dev/urandom"); err == nil {
		t.Fatal("ReadFileBounded(/dev/urandom) succeeded; want error")
	}
	if _, err := ReadFileBounded("/dev/zero"); err == nil {
		t.Fatal("ReadFileBounded(/dev/zero) succeeded; want error")
	}
	// A directory is also not a regular file.
	dir := t.TempDir()
	if _, err := ReadFileBounded(dir); err == nil {
		t.Fatal("ReadFileBounded(dir) succeeded; want error")
	}
}

// TestReadFileBoundedCap verifies the byte cap and the regular round-trip.
func TestReadFileBoundedCap(t *testing.T) {
	dir := t.TempDir()
	// A file larger than the cap must be rejected (limit reader + size).
	p := filepath.Join(dir, "big")
	if err := os.WriteFile(p, make([]byte, MaxFileBytes+1024), 0o600); err != nil {
		t.Fatalf("write big: %v", err)
	}
	if _, err := ReadFileBounded(p); err == nil {
		t.Fatal("ReadFileBounded(oversize) succeeded; want error")
	}
	// A regular file under the cap must round-trip.
	q := filepath.Join(dir, "ok")
	payload := strings.Repeat("x", 4096)
	if err := os.WriteFile(q, []byte(payload), 0o600); err != nil {
		t.Fatalf("write ok: %v", err)
	}
	got, err := ReadFileBounded(q)
	if err != nil {
		t.Fatalf("ReadFileBounded(ok): %v", err)
	}
	if string(got) != payload {
		t.Fatal("ReadFileBounded(ok) payload mismatch")
	}
}
