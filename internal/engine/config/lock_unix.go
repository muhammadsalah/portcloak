// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package config

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// lockFile takes a non-blocking flock. It reports false, with no error, when the
// lock is simply held by somebody else — which is an answer, not a fault.
func lockFile(f *os.File, mode LockMode) (bool, error) {
	how := unix.LOCK_SH
	if mode == LockExclusive {
		how = unix.LOCK_EX
	}
	err := unix.Flock(int(f.Fd()), how|unix.LOCK_NB)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.EWOULDBLOCK):
		return false, nil
	default:
		return false, err
	}
}

func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
