package block

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// PressurePlate is a redstone component that emits power when entities stand on it.
type PressurePlate struct {
	carpet
	transparent
	sourceWaterDisplacer

	Type   PressurePlateType
	Signal int
}

// BreakInfo ...
func (p PressurePlate) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(p))
}

// UseOnBlock ...
func (p PressurePlate) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, p)
	if !used {
		return false
	}
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		return false
	}
	p.Signal = 0
	place(tx, pos, p, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (p PressurePlate) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		breakBlock(p, pos, tx)
	}
}

// EntityInside ...
func (p PressurePlate) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if !p.triggersEntity(e) {
		return
	}
	if p.Signal == 15 && !p.Type.weighted() {
		return
	}
	p.updateSignal(pos, tx)
}

// ScheduledTick ...
func (p PressurePlate) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	p.updateSignal(pos, tx)
}

func (p PressurePlate) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	return p.Signal
}

func (p PressurePlate) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if face == cube.FaceDown {
		return p.Signal
	}
	return 0
}

// EncodeItem ...
func (p PressurePlate) EncodeItem() (name string, meta int16) {
	return "minecraft:" + p.Type.blockName(), 0
}

// EncodeBlock ...
func (p PressurePlate) EncodeBlock() (string, map[string]any) {
	return "minecraft:" + p.Type.blockName(), map[string]any{"redstone_signal": int32(p.Signal)}
}

func (p PressurePlate) updateSignal(pos cube.Pos, tx *world.Tx) {
	newSignal := p.signalForEntities(pos, tx)
	if newSignal == p.Signal {
		if newSignal > 0 {
			tx.ScheduleBlockUpdate(pos, p, redstoneTicks(1))
		}
		return
	}
	p.Signal = newSignal
	tx.SetBlock(pos, p, nil)
	tx.DoBlockUpdatesAround(pos.Side(cube.FaceDown))
	if newSignal > 0 {
		tx.ScheduleBlockUpdate(pos, p, redstoneTicks(1))
	}
}

func (p PressurePlate) signalForEntities(pos cube.Pos, tx *world.Tx) int {
	count := 0
	box := cube.Box(float64(pos[0]), float64(pos[1]), float64(pos[2]), float64(pos[0]+1), float64(pos[1])+0.5, float64(pos[2]+1))
	for e := range tx.EntitiesWithin(box) {
		if p.triggersEntity(e) {
			count++
		}
	}
	if count == 0 {
		return 0
	}
	if p.Type.weighted() {
		maxWeight := p.Type.maxWeight()
		if count > maxWeight {
			count = maxWeight
		}
		return int(math.Ceil(float64(count) / float64(maxWeight) * 15))
	}
	return 15
}

func (p PressurePlate) triggersEntity(e world.Entity) bool {
	if p.Type.triggersItems() {
		return true
	}
	return e.H().Type().EncodeEntity() != "minecraft:item"
}

func allPressurePlates() (plates []world.Block) {
	for _, t := range PressurePlateTypes() {
		for signal := 0; signal <= 15; signal++ {
			plates = append(plates, PressurePlate{Type: t, Signal: signal})
		}
	}
	return
}
