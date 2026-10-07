package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EndCrystal is an item that can be placed on obsidian or bedrock to spawn an End crystal entity.
type EndCrystal struct{}

// endCrystalSupport represents a block that supports an End crystal.
type endCrystalSupport interface {
	SupportsEndCrystal() bool
}

// UseOnBlock places a crystal above obsidian or bedrock. Only the immediately
// adjacent block must be air; the two-block-high entity box must be unoccupied.
func (EndCrystal) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	support, ok := tx.Block(pos).(endCrystalSupport)
	if !ok || !support.SupportsEndCrystal() {
		return false
	}

	above := pos.Side(cube.FaceUp)
	if above.OutOfBounds(tx.Range()) {
		return false
	}
	if tx.Block(above) != air() {
		return false
	}
	if _, liquid := tx.Liquid(above); liquid {
		return false
	}

	box := cube.Box(0, 0, 0, 1, 2, 1).Translate(above.Vec3())
	for entity := range tx.EntitiesWithin(box.Grow(2)) {
		if entity.H().Type().BBox(entity).Translate(entity.Position()).IntersectsWith(box) {
			return false
		}
	}

	create := tx.World().EntityRegistry().Config().EndCrystal
	if create == nil {
		return false
	}
	opts := world.EntitySpawnOpts{Position: above.Vec3Middle()}
	tx.AddEntity(create(opts, false))
	if fight := tx.DragonFight(); fight != nil {
		fight.TryRespawn(tx)
	}
	ctx.SubtractFromCount(1)
	return true
}

// EncodeItem returns the ID and meta of the end crystal item.
func (EndCrystal) EncodeItem() (string, int16) {
	return "minecraft:end_crystal", 0
}
