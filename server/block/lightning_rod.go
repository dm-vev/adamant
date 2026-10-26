package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// LightningRod redirects nearby lightning from the native 64-block cylinder
// when it is exposed to rain, and emits a redstone pulse when
// struck. It oxidises like other copper blocks.
type LightningRod struct {
	transparent
	sourceWaterDisplacer

	// Facing is the direction the rod points, away from its support.
	Facing cube.Face
	// Powered is set for eight ticks after the latest lightning hit.
	Powered bool
	// Oxidation is the current oxidation level of the rod.
	Oxidation OxidationType
	// Waxed indicates whether the rod was waxed to stop oxidation.
	Waxed bool
}

// Model ...
func (r LightningRod) Model() world.BlockModel { return model.EndRod{Axis: r.Facing.Axis()} }

// SideClosed lets water flow around the thin rod.
func (LightningRod) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// BreakInfo ...
func (r LightningRod) BreakInfo() BreakInfo {
	return newBreakInfo(3, func(t item.Tool) bool {
		return t.ToolType() == item.TypePickaxe && t.HarvestLevel() >= item.ToolTierStone.HarvestLevel
	}, pickaxeEffective, oneOf(LightningRod{Oxidation: r.Oxidation, Waxed: r.Waxed})).withBlastResistance(6)
}

// UseOnBlock places the rod pointing away from the clicked face.
func (r LightningRod) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}
	r.Facing, r.Powered = face, false
	place(tx, pos, r, user, ctx)
	return placed(ctx)
}

// Strike follows CopperLightningRodBlock.onLightningHit: an unwaxed rod loses
// one oxidation stage, and each hit restarts its eight-tick pulse.
func (r LightningRod) Strike(pos cube.Pos, tx *world.Tx) {
	tx.CancelBlockUpdate(pos, r)
	if !r.Waxed {
		if previous, ok := r.Oxidation.Decrease(); ok {
			r.Oxidation = previous
		}
	}
	r.Powered = true
	// Native copper replacement holds the circuit lock, retaining the producer
	// across an oxidation identifier change before setting its active strength.
	tx.Redstone().SetProducerBlock(pos, r)
	tx.Redstone().SetProducerStrength(pos, 15)
	tx.ScheduleBlockUpdate(pos, r, 8*time.Second/20)
	// The native hit callback emits event 2033 at the integer block position
	// with zero data for every facing.
	tx.AddParticle(pos.Vec3(), particle.ElectricSpark{})
}

// ScheduledTick ends the pulse of a struck rod.
func (r LightningRod) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	// Expiry clears component strength even if another operation already
	// cleared the visible bit; the native callback does not gate on that bit.
	r.Powered = false
	tx.SetBlock(pos, r, nil)
	tx.Redstone().SetProducerStrength(pos, 0)
}

// RedstoneProducerPower initializes the circuit component from the permutation.
func (r LightningRod) RedstoneProducerPower() int {
	if r.Powered {
		return 15
	}
	return 0
}

// RedstoneProducerOutput supplies the strength stored by the circuit component.
func (r LightningRod) RedstoneProducerOutput(power int) world.Block {
	r.Powered = power != 0
	return r
}

// RedstonePower ports ownSignal: a struck rod powers every side.
func (r LightningRod) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	if r.Powered {
		return 15
	}
	return 0
}

// RedstoneStrongPower ports getDirectSignal: a struck rod strongly powers the
// block it stands on.
func (r LightningRod) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if r.Powered && face == r.Facing.Opposite() {
		return 15
	}
	return 0
}

// Wax waxes the rod to stop it from oxidising further.
func (r LightningRod) Wax(cube.Pos, mgl64.Vec3) (world.Block, bool) {
	before := r.Waxed
	r.Waxed = true
	return r, !before
}

// Strip removes wax or oxidation from the rod.
func (r LightningRod) Strip() (world.Block, world.Sound, bool) {
	if r.Waxed {
		r.Waxed = false
		return r, sound.WaxRemoved{}, true
	}
	if o, ok := r.Oxidation.Decrease(); ok {
		r.Oxidation = o
		return r, sound.CopperScraped{}, true
	}
	return r, nil, false
}

// CanOxidate returns whether the rod can still oxidise.
func (r LightningRod) CanOxidate() bool { return !r.Waxed }

// OxidationLevel returns the current oxidation level of the rod.
func (r LightningRod) OxidationLevel() OxidationType { return r.Oxidation }

// WithOxidationLevel returns the rod with its oxidation level set.
func (r LightningRod) WithOxidationLevel(o OxidationType) Oxidisable {
	r.Oxidation = o
	return r
}

// RandomTick handles the natural oxidation of the rod.
func (r LightningRod) RandomTick(pos cube.Pos, tx *world.Tx, rnd *rand.Rand) {
	attemptOxidation(pos, tx, rnd, r)
}

// EncodeItem ...
func (r LightningRod) EncodeItem() (name string, meta int16) {
	name, _ = r.EncodeBlock()
	return name, 0
}

// EncodeBlock ...
func (r LightningRod) EncodeBlock() (string, map[string]any) {
	name := "lightning_rod"
	if r.Oxidation != UnoxidisedOxidation() {
		name = r.Oxidation.String() + "_" + name
	}
	if r.Waxed {
		name = "waxed_" + name
	}
	return "minecraft:" + name, map[string]any{"facing_direction": int32(r.Facing), "powered_bit": r.Powered}
}

// allLightningRods returns every lightning rod state.
func allLightningRods() (rods []world.Block) {
	for _, waxed := range []bool{false, true} {
		for _, oxidation := range OxidationTypes() {
			for _, face := range cube.Faces() {
				for _, powered := range []bool{false, true} {
					rods = append(rods, LightningRod{Facing: face, Powered: powered, Oxidation: oxidation, Waxed: waxed})
				}
			}
		}
	}
	return
}
