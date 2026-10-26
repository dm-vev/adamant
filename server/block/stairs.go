package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Stairs are blocks that allow entities to walk up blocks without jumping.
type Stairs struct {
	transparent
	sourceWaterDisplacer

	// Block is the block to use for the type of stair.
	Block world.Block
	// UpsideDown specifies if the stairs are upside down. If set to true, the full side is at the top part
	// of the block.
	UpsideDown bool
	// Facing is the direction that the full side of the stairs is facing.
	Facing cube.Direction
	// Corner is the canonical corner shape encoded by the game.
	Corner model.StairCorner
}

// UseOnBlock handles the directional placing of stairs and makes sure they are properly placed upside down
// when needed.
func (s Stairs) UseOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, face, used = firstReplaceable(tx, pos, face, s)
	if !used {
		return
	}
	s.Facing = user.Rotation().Direction()
	s.UpsideDown = face == cube.FaceDown || (clickPos[1] > 0.5 && face != cube.FaceUp)
	s.Corner = s.cornerAt(pos, tx)

	place(tx, pos, s, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick updates the persisted corner after adjacent stairs change.
func (s Stairs) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	corner := s.cornerAt(pos, tx)
	if corner != s.Corner {
		s.Corner = corner
		tx.SetBlock(pos, s, nil)
	}
}

// ReconstructMissingState restores a corner omitted by an older disk palette.
// Explicitly saved corner values are left untouched.
func (s Stairs) ReconstructMissingState(pos cube.Pos, missing []string, bs world.BlockSource) world.Block {
	for _, name := range missing {
		if name == "minecraft:corner" {
			s.Corner = s.cornerAt(pos, bs)
			break
		}
	}
	return s
}

func (s Stairs) cornerAt(pos cube.Pos, bs world.BlockSource) model.StairCorner {
	right, left := s.Facing.RotateRight(), s.Facing.RotateLeft()
	if front, ok := bs.Block(pos.Side(s.Facing.Face())).(Stairs); ok && front.UpsideDown == s.UpsideDown {
		if (front.Facing == right || front.Facing == left) && !s.sameStairAt(pos.Side(front.Facing.Opposite().Face()), bs) {
			switch front.Facing {
			case right:
				return model.StairCornerOuterRight
			case left:
				return model.StairCornerOuterLeft
			}
		}
	}
	if back, ok := bs.Block(pos.Side(s.Facing.Opposite().Face())).(Stairs); ok && back.UpsideDown == s.UpsideDown {
		if (back.Facing == right || back.Facing == left) && !s.sameStairAt(pos.Side(back.Facing.Face()), bs) {
			switch back.Facing {
			case right:
				return model.StairCornerInnerRight
			case left:
				return model.StairCornerInnerLeft
			}
		}
	}
	return model.StairCornerNone
}

func (s Stairs) sameStairAt(pos cube.Pos, bs world.BlockSource) bool {
	side, ok := bs.Block(pos).(Stairs)
	return ok && side.Facing == s.Facing && side.UpsideDown == s.UpsideDown && side.Block == s.Block
}

// Model ...
func (s Stairs) Model() world.BlockModel {
	return model.Stair{Facing: s.Facing, UpsideDown: s.UpsideDown, Corner: s.Corner}
}

// BreakInfo ...
func (s Stairs) BreakInfo() BreakInfo {
	breakInfo := s.Block.(Breakable).BreakInfo()
	return newBreakInfo(breakInfo.Hardness, breakInfo.Harvestable, breakInfo.Effective, oneOf(s)).withBlastResistance(breakInfo.BlastResistance)
}

// Instrument ...
func (s Stairs) Instrument() sound.Instrument {
	if _, ok := s.Block.(Planks); ok {
		return sound.Bass()
	}
	if _, ok := s.Block.(BambooMosaic); ok {
		return sound.Bass()
	}
	return sound.BassDrum()
}

// FlammabilityInfo ...
func (s Stairs) FlammabilityInfo() FlammabilityInfo {
	if flammable, ok := s.Block.(Flammable); ok {
		return flammable.FlammabilityInfo()
	}
	return newFlammabilityInfo(0, 0, false)
}

// EncodeItem ...
func (s Stairs) EncodeItem() (name string, meta int16) {
	return "minecraft:" + encodeStairsBlock(s.Block) + "_stairs", 0
}

// EncodeBlock ...
func (s Stairs) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:" + encodeStairsBlock(s.Block) + "_stairs", map[string]any{
		"upside_down_bit":  s.UpsideDown,
		"weirdo_direction": toStairsDirection(s.Facing),
		"minecraft:corner": s.Corner.String(),
	}
}

// toStairDirection converts a facing to a stair's direction for Minecraft.
func toStairsDirection(v cube.Direction) int32 {
	return int32(3 - v)
}

// SideClosed ...
func (s Stairs) SideClosed(pos, side cube.Pos, tx *world.Tx) bool {
	return s.Model().FaceSolid(pos, pos.Face(side), tx)
}

// CanRedstoneWireStepDown ...
func (Stairs) CanRedstoneWireStepDown(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// allStairs returns all states of stairs.
func allStairs() (stairs []world.Block) {
	for _, block := range StairsBlocks() {
		for facing := cube.Direction(0); facing <= 3; facing++ {
			for half := range 2 {
				for corner := model.StairCornerNone; corner <= model.StairCornerOuterRight; corner++ {
					stairs = append(stairs, Stairs{Block: block, Facing: facing, UpsideDown: half != 0, Corner: corner})
				}
			}
		}
	}
	return
}
