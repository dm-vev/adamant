package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Bedrock uses the same named events for buttons and levers. The sound
// carries the resulting block state; the client selects the material's click
// sound from that state.
func playRedstoneSwitchSound(pos cube.Pos, tx *world.Tx, b world.Block, powered bool) {
	name := "button.click_off"
	if powered {
		name = "button.click_on"
	}
	tx.PlaySound(pos.Vec3Centre(), sound.Event{Name: name, Block: b})
}

// Plates use their own named events and emit just above the supporting surface.
// As in native checkPressed, the payload identifies the resulting permutation.
func playPressurePlateSound(pos cube.Pos, tx *world.Tx, b PressurePlate, powered bool) {
	name := "pressure_plate.click_off"
	if powered {
		name = "pressure_plate.click_on"
	}
	tx.PlaySound(pos.Vec3().Add(mgl64.Vec3{.5, .1, .5}), sound.Event{Name: name, Block: b})
}
