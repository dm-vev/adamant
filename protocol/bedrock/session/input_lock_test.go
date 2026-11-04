package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/player/input"
)

// TestBedrockLocks holds every input lock to the bit of
// UpdateClientInputLocks it was before locks had values of their own: the
// constants of the model, in the order of input.All.
func TestBedrockLocks(t *testing.T) {
	want := []uint32{2, 4, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096}
	all := input.All()
	if len(all) != len(want) {
		t.Fatalf("input.All() has %d locks, want %d", len(all), len(want))
	}
	for i, l := range all {
		if got := bedrockLock(l); got != want[i] {
			t.Errorf("bedrockLock(input.All()[%d]) = %d, want %d", i, got, want[i])
		}
	}
	if got := bedrockLock(input.Lock{}); got != 0 {
		t.Errorf("bedrockLock of the zero Lock = %d, want 0", got)
	}
}
