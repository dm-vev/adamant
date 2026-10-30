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
func emitTripwireHookState(pos cube.Pos, tx *world.Tx, previous TripwireHook, attached, powered bool) {
	var name string
	var event world.GameEvent
	switch {
	case powered && !previous.Powered:
		name = "power.on"
		event = world.GameEventBlockActivate
	case !powered && previous.Powered:
		name = "power.off"
		event = world.GameEventBlockDeactivate
	case attached && !previous.Attached:
		name = "attach"
		event = world.GameEventBlockAttach
	case !attached && previous.Attached:
		name = "detach"
		event = world.GameEventBlockDetach
	default:
		return
	}
	registry := tx.World().BlockRegistry()
	hash, _ := registry.RuntimeIDToHash(registry.BlockRuntimeID(tx.Block(pos)))
	tx.PlaySound(pos.Vec3().Add(mgl64.Vec3{0.5, 0.1, 0.5}), sound.NativeEvent{SoundType: name, ExtraData: int32(hash)})
	tx.EmitGameEvent(event, pos.Vec3Centre(), world.GameEventContext{})
}
