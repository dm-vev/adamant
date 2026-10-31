package session

import (
	"fmt"
	"testing"
)

// TestBellSupportUpdatesMatchNativePackets retains every intermediate state,
// actor notification and removal flag, including split flushes and backlog.
func TestBellSupportUpdatesMatchNativePackets(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprintf("split%t", split), func(t *testing.T) { testBellSupportsNative(t, true, split, false) })
	}
	t.Run("backlog", func(t *testing.T) { testBellSupportsNative(t, true, false, true) })
}
