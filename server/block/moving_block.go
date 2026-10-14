package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
)

// MovingBlock is a temporary block used to animate piston movements.
type MovingBlock struct {
	Moving       world.Block
	MovingEntity map[string]any
	PistonPos    cube.Pos
	// Expanding reports whether the piston moving the block extends.
	Expanding bool
}

// Model ...
func (m MovingBlock) Model() world.BlockModel {
	if m.Moving != nil {
		return m.Moving.Model()
	}
	return model.Empty{}
}

// EncodeBlock ...
func (MovingBlock) EncodeBlock() (string, map[string]any) {
	return "minecraft:moving_block", nil
}

// EncodeNBT encodes the MovingBlock block actor as BDS does. Pistons move
// only a block's first layer, so the second layer it carries is air.
func (m MovingBlock) EncodeNBT() map[string]any {
	nbt := map[string]any{
		"id":               "MovingBlock",
		"pistonPosX":       int32(m.PistonPos.X()),
		"pistonPosY":       int32(m.PistonPos.Y()),
		"pistonPosZ":       int32(m.PistonPos.Z()),
		"expanding":        boolByte(m.Expanding),
		"movingBlockExtra": nbtconv.WriteBlock(Air{}),
	}
	if m.Moving != nil {
		nbt["movingBlock"] = nbtconv.WriteBlock(m.Moving)
	}
	if m.MovingEntity != nil {
		nbt["movingEntity"] = m.MovingEntity
	}
	return nbt
}

// DecodeNBT ...
func (m MovingBlock) DecodeNBT(data map[string]any) any {
	m.PistonPos = cube.Pos{
		int(nbtconv.Int32(data, "pistonPosX")),
		int(nbtconv.Int32(data, "pistonPosY")),
		int(nbtconv.Int32(data, "pistonPosZ")),
	}
	m.Expanding = nbtconv.Bool(data, "expanding")
	m.Moving = nbtconv.Block(data, "movingBlock")
	if ent, ok := data["movingEntity"].(map[string]any); ok {
		m.MovingEntity = ent
	}
	return m
}

func allMovingBlocks() (blocks []world.Block) {
	return []world.Block{MovingBlock{}}
}
