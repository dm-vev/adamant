package version

import (
	"slices"
	"testing"
)

// TestMinecraftNumbers checks the numbers of the release as the world format
// stores them.
func TestMinecraftNumbers(t *testing.T) {
	if got, want := MinecraftNumbers(), []int32{1, 26, 50, 0, 0}; !slices.Equal(got, want) {
		t.Fatalf("MinecraftNumbers() = %v, want %v", got, want)
	}
}
