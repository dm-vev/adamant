package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// observerCircuit follows PulseCapacitor::setStrength/evaluate. The observer's
// scheduled tick changes its visible bit; its electrical pulse is evaluated
// separately and published in the following chunk update pass.
type observerCircuit struct {
	facing         cube.Face
	strength, next int
}

func (o Observer) RedstoneCircuit() world.RedstoneCircuitState {
	state := observerCircuit{facing: o.Facing}
	if o.Powered {
		state.next = 15
	}
	return state
}

func (Observer) RedstoneCircuitOutput(world.Block) world.Block { return nil }

func (s observerCircuit) Cache(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	o, ok := tx.Block(pos).(Observer)
	if !ok {
		return nil
	}
	if o.Facing != s.facing {
		return o.RedstoneCircuit()
	}
	return s
}

func (s observerCircuit) Evaluate() (world.RedstoneCircuitState, world.Block) {
	s.strength, s.next = s.next, 0
	return s, nil
}

func (s observerCircuit) Refresh(_, after world.Block) world.RedstoneCircuitState {
	o, ok := after.(Observer)
	if !ok {
		return nil
	}
	if o.Facing != s.facing {
		return o.RedstoneCircuit()
	}
	return s
}

func (s observerCircuit) CircuitStrength() int { return s.strength }

func (s observerCircuit) WithCircuitStrength(power int) world.RedstoneCircuitState {
	s.next = power
	return s
}
