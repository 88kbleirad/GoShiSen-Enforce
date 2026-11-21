package safefile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBoundedRejectsNonRegular(t *testing.T) {
	for _, p := range []string{"/dev/urandom", "/dev/zero"} {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if _, err := ReadBounded(p); err == nil {
			t.Fatalf("ReadBounded(%s) succeeded; want error", p)
		}
	}
	dir := t.TempDir()
	if _, err := ReadBounded(dir); err == nil {
		t.Fatal("ReadBounded(dir) succeeded; want error")
	}
	// A FIFO must be rejected by the pre-open stat, not by blocking.
	fifo := filepath.Join(dir, "fifo")
	if err := execCmd("mkfifo", fifo); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := ReadBounded(fifo); err == nil {
			t.Error("ReadBounded(fifo) succeeded; want error")
		}
	}()
	select {
	case <-done:
	case <-timeAfter():
		t.Fatal("ReadBounded(fifo) blocked; pre-open stat missing")
	}
}

func TestReadBoundedCapAndRoundtrip(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, make([]byte, MaxBytes+1024), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBounded(big); err == nil {
		t.Fatal("ReadBounded(oversize) succeeded; want error")
	}
	ok := filepath.Join(dir, "ok")
	payload := strings.Repeat("x", 4096)
	if err := os.WriteFile(ok, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadBounded(ok)
	if err != nil {
		t.Fatalf("ReadBounded(ok): %v", err)
	}
	if string(got) != payload {
		t.Fatal("payload mismatch")
	}
}
