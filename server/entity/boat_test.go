package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
)

func TestBoatTypeEncodeEntity(t *testing.T) {
	if got, want := entity.BoatType.EncodeEntity(), "minecraft:boat"; got != want {
		t.Fatalf("BoatType.EncodeEntity() = %q, want %q", got, want)
	}
}

func TestChestBoatTypeEncodeEntity(t *testing.T) {
	if got, want := entity.ChestBoatType.EncodeEntity(), "minecraft:chest_boat"; got != want {
		t.Fatalf("ChestBoatType.EncodeEntity() = %q, want %q", got, want)
	}
}
