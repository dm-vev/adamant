package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
)

func TestEndCrystalNetworkEncodeEntity(t *testing.T) {
	if got, want := entity.EndCrystalType.NetworkEncodeEntity(), "minecraft:ender_crystal"; got != want {
		t.Fatalf("NetworkEncodeEntity() returned %q, want %q", got, want)
	}
}
