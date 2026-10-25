package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// wireCircuit is the TransporterComponent's cached-input/evaluate pair. The
// source graph supplies attenuated input; a wire permutation is published
// after the shared circuit step, rather than on every graph invalidation.
type wireCircuit struct {
	wire RedstoneWire
	next int
}

func (r RedstoneWire) RedstoneCircuit() world.RedstoneCircuitState { return wireCircuit{wire: r} }

func (r RedstoneWire) RedstoneCircuitOutput(output world.Block) world.Block {
	next, ok := output.(RedstoneWire)
	if !ok {
		return nil
	}
	r.Power = next.Power
	return r
}

func (s wireCircuit) Cache(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	r, ok := tx.Block(pos).(RedstoneWire)
	if !ok {
		return nil
	}
	s.wire = r
	s.next = tx.RedstonePower(pos)
	return s
}

func (s wireCircuit) Evaluate() (world.RedstoneCircuitState, world.Block) {
	if s.wire.Power == s.next {
		return s, nil
	}
	s.wire.Power = s.next
	return s, s.wire
}

func (s wireCircuit) Refresh(_, after world.Block) world.RedstoneCircuitState {
	r, ok := after.(RedstoneWire)
	if !ok {
		return nil
	}
	s.wire = r
	return s
}
