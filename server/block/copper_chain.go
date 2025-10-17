package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"math/rand/v2"
)

// CopperChain is a decorative block that can be oxidised and waxed.
type CopperChain struct {
	transparent
	sourceWaterDisplacer

	// Axis is the axis which the chain faces.
	Axis cube.Axis
	// Oxidation is the level of oxidation of the chain.
	Oxidation OxidationType
	// Waxed specifies whether the chain has been waxed with honeycomb.
	Waxed bool
}

// SideClosed ...
func (CopperChain) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// UseOnBlock ...
func (c CopperChain) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, face, used = firstReplaceable(tx, pos, face, c)
	if !used {
		return
	}
	c.Axis = face.Axis()

	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (c CopperChain) BreakInfo() BreakInfo {
	return newBreakInfo(3, func(t item.Tool) bool {
		return t.ToolType() == item.TypePickaxe && t.HarvestLevel() >= item.ToolTierStone.HarvestLevel
	}, pickaxeEffective, oneOf(c)).withBlastResistance(30)
}

// Wax waxes the copper chain to stop it from oxidising further.
func (c CopperChain) Wax(cube.Pos, mgl64.Vec3) (world.Block, bool) {
	before := c.Waxed
	c.Waxed = true
	return c, !before
}

// Strip strips oxidation or wax from the copper chain.
func (c CopperChain) Strip() (world.Block, world.Sound, bool) {
	if c.Waxed {
		c.Waxed = false
		return c, sound.WaxRemoved{}, true
	}
	if ot, ok := c.Oxidation.Decrease(); ok {
		c.Oxidation = ot
		return c, sound.CopperScraped{}, true
	}
	return c, nil, false
}

// CanOxidate returns whether the chain can oxidise further.
func (c CopperChain) CanOxidate() bool {
	return !c.Waxed
}

// OxidationLevel returns the current oxidation level.
func (c CopperChain) OxidationLevel() OxidationType {
	return c.Oxidation
}

// WithOxidationLevel returns the copper chain with the oxidation level set.
func (c CopperChain) WithOxidationLevel(o OxidationType) Oxidisable {
	c.Oxidation = o
	return c
}

// RandomTick handles the natural oxidation of the chain.
func (c CopperChain) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	attemptOxidation(pos, tx, r, c)
}

// EncodeItem ...
func (c CopperChain) EncodeItem() (name string, meta int16) {
	name = "copper_chain"
	if c.Oxidation != UnoxidisedOxidation() {
		name = c.Oxidation.String() + "_" + name
	}
	if c.Waxed {
		name = "waxed_" + name
	}
	return "minecraft:" + name, 0
}

// EncodeBlock ...
func (c CopperChain) EncodeBlock() (string, map[string]any) {
	name := "copper_chain"
	if c.Oxidation != UnoxidisedOxidation() {
		name = c.Oxidation.String() + "_" + name
	}
	if c.Waxed {
		name = "waxed_" + name
	}
	return "minecraft:" + name, map[string]any{"pillar_axis": c.Axis.String()}
}

// Model ...
func (c CopperChain) Model() world.BlockModel {
	return model.Chain{Axis: c.Axis}
}

// allCopperChains returns a list of every copper chain state.
func allCopperChains() (chains []world.Block) {
	for _, waxed := range []bool{false, true} {
		for _, oxidation := range OxidationTypes() {
			for _, axis := range cube.Axes() {
				chains = append(chains, CopperChain{Axis: axis, Oxidation: oxidation, Waxed: waxed})
			}
		}
	}
	return
}
