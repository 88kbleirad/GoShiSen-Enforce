// Package safefile reads operator-supplied files with bounded, fail-fast
// semantics.
//
// Background: os.ReadFile on a non-EOF stream (a character device such as
// /dev/urandom, or a FIFO) never returns and grows memory without bound —
// a post-publish anomaly sweep of the v0.2.7 CLI measured ~1 GiB/s RSS
// before the process was killed. Reading a FIFO also blocks in open()
// before any content check can run.
//
// ReadBounded therefore:
//   - stats the path *before* opening, so a non-regular file (device,
//     FIFO, socket, directory) is rejected without ever blocking;
//   - caps the read at MaxBytes, so an endless regular file (or a
//     /proc-style file that reports size 0 but streams) cannot exhaust
//     memory.
package safefile

import (
	"fmt"
	"io"
)

// MaxBytes bounds a single configuration/policy read. The schemas are
// tiny (a 200k-rule benchmark policy is a few MB), so 16 MiB is far above
// any legitimate input and far below a memory-exhausting stream.
const MaxBytes = 16 << 20

// ReadBounded reads a regular file with a hard size cap. It returns an
// error for a missing path, a non-regular file, or content over MaxBytes.
//
// The regular-file check is performed on the *open* descriptor
// (f.Stat), not on a pre-open os.Stat: a pre-open stat plus a later open
// leaves a TOCTOU window in which a path could be swapped for a FIFO,
// making open() block forever. Opening first and validating the
// descriptor closes that window. (Symlinks are followed: the target's
// mode is what counts, which is the documented behavior for the config
// and policy paths.)
func ReadBounded(path string) ([]byte, error) {
	f, err := openNoBlock(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("%s: exceeds %d bytes", path, MaxBytes)
	}
	return data, nil
}
