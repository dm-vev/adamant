//go:build redstone_bell_support_audit

package session

import (
	"fmt"
	"testing"
)

// TestBellSupportUpdatesMatchNativePackets preserves the unfinished wire audit.
// State transitions are covered by the ordinary TestBellSupportStatesMatchBDS;
// this diagnostic retains the native intermediate states, actor notifications
// and removal flags that the current publication queue does not yet preserve.
func TestBellSupportUpdatesMatchNativePackets(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprintf("split%t", split), func(t *testing.T) { testBellSupportsNative(t, true, split) })
	}
}
