package session

import (
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// bedrockLock returns the bit of UpdateClientInputLocks that locks the input
// l, 0 for the zero Lock.
func bedrockLock(l input.Lock) uint32 {
	switch l {
	case input.Camera():
		return v2193.ClientInputLockCamera
	case input.Movement():
		return v2193.ClientInputLockMovement
	case input.LateralMovement():
		return v2193.ClientInputLockLateralMovement
	case input.Sneak():
		return v2193.ClientInputLockSneak
	case input.Jump():
		return v2193.ClientInputLockJump
	case input.Mount():
		return v2193.ClientInputLockMount
	case input.Dismount():
		return v2193.ClientInputLockDismount
	case input.MoveForward():
		return v2193.ClientInputLockMoveForward
	case input.MoveBackward():
		return v2193.ClientInputLockMoveBackward
	case input.MoveLeft():
		return v2193.ClientInputLockMoveLeft
	case input.MoveRight():
		return v2193.ClientInputLockMoveRight
	}
	return 0
}
