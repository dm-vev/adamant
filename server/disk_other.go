//go:build !(linux || darwin || freebsd || openbsd || netbsd || dragonfly || windows)

package server

// diskFree cannot tell the bytes free on this system: ok is false, and
// backups are made without the check of MinFreeSpace.
func diskFree(string) (free uint64, ok bool, err error) { return 0, false, nil }

// sameDevice reports false: on this system the server cannot tell.
func sameDevice(string, string) bool { return false }
