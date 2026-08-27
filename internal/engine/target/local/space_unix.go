// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package local

import "golang.org/x/sys/unix"

func freeSpace(path string) int64 {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0
	}
	return int64(st.Bavail) * int64(st.Bsize)
}
