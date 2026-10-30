package block

import "time"

// OpenAction is a world.BlockAction to open a block at a position. It is sent for blocks such as chests.
type OpenAction struct{ action }

// CloseAction is a world.BlockAction to close a block at a position, complementary to the OpenAction action.
type CloseAction struct{ action }

// StartCrackAction is a world.BlockAction to make the cracks in a block start forming, following the break time set in
// the action.
type StartCrackAction struct {
	action
	BreakTime time.Duration
}

// ContinueCrackAction is a world.BlockAction sent every so often to continue the cracking process of the block. It is
// only ever sent after a StartCrackAction action, and may have an altered break time if the player is not on the
// ground, submerged or is using a different item than at first.
type ContinueCrackAction struct {
	action
	BreakTime time.Duration
}

// StopCrackAction is a world.BlockAction to make the cracks forming in a block stop and disappear.
type StopCrackAction struct{ action }

// DecoratedPotWobbleAction is a world.BlockAction to make a decorated pot wobble when interacted with.
type DecoratedPotWobbleAction struct {
	action
	DecoratedPot DecoratedPot
	// Success is whether an item was successfully inserted into the decorated pot.
	Success bool
}

// MovingBlockUpdateAction publishes a moving actor after its first native tick.
// The session sends the parent piston and block changes before this actor data.
type MovingBlockUpdateAction struct {
	action
	MovingBlock MovingBlock
}

// BellRingAction retains the native redstone ring's repeated block/actor
// notification. Its actor snapshot starts animation at Ticks zero.
type BellRingAction struct {
	action
	Bell Bell
}

// BellUseRejectedAction confirms an unchanged bell permutation to the player
// whose interaction was rejected by the native attachment/direction gate.
type BellUseRejectedAction struct {
	action
	Bell Bell
}

// action implements the Action interface. Structures in this package may embed it to gets its functionality
// out of the box.
type action struct{}

// BlockAction serves to implement the world.BlockAction interface.
func (action) BlockAction() {}
