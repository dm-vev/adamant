package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math/rand/v2"
	"time"
)

// CoralFan is a non-solid coral fan placed on the ground.
type CoralFan struct {
	empty
	transparent
	sourceWaterDisplacer

	// Type is the type of coral of the block.
	Type CoralType
	// Dead is whether the coral fan is dead.
	Dead bool
	// Axis is the axis for the fan direction (X or Z).
	Axis cube.Axis
}

// UseOnBlock ...
func (c CoralFan) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used {
		return false
	}
	if face != cube.FaceUp {
		return false
	}
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		return false
	}
	if liquid, ok := tx.Liquid(pos); ok {
		if water, ok := liquid.(Water); ok && water.Depth != 8 {
			return false
		}
	}

	facing := user.Rotation().Direction()
	if facing == cube.North || facing == cube.South {
		c.Axis = cube.Z
	} else {
		c.Axis = cube.X
	}

	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (CoralFan) HasLiquidDrops() bool {
	return false
}

// SideClosed ...
func (CoralFan) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// NeighbourUpdateTick ...
func (c CoralFan) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		breakBlock(c, pos, tx)
		return
	} else if c.Dead {
		return
	}
	tx.ScheduleBlockUpdate(pos, c, time.Second*5/2)
}

// ScheduledTick ...
func (c CoralFan) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	adjacentWater := false
	pos.Neighbours(func(neighbour cube.Pos) {
		if liquid, ok := tx.Liquid(neighbour); ok {
			if _, ok := liquid.(Water); ok {
				adjacentWater = true
			}
		}
	}, tx.Range())
	if !adjacentWater {
		c.Dead = true
		tx.SetBlock(pos, c, nil)
	}
}

// BreakInfo ...
func (c CoralFan) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(c))
}

// EncodeBlock ...
func (c CoralFan) EncodeBlock() (name string, properties map[string]any) {
	axis := int32(0)
	if c.Axis == cube.X {
		axis = 1
	}
	if c.Dead {
		return "minecraft:dead_" + c.Type.String() + "_coral_fan", map[string]any{"coral_fan_direction": axis}
	}
	return "minecraft:" + c.Type.String() + "_coral_fan", map[string]any{"coral_fan_direction": axis}
}

// EncodeItem ...
func (c CoralFan) EncodeItem() (name string, meta int16) {
	if c.Dead {
		return "minecraft:dead_" + c.Type.String() + "_coral_fan", 0
	}
	return "minecraft:" + c.Type.String() + "_coral_fan", 0
}

// CoralWallFan is a coral fan attached to a wall.
type CoralWallFan struct {
	empty
	transparent
	sourceWaterDisplacer

	// Type is the type of coral of the block.
	Type CoralType
	// Dead is whether the coral fan is dead.
	Dead bool
	// Facing is the direction the fan is facing.
	Facing cube.Direction
}

// UseOnBlock ...
func (c CoralWallFan) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used {
		return false
	}
	if face.Axis() == cube.Y {
		return false
	}
	if !tx.Block(pos.Side(face.Opposite())).Model().FaceSolid(pos.Side(face.Opposite()), face, tx) {
		return false
	}
	if liquid, ok := tx.Liquid(pos); ok {
		if water, ok := liquid.(Water); ok && water.Depth != 8 {
			return false
		}
	}

	c.Facing = face.Direction()
	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (CoralWallFan) HasLiquidDrops() bool {
	return false
}

// SideClosed ...
func (CoralWallFan) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// NeighbourUpdateTick ...
func (c CoralWallFan) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(c.Facing.Face().Opposite())).Model().FaceSolid(pos.Side(c.Facing.Face().Opposite()), c.Facing.Face(), tx) {
		breakBlock(c, pos, tx)
		return
	} else if c.Dead {
		return
	}
	tx.ScheduleBlockUpdate(pos, c, time.Second*5/2)
}

// ScheduledTick ...
func (c CoralWallFan) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	adjacentWater := false
	pos.Neighbours(func(neighbour cube.Pos) {
		if liquid, ok := tx.Liquid(neighbour); ok {
			if _, ok := liquid.(Water); ok {
				adjacentWater = true
			}
		}
	}, tx.Range())
	if !adjacentWater {
		c.Dead = true
		tx.SetBlock(pos, c, nil)
	}
}

// BreakInfo ...
func (c CoralWallFan) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(c))
}

func wallCoralDirection(direction cube.Direction) int32 {
	switch direction {
	case cube.West:
		return 0
	case cube.East:
		return 1
	case cube.North:
		return 2
	case cube.South:
		return 3
	}
	return 0
}

// EncodeBlock ...
func (c CoralWallFan) EncodeBlock() (name string, properties map[string]any) {
	if c.Dead {
		return "minecraft:dead_" + c.Type.String() + "_coral_wall_fan", map[string]any{"coral_direction": wallCoralDirection(c.Facing)}
	}
	return "minecraft:" + c.Type.String() + "_coral_wall_fan", map[string]any{"coral_direction": wallCoralDirection(c.Facing)}
}

// EncodeItem ...
func (c CoralWallFan) EncodeItem() (name string, meta int16) {
	if c.Dead {
		return "minecraft:dead_" + c.Type.String() + "_coral_fan", 0
	}
	return "minecraft:" + c.Type.String() + "_coral_fan", 0
}

// allCoralFans returns all coral fan block variants.
func allCoralFans() (fans []world.Block) {
	for _, t := range CoralTypes() {
		for _, dead := range []bool{false, true} {
			fans = append(fans, CoralFan{Type: t, Dead: dead, Axis: cube.X})
			fans = append(fans, CoralFan{Type: t, Dead: dead, Axis: cube.Z})
		}
	}
	return
}

// allCoralWallFans returns all coral wall fan block variants.
func allCoralWallFans() (fans []world.Block) {
	for _, t := range CoralTypes() {
		for _, dead := range []bool{false, true} {
			for _, d := range cube.Directions() {
				fans = append(fans, CoralWallFan{Type: t, Dead: dead, Facing: d})
			}
		}
	}
	return
}
