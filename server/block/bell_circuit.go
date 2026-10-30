package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// BellBlockActor reads its consumer on the dimension's two-GT sampling gate.
// The actor owns ringing and toggle_bit; neither represents consumer strength.
type bellCircuit struct{ input int }

func (Bell) RedstoneCircuit() world.RedstoneCircuitState                   { return bellCircuit{} }
func (Bell) RedstoneCircuitOutput(world.Block) world.Block                 { return nil }
func (s bellCircuit) Cache(cube.Pos, *world.Tx) world.RedstoneCircuitState { return s }
func (s bellCircuit) CacheAfterSources(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	s.input = 0
	for _, face := range cube.Faces() {
		s.input = max(s.input, tx.RedstonePowerFrom(pos, face))
	}
	return s
}
func (s bellCircuit) Evaluate() (world.RedstoneCircuitState, world.Block) { return s, nil }
func (s bellCircuit) CircuitConsumerStrength() int                        { return s.input }
func (s bellCircuit) Refresh(_, after world.Block) world.RedstoneCircuitState {
	if _, ok := after.(Bell); !ok {
		return nil
	}
	return s
}
