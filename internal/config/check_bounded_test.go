package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Options.Check runs on daemon startup before the policy is read. It used a
// plain os.Open on the operator-supplied policy path, which blocks forever on
// a FIFO and accepts a directory; these tests pin the bounded, fail-fast
// behaviour (same defect class as the v0.2.7 anomaly sweep).

func checkWithin(t *testing.T, o *Options, limit time.Duration) error {
	t.Helper()
	type res struct{ err error }
	ch := make(chan res, 1)
	go func() { ch <- res{o.Check()} }()
	select {
	case r := <-ch:
		return r.err
	case <-time.After(limit):
		t.Fatalf("Options.Check blocked on the policy path for > %v", limit)
		return nil
	}
}

func TestCheckRejectsFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	err := checkWithin(t, &Options{File: File{PolicyFile: path}}, 5*time.Second)
	if err == nil {
		t.Fatal("Check accepted a FIFO policy path")
	}
}

func TestCheckRejectsDirectory(t *testing.T) {
	if err := checkWithin(t, &Options{File: File{PolicyFile: t.TempDir()}}, 5*time.Second); err == nil {
		t.Fatal("Check accepted a directory as policy_file")
	}
}

func TestCheckRejectsDevice(t *testing.T) {
	if _, err := os.Stat("/dev/null"); err != nil {
		t.Skip("no /dev/null")
	}
	if err := checkWithin(t, &Options{File: File{PolicyFile: "/dev/null"}}, 5*time.Second); err == nil {
		t.Fatal("Check accepted a character device as policy_file")
	}
}

func TestCheckAcceptsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(path, []byte("rules: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkWithin(t, &Options{File: File{PolicyFile: path}}, 5*time.Second); err != nil {
		t.Fatalf("Check rejected a regular policy file: %v", err)
	}
}

func TestCheckRejectsOverLimitFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.yaml")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate((16 << 20) + 1); err != nil {
		f.Close()
		t.Skipf("truncate unsupported: %v", err)
	}
	f.Close()
	if err := checkWithin(t, &Options{File: File{PolicyFile: path}}, 5*time.Second); err == nil {
		t.Fatal("Check accepted a policy file over the 16 MiB cap")
	}
}
