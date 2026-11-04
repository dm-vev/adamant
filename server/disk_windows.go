//go:build windows

package server

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// diskFree returns the bytes free to the server on the volume of the folder
// at path. ok is false if it cannot tell.
func diskFree(path string) (free uint64, ok bool, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false, err
	}
	var avail, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &totalFree); err != nil {
		return 0, false, err
	}
	return avail, true, nil
}

// sameDevice reports whether the files at a and b lie on the same volume,
// where hard links between them work.
func sameDevice(a, b string) bool {
	a, errA := filepath.Abs(a)
	b, errB := filepath.Abs(b)
	return errA == nil && errB == nil && strings.EqualFold(filepath.VolumeName(a), filepath.VolumeName(b))
}
