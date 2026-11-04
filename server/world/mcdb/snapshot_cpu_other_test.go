//go:build !linux

package mcdb

import "time"

// processCPUTime returns 0: the CPU time of the process is measured on
// Linux only.
func processCPUTime() time.Duration { return 0 }
