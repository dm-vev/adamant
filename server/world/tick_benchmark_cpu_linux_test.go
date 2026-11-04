package world

import (
	"syscall"
	"time"
)

// processCPUTime returns the CPU time the process used, user and system,
// which, unlike the time a benchmark took, does not count the time it
// waited for a CPU on a busy machine.
func processCPUTime() time.Duration {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
