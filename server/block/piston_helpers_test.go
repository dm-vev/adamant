package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Only tests use these helpers of piston.go, so they live with the tests.

func (p Piston) shouldExtend(pos cube.Pos, tx *world.Tx) bool {
	return p.inputPower(pos, tx) > 0
}

func (p Piston) extended(pos cube.Pos, tx *world.Tx) bool {
	front := tx.Block(pos.Side(p.Facing))
	if head, ok := front.(PistonHead); ok {
		return head.Facing == p.Facing && head.Sticky == p.Sticky
	}
	if moving, ok := front.(MovingBlock); ok {
		return moving.PistonPos == pos
	}
	return false
}
