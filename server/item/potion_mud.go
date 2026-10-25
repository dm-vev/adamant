package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// pourWaterOnBlock ports PotionItem.useOn, which splash and lingering potions
// inherit: water poured from above or the side onto a block of
// convertible_to_mud turns it into mud and leaves an empty bottle.
func pourWaterOnBlock(p potion.Potion, pos cube.Pos, face cube.Face, tx *world.Tx, ctx *UseContext) bool {
	if face == cube.FaceDown || p != potion.Water() {
		return false
	}
	state, err := world.CanonicalStateOf(tx.Block(pos))
	if err != nil {
		return false
	}
	if convertible, _ := gamedata.BlockTagContains("minecraft:convertible_to_mud", state.Name); !convertible {
		return false
	}
	mud, ok := world.BlockByName("minecraft:mud", nil)
	if !ok {
		return false
	}
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "random.splash", Volume: 1, Pitch: 1})
	ctx.SubtractFromCount(1)
	ctx.NewItem, ctx.NewItemSurvivalOnly = NewStack(GlassBottle{}, 1), true
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "bottle.empty", Volume: 1, Pitch: 1})
	tx.EmitGameEvent(world.GameEventFluidPlace, pos.Vec3Centre(), world.GameEventContext{})
	tx.SetBlock(pos, mud, nil)
	return true
}

// UseOnBlock ...
func (p Potion) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	return pourWaterOnBlock(p.Type, pos, face, tx, ctx)
}

// UseOnBlock ...
func (s SplashPotion) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	return pourWaterOnBlock(s.Type, pos, face, tx, ctx)
}

// UseOnBlock ...
func (l LingeringPotion) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	return pourWaterOnBlock(l.Type, pos, face, tx, ctx)
}
