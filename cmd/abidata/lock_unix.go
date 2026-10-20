//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func lockFile(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return errLocked
		}
		return fmt.Errorf("lock data dir: %w", err)
	}
	return nil
}

func unlockFile(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
