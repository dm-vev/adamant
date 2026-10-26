package player

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func entityItemWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// TestArmourStandPlacementFollowsJava follows ArmorStandItem.useOn: the stand
// stands on the floor of the block above the clicked one, turned to the
// nearest 45 degrees facing the player, named after the stack.
func TestArmourStandPlacementFollowsJava(t *testing.T) {
	w := entityItemWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{8, 64, 8}, block.Stone{}, nil)
		start := mgl64.Vec3{8.5, 65, 11.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: start}.New(Type, Config{Position: start, Rotation: cube.Rotation{170, 60}, GameMode: world.GameModeSurvival})).(*Player)
		p.SetHeldItems(item.NewStack(item.ArmourStand{}, 1).WithCustomName("Bob"), item.Stack{})
		p.UseItemOnBlock(cube.Pos{8, 64, 8}, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5})
		var stand world.Entity
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:armor_stand" {
				stand = e
			}
		}
		if stand == nil {
			t.Fatal("no armour stand placed")
		}
		// wrapDegrees(170 - 180) = -10, rounded to the nearest 45 is 0.
		if pos := stand.Position(); pos != (mgl64.Vec3{8.5, 65, 8.5}) || math.Abs(stand.Rotation().Yaw()) > 1e-9 {
			t.Fatalf("armour stand at %v facing %v, want 8.5 65 8.5 facing 0", pos, stand.Rotation().Yaw())
		}
		if n, ok := stand.(interface{ NameTag() string }); ok && n.NameTag() != "Bob" {
			t.Fatalf("armour stand named %q, want Bob", n.NameTag())
		}
		if held, _ := p.HeldItems(); !held.Empty() {
			t.Fatalf("armour stand item kept: %v", held)
		}
	})
}

// TestBoatPlacedOnWaterSurface follows BoatItem.use: the boat goes where the
// player's view meets the water surface, facing as the player does.
func TestBoatPlacedOnWaterSurface(t *testing.T) {
	w := entityItemWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := 0; x < 16; x++ {
			for z := 0; z < 16; z++ {
				tx.SetBlock(cube.Pos{x, 62, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 63, z}, block.Water{Still: true, Depth: 8}, nil)
			}
		}
		start := mgl64.Vec3{8.5, 64, 8.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: start}.New(Type, Config{Position: start, Rotation: cube.Rotation{30, 60}, GameMode: world.GameModeSurvival})).(*Player)
		p.SetHeldItems(item.NewStack(item.Boat{Variant: item.BoatVariantFromInt(0)}, 1), item.Stack{})
		p.UseItem()
		var boat world.Entity
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:boat" {
				boat = e
			}
		}
		if boat == nil {
			t.Fatal("no boat placed")
		}
		// The water's surface is at 63 + 8/9 below a source without water above.
		if pos := boat.Position(); math.Abs(pos[1]-(63+8.0/9)) > 1e-6 || math.Abs(boat.Rotation().Yaw()-30) > 1e-9 {
			t.Fatalf("boat at %v facing %v, want on the water surface facing 30", pos, boat.Rotation().Yaw())
		}
	})
}
