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
	// A FIFO must be rejected without blocking: the open itself is
	// O_NONBLOCK, so no writer is needed and no window exists in which a
	// swapped-in FIFO could block the reader.
	fifo := filepath.Join(dir, "fifo")
	if err := execCmd("mkfifo", fifo); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ReadBounded(fifo)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("ReadBounded(fifo) succeeded; want error")
		}
	case <-timeAfter():
		t.Fatal("ReadBounded(fifo) blocked; open is not O_NONBLOCK")
	}
}

// TestReadBoundedChoiceSwapSimulatesTOCTOU documents the race the
// O_NONBLOCK open closes: a path that is a regular file at check time
// and a FIFO at open time must still not block the reader.
func TestReadBoundedSwapToFIFODoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "target")
	if err := os.WriteFile(p, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Sanity: the regular file reads fine.
	if _, err := ReadBounded(p); err != nil {
		t.Fatalf("regular read: %v", err)
	}
	// Replace with a FIFO (the attacker's swap) and read again.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := execCmd("mkfifo", p); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ReadBounded(p)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("swapped FIFO accepted; want error")
		}
	case <-timeAfter():
		t.Fatal("swapped FIFO blocked the reader")
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

// TestReadBoundedProcFile pins the size-independence of ReadBounded: procfs
// reports st_size 0 for regular files whose content is non-empty (e.g.
// /proc/version, /proc/net/netfilter/nfnetlink_queue). A size-based
// implementation would return zero bytes here.
func TestReadBoundedProcFile(t *testing.T) {
	const path = "/proc/version"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no %s: %v", path, err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 0 {
		t.Skip("procfs reports a size here; test would not exercise the zero-size case")
	}
	data, err := ReadBounded(path)
	if err != nil {
		t.Fatalf("ReadBounded(%s): %v", path, err)
	}
	if len(data) == 0 {
		t.Fatalf("ReadBounded(%s) returned 0 bytes although st_size=%d; must not depend on st_size", path, st.Size())
	}
}
