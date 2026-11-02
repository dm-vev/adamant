package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// DoorBlock::neighborChanged schedules a one-game-tick survival check at
// notification time, independently of which neighbour changed. A notification
// for a removed lower half reaches its upper half on the following tick.
func (d WoodDoor) NeighbourChanged(pos, changed cube.Pos, tx *world.Tx) {
	doorNeighbourChanged(d, pos, changed, tx)
}
func (d IronDoor) NeighbourChanged(pos, changed cube.Pos, tx *world.Tx) {
	doorNeighbourChanged(d, pos, changed, tx)
}
func (d CopperDoor) NeighbourChanged(pos, changed cube.Pos, tx *world.Tx) {
	doorNeighbourChanged(d, pos, changed, tx)
}

func doorNeighbourChanged(b world.Block, pos, changed cube.Pos, tx *world.Tx) {
	// Native block replacement notifies the surrounding cells, not itself.
	// The general kernel API also supplies a self update for legacy blocks.
	if pos == changed {
		return
	}
	if !doorCanSurvive(b, pos, tx) {
		tx.ScheduleBlockUpdate(pos, b, gameTicks(1))
	}
	tx.Redstone().ScheduleUpdate(pos)
}

// CopperDoorBlock registers native tick executors even for waxed and fully
// oxidised variants. FillCommand schedules their one-tick post-fill callback.
func (CopperDoor) TickAfterCommandFill() bool { return true }

func doorCanSurvive(b world.Block, pos cube.Pos, tx *world.Tx) bool {
	d := b.Model().(model.Door)
	other := pos.Side(cube.FaceUp)
	if d.Top {
		other = pos.Side(cube.FaceDown)
	} else {
		below := pos.Side(cube.FaceDown)
		if !doorSupportedBy(below, tx) {
			return false
		}
	}
	partner, ok := tx.Block(other).Model().(model.Door)
	return ok && partner.Kind == d.Kind && partner.Top != d.Top
}

// Native door support uses support kind 1, not a fence/wall's centre support
// or farmland's collision model. PathIds is an explicit native exception.
func doorSupportedBy(pos cube.Pos, tx *world.Tx) bool {
	b := tx.Block(pos)
	if _, path := b.(DirtPath); path {
		return true
	}
	switch b.Model().(type) {
	case model.Fence, model.Wall, model.TilledGrass:
		return false
	}
	return b.Model().FaceSolid(pos, cube.FaceUp, tx)
}

func (d WoodDoor) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	doorSurvivalTick(d, pos, tx)
}
func (d IronDoor) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	doorSurvivalTick(d, pos, tx)
}
func (d CopperDoor) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	doorSurvivalTick(d, pos, tx)
}

func doorSurvivalTick(b world.Block, pos cube.Pos, tx *world.Tx) {
	if doorCanSurvive(b, pos, tx) {
		return
	}
	breakBlockNoDrops(b, pos, tx)
	if !b.Model().(model.Door).Top && tx.World().GameRuleBool("block_drops") {
		tx.DropBlockResource(pos, item.NewStack(b.(world.Item), 1))
	}
}
