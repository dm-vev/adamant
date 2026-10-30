package block

import "testing"

// Compare visible hook/string state and electrical output independently, including
// rebuilding a powered line before a replacement producer has been registered.
func TestTripwireLifecycleMatchesBDS(t *testing.T) {
	testSculkNativeContacts(t, "bedrock_1_26_51_hook_lifecycle.json", 72)
}

func TestTripwireHookPermutationsMatchBDS(t *testing.T) {
	testSculkNativeContacts(t, "bedrock_1_26_51_hook_permutations.json", 64)
}
