package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestFlintAndSteelPlacesFireAsJava follows FlintAndSteelItem.useOn and
// BaseFireBlock.canBePlacedAt: fire needs a sturdy floor or a neighbour that
// burns, soul sand gives soul fire, and a failed use costs no durability.
func TestFlintAndSteelPlacesFireAsJava(t *testing.T) {
	w := entityItemWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{8, 63, 8}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{10, 63, 8}, block.SoulSand{}, nil)
		tx.SetBlock(cube.Pos{12, 66, 8}, block.Glass{}, nil)
		start := mgl64.Vec3{9.5, 64, 10.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: start}.New(Type, Config{Position: start, GameMode: world.GameModeSurvival})).(*Player)
		flint := item.NewStack(item.FlintAndSteel{}, 1)
		p.SetHeldItems(flint, item.Stack{})

		p.UseItemOnBlock(cube.Pos{8, 63, 8}, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5})
		if f, ok := tx.Block(cube.Pos{8, 64, 8}).(block.Fire); !ok || f.Type != block.NormalFire() {
			t.Fatalf("no fire on stone: %v", tx.Block(cube.Pos{8, 64, 8}))
		}
		p.UseItemOnBlock(cube.Pos{10, 63, 8}, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5})
		if f, ok := tx.Block(cube.Pos{10, 64, 8}).(block.Fire); !ok || f.Type != block.SoulFire() {
			t.Fatalf("no soul fire on soul sand: %v", tx.Block(cube.Pos{10, 64, 8}))
		}
		held, _ := p.HeldItems()
		used := held.Durability()
		// Below glass there is nothing for fire to stand on or burn.
		p.UseItemOnBlock(cube.Pos{12, 66, 8}, cube.FaceDown, mgl64.Vec3{0.5, 0, 0.5})
		if _, ok := tx.Block(cube.Pos{12, 65, 8}).(block.Fire); ok {
			t.Fatal("fire hangs in the air below glass")
		}
		if held, _ := p.HeldItems(); held.Durability() != used {
			t.Fatal("a failed use cost durability")
		}
	})
}

// TestShearsFollowJava follows ShearsItem: shearing the tip of weeping
// vines stops them at age 25, and breaking short grass wears the shears.
func TestShearsFollowJava(t *testing.T) {
	w := entityItemWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{8, 70, 8}, block.Netherrack{}, nil)
		tx.SetBlock(cube.Pos{8, 69, 8}, block.WeepingVines{Age: 3}, nil)
		tx.SetBlock(cube.Pos{8, 68, 8}, block.WeepingVines{Age: 4}, nil)
		tx.SetBlock(cube.Pos{10, 63, 8}, block.Grass{}, nil)
		tx.SetBlock(cube.Pos{10, 64, 8}, block.ShortGrass{}, nil)
		start := mgl64.Vec3{9.5, 66, 9.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: start}.New(Type, Config{Position: start, GameMode: world.GameModeSurvival})).(*Player)
		p.SetHeldItems(item.NewStack(item.Shears{}, 1), item.Stack{})
		p.UseItemOnBlock(cube.Pos{8, 69, 8}, cube.FaceSouth, mgl64.Vec3{0.5, 0.5, 1})
		if v := tx.Block(cube.Pos{8, 69, 8}).(block.WeepingVines); v.Age != 3 {
			t.Fatalf("shears changed the vines above the tip to age %v", v.Age)
		}
		p.UseItemOnBlock(cube.Pos{8, 68, 8}, cube.FaceSouth, mgl64.Vec3{0.5, 0.5, 1})
		if v := tx.Block(cube.Pos{8, 68, 8}).(block.WeepingVines); v.Age != 25 {
			t.Fatalf("sheared tip age %v, want 25", v.Age)
		}
		held, _ := p.HeldItems()
		before := held.Durability()
		p.BreakBlock(cube.Pos{10, 64, 8})
		if held, _ := p.HeldItems(); held.Durability() != before-1 {
			t.Fatalf("shears durability %v after cutting grass, want %v", held.Durability(), before-1)
		}
	})
}
