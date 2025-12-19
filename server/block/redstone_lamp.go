package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// RedstoneLamp is a light source that toggles based on redstone power.
type RedstoneLamp struct {
	solid

	Lit bool
}

// BreakInfo ...
func (l RedstoneLamp) BreakInfo() BreakInfo {
	return newBreakInfo(0.3, alwaysHarvestable, nothingEffective, oneOf(l))
}

// LightEmissionLevel ...
func (l RedstoneLamp) LightEmissionLevel() uint8 {
	if l.Lit {
		return 15
	}
	return 0
}

// NeighbourUpdateTick ...
func (l RedstoneLamp) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	powered := redstonePowered(pos, tx)
	if powered && !l.Lit {
		l.Lit = true
		tx.SetBlock(pos, l, nil)
		return
	}
	if !powered && l.Lit {
		tx.ScheduleBlockUpdate(pos, l, redstoneTicks(4))
	}
}

// ScheduledTick ...
func (l RedstoneLamp) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if l.Lit && !redstonePowered(pos, tx) {
		l.Lit = false
		tx.SetBlock(pos, l, nil)
	}
}

// EncodeItem ...
func (RedstoneLamp) EncodeItem() (name string, meta int16) {
	return "minecraft:redstone_lamp", 0
}

// EncodeBlock ...
func (l RedstoneLamp) EncodeBlock() (string, map[string]any) {
	if l.Lit {
		return "minecraft:lit_redstone_lamp", nil
	}
	return "minecraft:redstone_lamp", nil
}

func allRedstoneLamps() (lamps []world.Block) {
	return []world.Block{RedstoneLamp{Lit: false}, RedstoneLamp{Lit: true}}
}
