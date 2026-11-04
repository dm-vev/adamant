package session

import (
	"github.com/df-mc/dragonfly/server/player/input"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// bedrockLock returns the bit of UpdateClientInputLocks that locks the input
// l, 0 for the zero Lock.
func bedrockLock(l input.Lock) uint32 {
	switch l {
	case input.Camera():
		return mc.ClientInputLockCamera
	case input.Movement():
		return mc.ClientInputLockMovement
	case input.LateralMovement():
		return mc.ClientInputLockLateralMovement
	case input.Sneak():
		return mc.ClientInputLockSneak
	case input.Jump():
		return mc.ClientInputLockJump
	case input.Mount():
		return mc.ClientInputLockMount
	case input.Dismount():
		return mc.ClientInputLockDismount
	case input.MoveForward():
		return mc.ClientInputLockMoveForward
	case input.MoveBackward():
		return mc.ClientInputLockMoveBackward
	case input.MoveLeft():
		return mc.ClientInputLockMoveLeft
	case input.MoveRight():
		return mc.ClientInputLockMoveRight
	}
	return 0
}
