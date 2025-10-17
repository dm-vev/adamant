package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// CopperBulb is a light-emitting block whose brightness depends on its oxidation level.
type CopperBulb struct {
	solid
	bassDrum

	// Oxidation is the current oxidation level of the bulb.
	Oxidation OxidationType
	// Waxed indicates whether the bulb was waxed to stop oxidation.
	Waxed bool
	// Lit specifies if the bulb is currently emitting light.
	Lit bool
	// Powered indicates if the bulb is receiving power.
	Powered bool
}

// LightEmissionLevel returns the amount of light produced by the bulb.
func (b CopperBulb) LightEmissionLevel() uint8 {
	if !b.Lit {
		return 0
	}
	switch b.Oxidation {
	case UnoxidisedOxidation():
		return 15
	case ExposedOxidation():
		return 12
	case WeatheredOxidation():
		return 10
	case OxidisedOxidation():
		return 8
	}
	return 0
}

// Activate toggles the bulb between its lit and unlit states.
func (b CopperBulb) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	b.Lit = !b.Lit
	if !b.Lit {
		b.Powered = false
	}
	tx.SetBlock(pos, b, nil)
	return true
}

// Wax waxes the bulb to stop it from oxidising further.
func (b CopperBulb) Wax(cube.Pos, mgl64.Vec3) (world.Block, bool) {
	before := b.Waxed
	b.Waxed = true
	return b, !before
}

// Strip removes wax or oxidation from the bulb.
func (b CopperBulb) Strip() (world.Block, world.Sound, bool) {
	if b.Waxed {
		b.Waxed = false
		return b, sound.WaxRemoved{}, true
	}
	if ot, ok := b.Oxidation.Decrease(); ok {
		b.Oxidation = ot
		return b, sound.CopperScraped{}, true
	}
	return b, nil, false
}

// BreakInfo ...
func (b CopperBulb) BreakInfo() BreakInfo {
	return newBreakInfo(3, func(t item.Tool) bool {
		return t.ToolType() == item.TypePickaxe && t.HarvestLevel() >= item.ToolTierStone.HarvestLevel
	}, pickaxeEffective, oneOf(b)).withBlastResistance(30)
}

// CanOxidate returns whether the bulb can still oxidise.
func (b CopperBulb) CanOxidate() bool {
	return !b.Waxed
}

// OxidationLevel returns the current oxidation level of the bulb.
func (b CopperBulb) OxidationLevel() OxidationType {
	return b.Oxidation
}

// WithOxidationLevel returns the bulb with its oxidation level set to the given value.
func (b CopperBulb) WithOxidationLevel(o OxidationType) Oxidisable {
	b.Oxidation = o
	return b
}

// RandomTick handles the natural oxidation of the bulb.
func (b CopperBulb) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	attemptOxidation(pos, tx, r, b)
}

// EncodeItem ...
func (b CopperBulb) EncodeItem() (name string, meta int16) {
	name = "copper_bulb"
	if b.Oxidation != UnoxidisedOxidation() {
		name = b.Oxidation.String() + "_" + name
	}
	if b.Waxed {
		name = "waxed_" + name
	}
	return "minecraft:" + name, 0
}

// EncodeBlock ...
func (b CopperBulb) EncodeBlock() (string, map[string]any) {
	name := "copper_bulb"
	if b.Oxidation != UnoxidisedOxidation() {
		name = b.Oxidation.String() + "_" + name
	}
	if b.Waxed {
		name = "waxed_" + name
	}
	return "minecraft:" + name, map[string]any{"lit": b.Lit, "powered_bit": b.Powered}
}

// allCopperBulbs returns all possible copper bulb states.
func allCopperBulbs() (bulbs []world.Block) {
	for _, waxed := range []bool{false, true} {
		for _, oxidation := range OxidationTypes() {
			for _, lit := range []bool{false, true} {
				for _, powered := range []bool{false, true} {
					bulbs = append(bulbs, CopperBulb{Oxidation: oxidation, Waxed: waxed, Lit: lit, Powered: powered})
				}
			}
		}
	}
	return
}
