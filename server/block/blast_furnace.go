package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// BlastFurnace is a block that smelts ores, raw metals, iron and gold armour and tools, similar to a furnace, but at
// twice the speed. It also serves as an armourer's job site block.
// The empty value of BlastFurnace is not valid. It must be created using block.NewBlastFurnace(cube.Face).
type BlastFurnace struct {
	solid
	bassDrum
	*smelter

	// Facing is the direction the blast furnace is facing.
	Facing cube.Direction
	// Lit is true if the blast furnace is lit.
	Lit bool
}

func (BlastFurnace) ContainerSize() int { return 3 }

// NewBlastFurnace creates a new initialised blast furnace. The smelter is properly initialised.
func NewBlastFurnace(face cube.Direction) BlastFurnace {
	return BlastFurnace{
		Facing:  face,
		smelter: newSmelter(),
	}
}

// Tick is called to check if the blast furnace should update and start or stop smelting.
func (b BlastFurnace) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if b.Lit && rand.Float64() <= 0.016 { // Every three or so seconds.
		tx.PlaySound(pos.Vec3Centre(), sound.BlastFurnaceCrackle{})
	}
	lit, inventoryChanged := b.tickSmelting(item.Blasting, true)
	if inventoryChanged {
		notifyComparatorUpdate(pos, tx)
	}
	if b.Lit != lit {
		b.Lit = lit
		tx.SetBlock(pos, b, nil)
	}
}

// LightEmissionLevel ...
func (b BlastFurnace) LightEmissionLevel() uint8 {
	if b.Lit {
		return 13
	}
	return 0
}

// EncodeItem ...
func (b BlastFurnace) EncodeItem() (name string, meta int16) {
	return "minecraft:blast_furnace", 0
}

// EncodeBlock ...
func (b BlastFurnace) EncodeBlock() (name string, properties map[string]interface{}) {
	if b.Lit {
		return "minecraft:lit_blast_furnace", map[string]interface{}{"minecraft:cardinal_direction": b.Facing.String()}
	}
	return "minecraft:blast_furnace", map[string]interface{}{"minecraft:cardinal_direction": b.Facing.String()}
}

// UseOnBlock ...
func (b BlastFurnace) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, b)
	if !used {
		return false
	}

	place(tx, pos, NewBlastFurnace(user.Rotation().Direction().Opposite()), user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (b BlastFurnace) BreakInfo() BreakInfo {
	return newBreakInfo(3.5, alwaysHarvestable, pickaxeEffective, oneOf(BlastFurnace{})).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		if b.smelter != nil {
			b.broken(pos, tx)
		}
	})
}

// Activate ...
func (b BlastFurnace) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	if opener, ok := u.(ContainerOpener); ok {
		opener.OpenBlockContainer(pos, tx)
		return true
	}
	return false
}

// EncodeNBT ...
func (b BlastFurnace) EncodeNBT() map[string]any {
	if b.smelter == nil {
		//noinspection GoAssignmentToReceiver
		b = NewBlastFurnace(b.Facing)
	}
	return smelterActorNBT("BlastFurnace", b.smelter)
}

// DecodeNBT ...
func (b BlastFurnace) DecodeNBT(data map[string]any) any {
	lit := b.Lit
	//noinspection GoAssignmentToReceiver
	b = NewBlastFurnace(b.Facing)
	b.Lit = lit
	decodeSmelter(b.smelter, data)
	return b
}

// allBlastFurnaces ...
func allBlastFurnaces() (furnaces []world.Block) {
	for _, face := range cube.Directions() {
		furnaces = append(furnaces, BlastFurnace{Facing: face})
		furnaces = append(furnaces, BlastFurnace{Facing: face, Lit: true})
	}
	return
}
