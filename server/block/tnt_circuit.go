package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TntBlock::_onRedstoneUpdate receives the published native consumer strength,
// not an immediate neighbour notification. Short pulses can miss its sample.
type tntCircuit struct {
	block                TNT
	input, previousInput int
}

func (t TNT) RedstoneCircuit() world.RedstoneCircuitState { return tntCircuit{block: t} }
func (t TNT) RedstoneCircuitOutput(output world.Block) world.Block {
	if _, ok := output.(TNT); ok {
		return t
	}
	return nil
}
func (s tntCircuit) Cache(cube.Pos, *world.Tx) world.RedstoneCircuitState { return s }
func (s tntCircuit) CacheAfterSources(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	s.block = tx.Block(pos).(TNT)
	s.previousInput, s.input = s.input, 0
	for _, face := range cube.Faces() {
		s.input = max(s.input, tx.RedstonePowerFrom(pos, face))
	}
	return s
}
func (s tntCircuit) Evaluate() (world.RedstoneCircuitState, world.Block) {
	if s.input == s.previousInput {
		return s, nil
	}
	return s, s.block
}
func (s tntCircuit) CircuitInputChange() (int, int) { return s.previousInput, s.input }
func (s tntCircuit) CircuitPublished(pos cube.Pos, tx *world.Tx, _, after world.Block) {
	if t, ok := after.(TNT); ok && s.input > 0 {
		t.Ignite(pos, tx, nil)
	}
}
func (s tntCircuit) Refresh(_, after world.Block) world.RedstoneCircuitState {
	t, ok := after.(TNT)
	if !ok {
		return nil
	}
	s.block = t
	return s
}
