package entity_test

import (
	"testing"

	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSheepDyeingFollowsJava follows DyeItem.interactLivingEntity: a sheep
// takes a dye of another colour, but not when sheared.
func TestSheepDyeingFollowsJava(t *testing.T) {
	t.Parallel()
	w := traderTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		s := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}}.New(entity.SheepType, entity.SheepConfig{})).(*entity.Sheep)
		p := addSurvivalPlayer(tx, mgl64.Vec3{2.5, 64, .5})
		p.SetHeldItems(item.NewStack(item.Dye{Colour: item.ColourWhite()}, 1), item.Stack{})
		if ctx := (&item.UseContext{}); s.Interact(tx, p, ctx) {
			t.Fatal("a white sheep took white dye")
		}
		p.SetHeldItems(item.NewStack(item.Dye{Colour: item.ColourRed()}, 1), item.Stack{})
		if ctx := (&item.UseContext{}); !s.Interact(tx, p, ctx) || ctx.CountSub != 1 {
			t.Fatal("a white sheep took no red dye")
		}
		s.Shear(tx)
		p.SetHeldItems(item.NewStack(item.Dye{Colour: item.ColourBlue()}, 1), item.Stack{})
		if ctx := (&item.UseContext{}); s.Interact(tx, p, ctx) {
			t.Fatal("a sheared sheep took dye")
		}
	})
}
