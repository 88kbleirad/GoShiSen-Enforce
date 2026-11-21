//go:build unix

package safefile

import (
	"os"
	"syscall"
)

// openNoBlock opens path without blocking on a FIFO (O_NONBLOCK). The
// caller validates the descriptor type before reading; for regular files
// O_NONBLOCK is ignored and reads behave normally.
func openNoBlock(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
