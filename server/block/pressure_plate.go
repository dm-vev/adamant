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

	Type PressurePlateType
	// Signal is the encoded redstone_signal property. Ordinary plates use
	// 0/1 for released/pressed; weighted plates store electrical strength.
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
	if p.Signal == 1 && !p.Type.weighted() {
		return
	}
	p.updateSignal(pos, tx)
}

// ScheduledTick ...
func (p PressurePlate) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	p.updateSignal(pos, tx)
}

func (p PressurePlate) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	if !p.Type.weighted() {
		if p.Signal == 1 {
			return 15
		}
		return 0
	}
	return p.Signal
}

func (p PressurePlate) RedstoneStrongPower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	if face == cube.FaceDown {
		return p.RedstonePower(pos, tx, face)
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
			tx.ScheduleBlockUpdate(pos, p, gameTicks(p.Type.pressedTicks()))
		}
		return
	}
	p.Signal = newSignal
	tx.SetBlock(pos, p, nil)
	tx.DoBlockUpdatesAround(pos.Side(cube.FaceDown))
	if newSignal > 0 {
		tx.ScheduleBlockUpdate(pos, p, gameTicks(p.Type.pressedTicks()))
	}
}

func (p PressurePlate) signalForEntities(pos cube.Pos, tx *world.Tx) int {
	count := 0
	// BasePressurePlateBlock::getSensitiveAABB is inset by 1/16 and
	// extends 1/4 block above the support. Entity origins alone miss
	// resting items and bodies overlapping from a neighbouring block.
	box := cube.Box(1.0/16, 0, 1.0/16, 15.0/16, 0.25, 15.0/16).Translate(pos.Vec3())
	for e := range redstoneEntitiesInBox(tx, box, !p.Type.triggersItems()) {
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
		return int(math.Ceil(float64(float32(count) / float32(maxWeight) * 15)))
	}
	return 1
}

func (p PressurePlate) triggersEntity(e world.Entity) bool {
	// Both native queries explicitly skip ActorType::Experience (69).
	if e.H().Type().EncodeEntity() == "minecraft:xp_orb" {
		return false
	}
	if p.Type.triggersItems() {
		return true
	}
	// Native stone plates query the Mob category, which also contains
	// players and armour stands, but excludes arrows and other objects.
	_, living := e.(interface{ Health() float64 })
	return living || e.H().Type().EncodeEntity() == "minecraft:armor_stand"
}

func allPressurePlates() (plates []world.Block) {
	for _, t := range PressurePlateTypes() {
		for signal := 0; signal <= 15; signal++ {
			plates = append(plates, PressurePlate{Type: t, Signal: signal})
		}
	}
	return
}
