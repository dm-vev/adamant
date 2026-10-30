package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Native PistonConsumer inherits ConsumerComponent::evaluate. The actor reads
// that scene strength; neighbour notifications do not directly move the piston.
type pistonCircuit struct {
	piston Piston
	input  int
}

func (p Piston) RedstoneCircuit() world.RedstoneCircuitState {
	input := 0
	if p.State == pistonStateExtended && p.NewState == pistonStateExtended {
		input = 15
	}
	return pistonCircuit{piston: p, input: input}
}

func (Piston) RedstoneCircuitOutput(world.Block) world.Block                 { return nil }
func (s pistonCircuit) Cache(cube.Pos, *world.Tx) world.RedstoneCircuitState { return s }
func (s pistonCircuit) CacheAfterSources(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	s.input = s.piston.inputPower(pos, tx)
	return s
}
func (s pistonCircuit) Evaluate() (world.RedstoneCircuitState, world.Block) { return s, nil }
func (s pistonCircuit) CircuitConsumerStrength() int                        { return s.input }
func (s pistonCircuit) Refresh(_, after world.Block) world.RedstoneCircuitState {
	p, ok := after.(Piston)
	if !ok {
		return nil
	}
	s.piston = p
	return s
}
