package block

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// placingUser places blocks as a player does, taking one item for each.
type placingUser struct {
	chiseledBookshelfUser
	tx *world.Tx
}

func (u *placingUser) PlaceBlock(pos cube.Pos, b world.Block, ctx *item.UseContext) {
	u.tx.SetBlock(pos, b, nil)
	ctx.SubtractFromCount(1)
}

// TestGrowingPlantsNeedSupport checks GrowingPlantBlock placement: glow
// berries plant cave vines only under a sturdy face or more vines, weeping
// vines hang the same way and twisting vines stand on a block. A berry-
// bearing tip continued by new vines becomes part of them with its berry.
func TestGrowingPlantsNeedSupport(t *testing.T) {
	w := world.Config{Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	t.Cleanup(func() { _ = w.Close() })
	var failure string
	<-w.Exec(func(tx *world.Tx) {
		fail := func(format string, a ...any) { failure = fmt.Sprintf(format, a...) }
		user := &placingUser{tx: tx}
		ceiling := cube.Pos{0, 70, 0}
		tx.SetBlock(ceiling, Stone{}, nil)
		if !(item.GlowBerries{}).UseOnBlock(ceiling, cube.FaceDown, mgl64.Vec3{}, tx, user, &item.UseContext{}) {
			fail("glow berries did not plant cave vines under stone")
			return
		}
		vines, ok := tx.Block(ceiling.Side(cube.FaceDown)).(CaveVines)
		if !ok || vines.Age >= 25 {
			fail("planted %#v under stone", tx.Block(ceiling.Side(cube.FaceDown)))
			return
		}
		vines.Variant = caveVinesHeadWithBerries
		tx.SetBlock(ceiling.Side(cube.FaceDown), vines, nil)
		if !(item.GlowBerries{}).UseOnBlock(ceiling.Side(cube.FaceDown), cube.FaceDown, mgl64.Vec3{}, tx, user, &item.UseContext{}) {
			fail("glow berries did not continue cave vines")
			return
		}
		if got := tx.Block(ceiling.Side(cube.FaceDown)).(CaveVines); got.Variant != caveVinesBodyWithBerries {
			fail("continued tip became %#v, want vines keeping their berry", got)
			return
		}
		floating := cube.Pos{10, 70, 0}
		if (item.GlowBerries{}).UseOnBlock(floating, cube.FaceDown, mgl64.Vec3{}, tx, user, &item.UseContext{}) {
			fail("glow berries planted cave vines under air")
			return
		}
		if (WeepingVines{}).UseOnBlock(floating, cube.FaceDown, mgl64.Vec3{}, tx, user, &item.UseContext{}) {
			fail("weeping vines hung from air")
			return
		}
		ground := cube.Pos{20, 64, 0}
		tx.SetBlock(ground, Netherrack{}, nil)
		if !(TwistingVines{}).UseOnBlock(ground, cube.FaceUp, mgl64.Vec3{}, tx, user, &item.UseContext{}) {
			fail("twisting vines did not stand on netherrack")
			return
		}
		if (TwistingVines{}).UseOnBlock(ground, cube.FaceDown, mgl64.Vec3{}, tx, user, &item.UseContext{}) {
			fail("twisting vines hung from netherrack")
			return
		}
	})
	if failure != "" {
		t.Fatal(failure)
	}
}
