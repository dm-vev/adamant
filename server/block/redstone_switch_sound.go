package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// Bedrock uses the same named events for buttons and levers. The extra data
// is the network hash of the resulting block state; the client selects the
// material's click sound from that state.
func playRedstoneSwitchSound(pos cube.Pos, tx *world.Tx, b world.Block, powered bool) {
	name := "button.click_off"
	if powered {
		name = "button.click_on"
	}
	registry := tx.World().BlockRegistry()
	hash, _ := registry.RuntimeIDToHash(registry.BlockRuntimeID(b))
	tx.PlaySound(pos.Vec3Centre(), sound.NativeEvent{SoundType: name, ExtraData: int32(hash)})
}
