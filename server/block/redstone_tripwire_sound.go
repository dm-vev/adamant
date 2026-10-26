package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Native _emitState gives electrical transitions priority over attachment.
// Its payload reads the block currently at pos: calculateState has already
// published the opposite hook, but has not yet published its own hook.
func emitTripwireHookSound(pos cube.Pos, tx *world.Tx, previous TripwireHook, attached, powered bool) {
	var name string
	switch {
	case powered && !previous.Powered:
		name = "power.on"
	case !powered && previous.Powered:
		name = "power.off"
	case attached && !previous.Attached:
		name = "attach"
	case !attached && previous.Attached:
		name = "detach"
	default:
		return
	}
	registry := tx.World().BlockRegistry()
	hash, _ := registry.RuntimeIDToHash(registry.BlockRuntimeID(tx.Block(pos)))
	tx.PlaySound(pos.Vec3().Add(mgl64.Vec3{0.5, 0.1, 0.5}), sound.NativeEvent{SoundType: name, ExtraData: int32(hash)})
}
