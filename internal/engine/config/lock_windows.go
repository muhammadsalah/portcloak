// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package config

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes a non-blocking region lock over the whole file.
//
// LockFileEx is the Windows equivalent of flock for this purpose, including the
// property the choice rests on: the lock is released when the handle closes,
// and the handle closes when the process dies.
func lockFile(f *os.File, mode LockMode) (bool, error) {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if mode == LockExclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	// A whole-file range. The length is given as 0xFFFFFFFF/0xFFFFFFFF rather
	// than the file's size because the file grows: the holder writes its
	// description after taking the lock.
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0,
		0xFFFFFFFF, 0xFFFFFFFF, new(windows.Overlapped))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, windows.ERROR_LOCK_VIOLATION), errors.Is(err, windows.ERROR_IO_PENDING):
		return false, nil
	default:
		return false, err
	}
}

func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0,
		0xFFFFFFFF, 0xFFFFFFFF, new(windows.Overlapped))
}
