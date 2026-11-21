package config

import "github.com/88kbleirad/GoShiSen-Enforce/internal/safefile"

// MaxFileBytes bounds the bytes read from operator-supplied files
// (config, policy). Kept for compatibility; the implementation lives in
// internal/safefile.
const MaxFileBytes = safefile.MaxBytes

// ReadFileBounded reads a regular file with a hard size cap. It rejects
// non-regular files before opening (so a FIFO/device cannot block) and
// caps the read at MaxFileBytes. See internal/safefile for details.
func ReadFileBounded(path string) ([]byte, error) {
	return safefile.ReadBounded(path)
}
