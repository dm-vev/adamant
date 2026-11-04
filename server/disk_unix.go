//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package server

import (
	"os"
	"syscall"
)

// diskFree returns the bytes free to the server on the file system of the
// folder at path. ok is false if it cannot tell.
func diskFree(path string) (free uint64, ok bool, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true, nil
}

// sameDevice reports whether the files at a and b lie on the same file
// system, where hard links between them work.
func sameDevice(a, b string) bool {
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	sa, okA := fa.Sys().(*syscall.Stat_t)
	sb, okB := fb.Sys().(*syscall.Stat_t)
	return okA && okB && uint64(sa.Dev) == uint64(sb.Dev)
}
