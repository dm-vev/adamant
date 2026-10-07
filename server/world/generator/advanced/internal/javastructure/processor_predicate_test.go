package javastructure

import (
	"testing"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
)

func TestRuleTestRandomConsumptionAndStateMatching(t *testing.T) {
	state := world.BlockState{Name: "minecraft:oak_stairs", Properties: map[string]any{"facing": "north"}}
	mismatchRandom := java26.NewLegacyRandom(911)
	wantMismatchRandom := java26.NewLegacyRandom(911)
	if testStateRule(map[string]any{"predicate_type": "minecraft:random_blockstate_match", "block_state": map[string]any{
		"Name": "minecraft:oak_stairs", "Properties": map[string]any{"facing": "south"},
	}, "probability": float64(1)}, state, [3]int32{}, &mismatchRandom) {
		t.Fatal("random state predicate matched a different Java state")
	}
	if got, want := mismatchRandom.NextFloat(), wantMismatchRandom.NextFloat(); got != want {
		t.Fatalf("state-mismatch predicate consumed random: following float %v, want %v", got, want)
	}

	matchRandom := java26.NewLegacyRandom(911)
	wantMatchRandom := java26.NewLegacyRandom(911)
	if !testStateRule(map[string]any{"predicate_type": "minecraft:random_blockstate_match", "block_state": map[string]any{
		"Name": "oak_stairs", "Properties": map[string]any{"facing": "north"},
	}, "probability": float64(1)}, state, [3]int32{}, &matchRandom) {
		t.Fatal("random state predicate failed to match an equivalent Java state")
	}
	wantMatchRandom.NextFloat()
	if got, want := matchRandom.NextFloat(), wantMatchRandom.NextFloat(); got != want {
		t.Fatalf("matching random state predicate consumed wrong amount: following float %v, want %v", got, want)
	}
}
