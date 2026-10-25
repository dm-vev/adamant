package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestMobAirIsSaved checks the air a mob has left is saved as Air and read
// back, as LivingEntity saves it, so a mob under water does not get a full
// breath back from a reload.
func TestMobAirIsSaved(t *testing.T) {
	var data world.EntityData
	CowConfig{}.Apply(&data)
	ai := data.Data.(mobAIBehaviour).mobAI()
	ai.air, ai.airSet = 120, true
	m := encodeSharedNBT(&data)
	if m["Air"] != int16(120) {
		t.Fatalf("cow saved Air %v, want 120", m["Air"])
	}
	var loaded world.EntityData
	CowConfig{}.Apply(&loaded)
	decodeSharedNBT(&loaded, m)
	if got := loaded.Data.(mobAIBehaviour).mobAI(); !got.airSet || got.air != 120 {
		t.Fatalf("cow loaded with air %d (set %v), want 120", got.air, got.airSet)
	}

	var fresh world.EntityData
	CowConfig{}.Apply(&fresh)
	if m := encodeSharedNBT(&fresh); m["Air"] != int16(maxMobAirSupply) {
		t.Fatalf("fresh cow saved Air %v, want %d", m["Air"], maxMobAirSupply)
	}
}
