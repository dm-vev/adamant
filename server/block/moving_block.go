package block

import (
	"maps"

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

	networkSent  bool
	repeatUpdate bool
}

// UpdateNBT suppresses the creation-time update. Native marks the actor dirty
// and publishes its compound on its first tick; chunk data still uses EncodeNBT.
func (MovingBlock) UpdateNBT() map[string]any { return nil }

// Tick publishes the initial moving actor data once it joins the actor clock.
func (m MovingBlock) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if !m.networkSent && m.Moving != nil {
		m.networkSent = true
		tx.SetBlockEntity(pos, m)
		viewers := tx.Viewers(pos.Vec3())
		for _, v := range viewers {
			v.ViewBlockAction(pos, MovingBlockUpdateAction{MovingBlock: m})
			if m.repeatUpdate {
				// BDS publishes twice when the destination replaced an occupied
				// cell, including a retracting head and inner cells of a chain.
				v.ViewBlockAction(pos, MovingBlockUpdateAction{MovingBlock: m})
			}
		}
		tx.ReleaseViewers(viewers)
	}
	// Native checks the parent's block and actor every tick, including after
	// initial publication. Removing a piston must not leave moving blocks behind.
	if _, ok := tx.Block(m.PistonPos).(Piston); !ok {
		m.restore(pos, tx)
	}
}

// restore completes a moving block, including when its parent disappeared.
// Both native completion paths restore the carried actor at its new position
// and dispatch the wrapped block's movedByPiston callback (vtable slot 0x1b0).
func (m MovingBlock) restore(pos cube.Pos, tx *world.Tx) {
	moved := m.Moving
	if moved == nil {
		tx.SetBlock(pos, nil, nil)
		return
	}
	if m.MovingEntity != nil {
		if nbtBlock, ok := moved.(world.NBTer); ok {
			movingEntity := maps.Clone(m.MovingEntity)
			movingEntity["x"] = int32(pos.X())
			movingEntity["y"] = int32(pos.Y())
			movingEntity["z"] = int32(pos.Z())
			if decoded, ok := world.DecodeNBT(nbtBlock, movingEntity, tx.World().BlockRegistry()).(world.Block); ok {
				moved = decoded
			}
		}
	}
	tx.SetBlock(pos, moved, nil)
	if handler, ok := moved.(interface{ MovedByPiston(cube.Pos, *world.Tx) }); ok {
		handler.MovedByPiston(pos, tx)
	}
}

// Model ...
func (m MovingBlock) Model() world.BlockModel {
	return movingBlockModel{moving: m}
}

type movingBlockModel struct{ moving MovingBlock }

func (m movingBlockModel) BBox(pos cube.Pos, source world.BlockSource) []cube.BBox {
	var piston *Piston
	if p, ok := source.Block(m.moving.PistonPos).(Piston); ok {
		piston = &p
	}
	boxes := movingBlockContactBoxes(m.moving, pos, piston, source)
	for i := range boxes {
		boxes[i] = boxes[i].Translate(pos.Vec3().Mul(-1))
	}
	return boxes
}

func (m movingBlockModel) FaceSolid(pos cube.Pos, face cube.Face, source world.BlockSource) bool {
	if m.moving.Moving != nil {
		return m.moving.Moving.Model().FaceSolid(pos, face, source)
	}
	return model.Empty{}.FaceSolid(pos, face, source)
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
