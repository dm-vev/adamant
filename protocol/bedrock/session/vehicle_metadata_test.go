package session

import (
	"context"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// vehicleKeys are the keys of the actor data of vehicles that the entity
// package once filled itself.
var vehicleKeys = []uint32{v2193.EntityDataKeyInteractText, v2193.EntityDataKeyIsBuoyant, v2193.EntityDataKeyBuoyancyData, v2193.EntityDataKeyControllingSeatIndex}

// boatBuoyancy is the buoyancy data BDS sends of boats.
const boatBuoyancy = "{\"apply_gravity\":true,\"base_buoyancy\":1.0,\"big_wave_probability\":0.02999999932944775,\"big_wave_speed\":10.0,\"drag_down_on_buoyancy_removed\":0.0,\"liquid_blocks\":[\"minecraft:water\",\"minecraft:flowing_water\"],\"simulate_waves\":true}"

// TestVehicleMetadataUnchanged holds the interact prompt, buoyancy and
// controlling seat of vehicles, empty and ridden, to the actor data frozen
// from the commit before the session derived them.
func TestVehicleMetadataUnchanged(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true}.New()
	defer w.Close()
	viewer := testSession("vehicles")
	viewer.br = world.DefaultBlockRegistry
	for _, c := range []struct {
		name   string
		id     string
		riders int
		want   map[uint32]any
	}{
		{"empty boat", "minecraft:boat", 0, map[uint32]any{0x4a: byte(0), 0x64: "action.interact.ride.boat", 0x77: byte(1), 0x79: boatBuoyancy}},
		{"boat with a rider", "minecraft:boat", 1, map[uint32]any{0x4a: byte(0), 0x64: "action.interact.ride.boat", 0x77: byte(1), 0x79: boatBuoyancy}},
		{"full boat", "minecraft:boat", 2, map[uint32]any{0x4a: byte(0), 0x77: byte(1), 0x79: boatBuoyancy}},
		{"chest boat", "minecraft:chest_boat", 0, map[uint32]any{0x4a: byte(0), 0x64: "action.interact.opencontainer", 0x77: byte(1), 0x79: boatBuoyancy}},
		{"full chest boat", "minecraft:chest_boat", 1, map[uint32]any{0x4a: byte(0), 0x64: "action.interact.opencontainer", 0x77: byte(1), 0x79: boatBuoyancy}},
		{"empty minecart", "minecraft:minecart", 0, map[uint32]any{0x64: "action.interact.ride.minecart"}},
		{"ridden minecart", "minecraft:minecart", 1, map[uint32]any{}},
		{"chest minecart", "minecraft:chest_minecart", 0, map[uint32]any{0x64: "action.interact.opencontainer"}},
		{"hopper minecart", "minecraft:hopper_minecart", 0, map[uint32]any{0x64: "action.interact.opencontainer"}},
		{"tnt minecart", "minecraft:tnt_minecart", 0, map[uint32]any{}},
		{"command block minecart", "minecraft:command_block_minecart", 0, map[uint32]any{0x64: "action.interact.ride.minecart"}},
	} {
		pos := mgl64.Vec3{8.5, 65, 8.5}
		h, err := entity.NewFromStructureNBT(map[string]any{"id": c.id}, world.EntitySpawnOpts{Position: pos, SpawnReason: "structure", SpawnFinalized: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Do(func(tx *world.Tx) {
			v := tx.AddEntity(h)
			for range c.riders {
				cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: pos}))
				if !v.(interface {
					Mount(*world.Tx, world.Entity) bool
				}).Mount(tx, cow) {
					t.Fatalf("%s: a cow did not mount", c.name)
				}
			}
			meta := viewer.parseEntityMetadata(v)
			got := map[uint32]any{}
			for _, k := range vehicleKeys {
				if val, ok := meta[k]; ok {
					got[k] = val
				}
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("%s: actor data %#v, want %#v", c.name, got, c.want)
			}
		}).Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}
