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

// TestGlassBottleFillsFromWaterSource follows BottleItem.use: the bottle
// fills from the water source the player looks at, even clicking into the
// air.
func TestGlassBottleFillsFromWaterSource(t *testing.T) {
	w := entityItemWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := 0; x < 16; x++ {
			for z := 0; z < 16; z++ {
				tx.SetBlock(cube.Pos{x, 62, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 63, z}, block.Water{Still: true, Depth: 8}, nil)
			}
		}
		start := mgl64.Vec3{8.5, 64, 8.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: start}.New(Type, Config{Position: start, Rotation: cube.Rotation{0, 80}, GameMode: world.GameModeSurvival})).(*Player)
		p.SetHeldItems(item.NewStack(item.GlassBottle{}, 2), item.Stack{})
		p.UseItem()
		held, _ := p.HeldItems()
		if held.Count() != 1 {
			t.Fatalf("bottles left after filling one: %v", held.Count())
		}
		found := false
		for _, it := range p.Inventory().Items() {
			if potion, ok := it.Item().(item.Potion); ok && potion.Type.JavaID() == "minecraft:water" {
				found = true
			}
		}
		if !found {
			t.Fatal("no water bottle in the inventory")
		}
	})
}

// bucketTestPlayer adds a survival player looking straight down at a stone
// floor at y 62 in the dimension passed.
func bucketTestPlayer(t *testing.T, dim world.Dimension, f func(tx *world.Tx, p *Player)) {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, RandomTickSpeed: -1, Dim: dim}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := 0; x < 16; x++ {
			for z := 0; z < 16; z++ {
				tx.SetBlock(cube.Pos{x, 62, z}, block.Stone{}, nil)
			}
		}
		start := mgl64.Vec3{8.5, 63, 8.5}
		f(tx, tx.AddEntity(world.EntitySpawnOpts{Position: start}.New(Type, Config{Position: start, Rotation: cube.Rotation{0, 90}, GameMode: world.GameModeSurvival})).(*Player))
	})
}

// TestBucketsFollowJava follows BucketItem.use: water empties beside the
// block looked at, an empty bucket picks the source up again, and water
// evaporates in the Nether.
func TestBucketsFollowJava(t *testing.T) {
	water := item.Bucket{Content: item.LiquidBucketContent(block.Water{Depth: 8, Still: true})}
	bucketTestPlayer(t, world.Overworld, func(tx *world.Tx, p *Player) {
		p.SetHeldItems(item.NewStack(water, 1), item.Stack{})
		p.UseItem()
		if l, ok := tx.Liquid(cube.Pos{8, 63, 8}); !ok || l.LiquidDepth() != 8 {
			t.Fatal("water bucket placed no source on the floor")
		}
		if held, _ := p.HeldItems(); !held.Comparable(item.NewStack(item.Bucket{}, 1)) {
			t.Fatalf("held after emptying: %v", held)
		}
		p.UseItem()
		if _, ok := tx.Liquid(cube.Pos{8, 63, 8}); ok {
			t.Fatal("empty bucket left the source")
		}
		if held, _ := p.HeldItems(); !held.Comparable(item.NewStack(water, 1)) {
			t.Fatalf("held after filling: %v", held)
		}
	})
	bucketTestPlayer(t, world.Nether, func(tx *world.Tx, p *Player) {
		p.SetHeldItems(item.NewStack(water, 1), item.Stack{})
		p.UseItem()
		if _, ok := tx.Liquid(cube.Pos{8, 63, 8}); ok {
			t.Fatal("water placed in the Nether")
		}
		if held, _ := p.HeldItems(); !held.Comparable(item.NewStack(item.Bucket{}, 1)) {
			t.Fatalf("held after evaporating: %v", held)
		}
	})
}

// TestFishBucketSpawnsFishInWater follows MobBucketItem: the water is
// emptied like a water bucket's and the fish spawns standing in it.
func TestFishBucketSpawnsFishInWater(t *testing.T) {
	bucketTestPlayer(t, world.Overworld, func(tx *world.Tx, p *Player) {
		p.SetHeldItems(item.NewStack(item.FishBucket{MobBucket: item.MobBucket{Entity: "minecraft:cod"}}, 1), item.Stack{})
		p.UseItem()
		if _, ok := tx.Liquid(cube.Pos{8, 63, 8}); !ok {
			t.Fatal("cod bucket placed no water")
		}
		var cod world.Entity
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:cod" {
				cod = e
			}
		}
		if cod == nil || cod.Position() != (mgl64.Vec3{8.5, 63, 8.5}) {
			t.Fatalf("cod %v, want one at 8.5 63 8.5", cod)
		}
		if _, ok := any(item.FishBucket{}).(item.Consumable); ok {
			t.Fatal("fish buckets can be eaten")
		}
	})
}

// TestCushionPlacementFollowsJava follows CushionItem.useOn: the cushion
// faces the player's nearest cardinal direction, and inside a cauldron it
// rests on the cauldron's floor, found on its collision shape.
func TestCushionPlacementFollowsJava(t *testing.T) {
	w := entityItemWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{8, 63, 8}, block.Cauldron{}, nil)
		start := mgl64.Vec3{8.5, 64.5, 9.6}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: start}.New(Type, Config{Position: start, Rotation: cube.Rotation{100, 70}, GameMode: world.GameModeSurvival})).(*Player)
		p.SetHeldItems(item.NewStack(item.Cushion{Colour: item.ColourRed()}, 1), item.Stack{})
		// The click lands on the floor inside the cauldron, a quarter block up.
		p.UseItemOnBlock(cube.Pos{8, 63, 8}, cube.FaceUp, mgl64.Vec3{0.5, 0.25, 0.5})
		var cushion world.Entity
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:cushion" {
				cushion = e
			}
		}
		if cushion == nil {
			t.Fatal("no cushion placed in the cauldron")
		}
		if y := cushion.Position()[1]; math.Abs(y-63.25) > 1e-6 {
			t.Fatalf("cushion at y %v, want on the cauldron floor at 63.25", y)
		}
		if yaw := cushion.Rotation().Yaw(); yaw != 90 {
			t.Fatalf("cushion faces %v, want 90 for a player facing 100", yaw)
		}
	})
}
