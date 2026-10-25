package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// comparatorCircuitState separates ComparatorCapacitor input caching from
// evaluation and the following chunk's block-actor/permutation publication.
type comparatorCircuitState struct {
	block      RedstoneComparator
	rear, side int
	analog     int
	ready      bool
	subtract   bool
	strength   uint8
	facing     cube.Direction
	sideAnalog int
}

func (c RedstoneComparator) RedstoneCircuit() world.RedstoneCircuitState {
	return comparatorCircuitState{block: c, subtract: c.Subtract, strength: c.Output, facing: c.Facing, sideAnalog: -1}
}

func (s comparatorCircuitState) CircuitDirection() cube.Face { return s.facing.Face() }

func (c RedstoneComparator) RedstoneCircuitOutput(output world.Block) world.Block {
	next, ok := output.(RedstoneComparator)
	if !ok {
		return nil
	}
	// _refreshOutputState changes the identifier and lit bit only when the
	// electrical strength crosses zero. A positive-to-positive change does
	// not repair a variant or lit bit selected by an external property edit.
	if (c.Output > 0) != (next.Output > 0) {
		c.Lit, c.Powered = next.Output > 0, next.Output > 0
	}
	c.Output = next.Output
	return c
}

func (s comparatorCircuitState) Cache(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	b, loaded := tx.BlockLoaded(pos)
	c, ok := b.(RedstoneComparator)
	if !loaded || !ok {
		return nil
	}
	s.block = c
	// cacheValues leaves electrical caches untouched until the first actor
	// tick sets mHasAnalogBeenSet. Setup initially supplies a zero actor value.
	if s.ready {
		s.rear = tx.RedstonePowerFrom(pos, s.facing.Face())
		s.side = diodeSideInputPower(pos, s.facing, tx)
	}
	if s.analog >= 0 {
		s.rear = s.analog
	}
	if s.sideAnalog >= 0 {
		s.side = s.sideAnalog
	}
	return s
}

func (s comparatorCircuitState) SampleAnalogInput(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	c, ok := tx.BlockLoaded(pos)
	comparator, valid := c.(RedstoneComparator)
	if !ok || !valid {
		return nil
	}
	analog := -1
	if output, ok := comparator.analogInputPower(pos, tx); ok {
		analog = int(output)
	}
	// The actor reads the live facing, but writes relative to the capacitor's
	// configured direction. A bare perpendicular rotation supplies its side
	// analog field; an opposite rotation leaves both previous fields intact.
	if comparator.Facing == s.facing {
		s.analog = analog
	} else if comparator.Facing != s.facing.Opposite() {
		s.sideAnalog = analog
	}
	s.ready = true
	return s
}

func (s comparatorCircuitState) Evaluate() (world.RedstoneCircuitState, world.Block) {
	output := comparatorTargetOutput(s.rear, s.side, s.subtract)
	if output == s.strength && output == s.block.Output {
		return s, nil
	}
	s.strength = output
	s.block.Output = output
	return s, s.block
}

func (s comparatorCircuitState) Refresh(before, after world.Block) world.RedstoneCircuitState {
	c, ok := after.(RedstoneComparator)
	if !ok {
		return nil
	}
	if previous, ok := before.(RedstoneComparator); ok && previous.Powered != c.Powered {
		// An external identifier replacement rebuilds configured direction and
		// analog samples while retaining the actor's output strength.
		return c.RedstoneCircuit()
	}
	s.block = c
	s.subtract, s.strength = c.Subtract, c.Output
	return s
}

func (s comparatorCircuitState) RefreshPublished(_, after world.Block) world.RedstoneCircuitState {
	c, ok := after.(RedstoneComparator)
	if !ok {
		return nil
	}
	s.block = c
	s.subtract, s.strength = c.Subtract, c.Output
	return s
}
