package block

import (
	"github.com/df-mc/dragonfly/server/world"
)

// RedstoneLamp is a light-emitting block that toggles based on redstone power.
type RedstoneLamp struct {
	solid

	// Lit specifies whether the lamp currently emits light.
	Lit bool
}

// LightEmissionLevel returns the current light level of the lamp.
func (l RedstoneLamp) LightEmissionLevel() uint8 {
	if l.Lit {
		return 15
	}
	return 0
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

// allRedstoneLamps returns all lamp block states for registration.
func allRedstoneLamps() []world.Block {
	return []world.Block{
		RedstoneLamp{},
		RedstoneLamp{Lit: true},
	}
}
