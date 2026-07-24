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

// UseOnBlock places an End crystal on top of a supporting block if the two blocks above it are air and no other
// entities intersect the space the crystal is placed in. The face clicked is ignored.
func (EndCrystal) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	support, ok := tx.Block(pos).(endCrystalSupport)
	if !ok || !support.SupportsEndCrystal() {
		return false
	}

	above, twoAbove := pos.Side(cube.FaceUp), pos.Side(cube.FaceUp).Side(cube.FaceUp)
	if above.OutOfBounds(tx.Range()) || twoAbove.OutOfBounds(tx.Range()) {
		return false
	}
	if tx.Block(above) != air() || tx.Block(twoAbove) != air() {
		return false
	}

	box := cube.Box(0, 0, 0, 1, 2, 1).Translate(above.Vec3())
	for entity := range tx.EntitiesWithin(box.Grow(2)) {
		if (user == nil || entity.H() != user.H()) && entity.H().Type().BBox(entity).Translate(entity.Position()).IntersectsWith(box) {
			return false
		}
	}

	create := tx.World().EntityRegistry().Config().EndCrystal
	if create == nil {
		return false
	}
	showBase := true
	if name, _ := tx.Block(pos).EncodeBlock(); name == "minecraft:bedrock" {
		showBase = false
	}
	opts := world.EntitySpawnOpts{Position: above.Vec3Middle()}
	tx.AddEntity(create(opts, showBase))
	ctx.SubtractFromCount(1)
	return true
}

// EncodeItem returns the ID and meta of the end crystal item.
func (EndCrystal) EncodeItem() (string, int16) {
	return "minecraft:end_crystal", 0
}
