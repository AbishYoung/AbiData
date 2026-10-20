//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func lockFile(f *os.File) error {
	var ol windows.Overlapped
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&ol,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return errLocked
		}
		return fmt.Errorf("lock data dir: %w", err)
	}
	return nil
}

func unlockFile(f *os.File) {
	var ol windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}
