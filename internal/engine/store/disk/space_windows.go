// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package disk

import (
	"golang.org/x/sys/windows"
)

func freeSpace(path string) int64 {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	var freeToCaller, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &free); err != nil {
		return 0
	}
	return int64(freeToCaller)
}
