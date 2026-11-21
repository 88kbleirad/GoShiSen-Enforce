//go:build !unix

package safefile

import "os"

// openNoBlock falls back to a plain open on platforms without
// O_NONBLOCK semantics for FIFOs (a FIFO open will block there; the
// target platforms are Linux/OpenWrt).
func openNoBlock(path string) (*os.File, error) {
	return os.Open(path)
}
