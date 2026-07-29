//go:build windows

package cachepolicy

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func diskFreeBytes(path string) int64 {
	root := filepath.VolumeName(path) + `\`
	pointer, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(pointer, &available, nil, nil); err != nil {
		return 0
	}
	if available > uint64(^uint64(0)>>1) {
		return int64(^uint64(0) >> 1)
	}
	return int64(available)
}
