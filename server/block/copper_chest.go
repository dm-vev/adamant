package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// CopperChest is an oxidisable source container for copper golems. It exposes
// the ordinary chest inventory and viewer protocol so it works with players,
// hoppers, and mob sorting rather than acting as a sorting marker only.
type CopperChest struct {
	Chest
	Oxidation OxidationType
	Waxed     bool
}

// NewCopperChest creates a copper chest with a fully initialised 27-slot inventory.
func NewCopperChest() CopperChest { return CopperChest{Chest: NewChest()} }
func (c CopperChest) EncodeItem() (string, int16) {
	return copperBlockName("copper_chest", c.Oxidation, c.Waxed), 0
}
func (c CopperChest) EncodeBlock() (string, map[string]any) {
	return copperBlockName("copper_chest", c.Oxidation, c.Waxed), map[string]any{"minecraft:cardinal_direction": c.Facing.String()}
}

// EncodeNBT uses the BDS chest block-actor schema used by copper chests.
func (c CopperChest) EncodeNBT() map[string]any {
	data := c.Chest.EncodeNBT()
	data["BlockEntityVersion"] = int32(0)
	data["Findable"] = byte(0)
	return data
}
func (c CopperChest) DecodeNBT(m map[string]any) any {
	c.Chest = c.Chest.DecodeNBT(m).(Chest)
	return c
}

func (c CopperChest) withChestData(data Chest) chestBlock { c.Chest = data; return c }
func (CopperChest) canPairChest(other world.Block) bool   { _, ok := other.(CopperChest); return ok }

// WithJavaChestState preserves contents across source oxidation and pairing updates.
func (c CopperChest) WithJavaChestState(previous world.Block, pos cube.Pos, kind string) world.Block {
	c.Chest = c.Chest.withJavaChestState(previous, pos, kind)
	return c
}
func (c CopperChest) CanOxidate() bool                              { return !c.Waxed }
func (c CopperChest) OxidationLevel() OxidationType                 { return c.Oxidation }
func (c CopperChest) WithOxidationLevel(o OxidationType) Oxidisable { c.Oxidation = o; return c }
func (c CopperChest) Wax(cube.Pos, mgl64.Vec3) (world.Block, bool) {
	if c.Waxed {
		return c, false
	}
	c.Waxed = true
	return c, true
}
func (c CopperChest) Strip() (world.Block, world.Sound, bool) {
	if c.Waxed {
		c.Waxed = false
		return c, sound.WaxRemoved{}, true
	}
	if o, ok := c.Oxidation.Decrease(); ok {
		c.Oxidation = o
		return c, sound.CopperScraped{}, true
	}
	return c, nil, false
}
func (c CopperChest) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	attemptOxidation(pos, tx, r, c)
}
func (c CopperChest) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, ok := firstReplaceable(tx, pos, face, c)
	if !ok {
		return false
	}
	c = NewCopperChest()
	c.Facing = user.Rotation().Direction().Opposite()
	place(tx, pos, c, user, ctx)
	return placed(ctx)
}
func (c CopperChest) BreakInfo() BreakInfo {
	return newBreakInfo(2.5, alwaysHarvestable, axeEffective, oneOf(c)).withBreakHandler(func(pos cube.Pos, tx *world.Tx, _ item.User) {
		c.Chest.dropContents(pos, tx, c)
	})
}
