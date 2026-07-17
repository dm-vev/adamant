package entity

import (
	"github.com/df-mc/dragonfly/server/world"
	"testing"
)

func TestCowNBT(t *testing.T) {
	data := &world.EntityData{}
	CowType.DecodeNBT(map[string]any{"Health": float32(0), "MaxHealth": float32(12), "Variant": int32(2)}, data)

	nbt := CowType.EncodeNBT(data)
	if nbt["Health"] != float32(0) || nbt["MaxHealth"] != float32(12) || nbt["Variant"] != int32(2) {
		t.Fatalf("unexpected cow NBT: %#v", nbt)
	}
}

func TestCowConfigDefaults(t *testing.T) {
	cow := CowConfig{}.New()
	if cow.health.Health() != 10 || cow.health.MaxHealth() != 10 {
		t.Fatalf("unexpected default health: %v/%v", cow.health.Health(), cow.health.MaxHealth())
	}
}
