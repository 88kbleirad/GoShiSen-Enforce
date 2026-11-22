package packet

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The queue-stats path is operator-visible and previously used an unbounded
// os.ReadFile: a FIFO blocked in open() forever, a device streamed without
// EOF. ReadQueueStats must fail closed, promptly, for every non-regular file
// and for oversized input (same defect class as the v0.2.7 anomaly sweep).

// 9 numeric fields, queue 42 (same shape as the /proc line)
const qstatsFixture = "  42   12345   2   2   65535   12   3   4567   1\n"

func TestReadQueueStatsParsesRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nfnetlink_queue")
	if err := os.WriteFile(path, []byte(qstatsFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := ReadQueueStats(path, 42)
	if err != nil {
		t.Fatalf("regular file: %v", err)
	}
	if st.QueueNum != 42 {
		t.Fatalf("queue num = %d, want 42", st.QueueNum)
	}
}

func TestReadQueueStatsMissingFileFailsClosed(t *testing.T) {
	if _, err := ReadQueueStats(filepath.Join(t.TempDir(), "nope"), 42); err == nil {
		t.Fatal("missing file must return an error (callers degrade silently)")
	}
}

func TestReadQueueStatsRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ReadQueueStats(path, 42)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO must be rejected")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadQueueStats blocked on a FIFO (unbounded read regression)")
	}
}

func TestReadQueueStatsRejectsDirectory(t *testing.T) {
	if _, err := ReadQueueStats(t.TempDir(), 42); err == nil {
		t.Fatal("directory must be rejected")
	}
}

func TestReadQueueStatsRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// one byte over the cap, written sparsely enough to stay cheap
	if err := f.Truncate(int64(16<<20) + 1); err != nil {
		f.Close()
		t.Skipf("truncate unsupported: %v", err)
	}
	f.Close()
	if _, err := ReadQueueStats(path, 42); err == nil {
		t.Fatal("file over the size cap must be rejected")
	}
}
