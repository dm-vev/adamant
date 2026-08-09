package entity_test

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestEndCrystalNetworkEncodeEntity(t *testing.T) {
	if got, want := entity.EndCrystalType.NetworkEncodeEntity(), "minecraft:ender_crystal"; got != want {
		t.Fatalf("NetworkEncodeEntity() returned %q, want %q", got, want)
	}
}

func TestEndCrystalLegacyBeamTargetNBTMigration(t *testing.T) {
	data := new(world.EntityData)
	entity.EndCrystalType.DecodeNBT(map[string]any{
		"BeamTargetX": int32(12),
		"BeamTargetY": int32(34),
		"BeamTargetZ": int32(-56),
	}, data)

	target, ok := data.Data.(*entity.EndCrystalBehaviour).BeamTarget()
	if want := (mgl64.Vec3{12, 34, -56}); !ok || target != want {
		t.Fatalf("legacy beam target = %v, %t; want %v, true", target, ok, want)
	}
	nbt := entity.EndCrystalType.EncodeNBT(data)
	for key, want := range map[string]any{"BlockTargetX": int32(12), "BlockTargetY": int32(34), "BlockTargetZ": int32(-56)} {
		if got := nbt[key]; !reflect.DeepEqual(got, want) {
			t.Fatalf("migrated %s = %v, want %v", key, got, want)
		}
	}
	for _, key := range []string{"BeamTargetX", "BeamTargetY", "BeamTargetZ"} {
		if _, ok := nbt[key]; ok {
			t.Fatalf("new NBT write retained legacy key %s", key)
		}
	}
}
