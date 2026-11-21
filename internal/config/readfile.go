package config

import (
	"fmt"
	"io"
	"os"
)

// maxFileBytes bounds the bytes read from operator-supplied files
// (config, policy). It is high enough for any realistic policy (the
// schema is tiny; a 200k-rule benchmark file is ~ few MB) and low
// enough to make an infinite source (a character device such as
// /dev/urandom) fail fast instead of ballooning memory (~1 GiB/s).
const MaxFileBytes = 16 << 20 // 16 MiB

// ReadFileBounded reads a regular file with a hard size cap. It is the
// safe replacement for os.ReadFile on operator input paths: a device
// (e.g. /dev/urandom) or other non-EOF stream is rejected after
// MaxFileBytes instead of being read forever.
func ReadFileBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("%s: exceeds %d bytes", path, MaxFileBytes)
	}
	return data, nil
}
