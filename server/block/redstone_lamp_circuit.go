package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

type lampCircuit struct {
	lamp          RedstoneLamp
	input         int
	previousInput int
	turnOff       int
}

func (l RedstoneLamp) RedstoneCircuit() world.RedstoneCircuitState { return lampCircuit{lamp: l} }

func (l RedstoneLamp) RedstoneCircuitOutput(output world.Block) world.Block {
	next, ok := output.(RedstoneLamp)
	if !ok {
		return nil
	}
	l.Lit = next.Lit
	return l
}

func (s lampCircuit) Cache(cube.Pos, *world.Tx) world.RedstoneCircuitState { return s }

func (s lampCircuit) CacheAfterSources(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	l, ok := tx.Block(pos).(RedstoneLamp)
	if !ok {
		return nil
	}
	s.lamp = l
	s.previousInput = s.input
	s.input = tx.RedstonePower(pos)
	return s
}

func (s lampCircuit) CircuitInputChange() (int, int) { return s.previousInput, s.input }

func (s lampCircuit) Evaluate() (world.RedstoneCircuitState, world.Block) {
	if s.input > 0 {
		s.turnOff = 0
		if !s.lamp.Lit {
			s.lamp.Lit = true
			return s, s.lamp
		}
	} else if s.lamp.Lit {
		if s.turnOff == 0 {
			s.turnOff = 2
		} else {
			s.turnOff--
			if s.turnOff == 0 {
				s.lamp.Lit = false
				return s, s.lamp
			}
		}
	}
	return s, nil
}

func (s lampCircuit) Refresh(_, after world.Block) world.RedstoneCircuitState {
	l, ok := after.(RedstoneLamp)
	if !ok {
		return nil
	}
	s.lamp = l
	return s
}
