package block

import (
	"os"
	"testing"
)

// This diagnostic records an unfinished audit, not a passing regression test.
// Native hook producer registration still differs when a powered line is rebuilt.
func TestTripwireLifecycleMatchesBDS(t *testing.T) {
	if os.Getenv("ADAMANT_REDSTONE_LIFECYCLE_AUDIT") != "1" {
		t.Skip("unfinished Bedrock hook lifecycle audit; set ADAMANT_REDSTONE_LIFECYCLE_AUDIT=1 to reproduce known mismatches (docs/redstone-audit.md)")
	}
	testSculkNativeContacts(t, "bedrock_1_26_51_hook_lifecycle.json", 72)
}
