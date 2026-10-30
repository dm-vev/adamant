package world

import "github.com/df-mc/dragonfly/server/block/cube"

// RedstoneCircuitProducer has a scene-graph strength independent of its block
// permutation. Setup initializes a pending component; actor callbacks update
// only the active component map, as in the native daylight detector.
type RedstoneCircuitProducer interface {
	RedstoneProducerPower() int
	RedstoneProducerOutput(power int) Block
}

// The scene component retains the output configuration captured during setup,
// including its direction. A same-identifier permutation edit does not recreate
// it; visible rotation and powered flags can therefore disagree with its output.
type redstoneProducer struct {
	power  int
	output RedstoneCircuitProducer
}

func (e *redstoneEngine) replaceProducer(pos cube.Pos, before, after Block) {
	p, ok := after.(RedstoneCircuitProducer)
	if !ok {
		delete(e.producers, pos)
		delete(e.pendingProducers, pos)
		return
	}
	beforeName, _ := before.EncodeBlock()
	afterName, _ := after.EncodeBlock()
	if beforeName == afterName {
		if _, ok := e.producers[pos]; ok {
			return
		}
		if _, ok := e.pendingProducers[pos]; ok {
			return
		}
	}
	delete(e.producers, pos)
	if e.pendingProducers == nil {
		e.pendingProducers = make(map[cube.Pos]redstoneProducer)
	}
	e.pendingProducers[pos] = redstoneProducer{power: ClampRedstonePower(p.RedstoneProducerPower()), output: p}
}

func (e *redstoneEngine) activateProducers() {
	if len(e.pendingProducers) == 0 {
		return
	}
	if e.producers == nil {
		e.producers = make(map[cube.Pos]redstoneProducer)
	}
	for p, state := range e.pendingProducers {
		e.producers[p] = state
		delete(e.pendingProducers, p)
	}
}

// SetProducerStrength follows the native active-map lookup. A callback before
// the first scene-graph update cannot modify the pending setup component.
func (r RedstoneTransaction) SetProducerStrength(pos cube.Pos, power int) {
	e := r.tx.World().redstone
	before, active := e.producers[pos]
	if !active {
		return
	}
	power = ClampRedstonePower(power)
	if before.power == power {
		return
	}
	before.power = power
	e.producers[pos] = before
	b, loaded := r.tx.BlockLoaded(pos)
	if loaded {
		e.invalidateAroundBlockChange(pos, b, b, RedstoneUpdateCauseScheduledTick, r.tx.Range())
	}
}

func (e *redstoneEngine) producerOutput(pos cube.Pos, b Block) Block {
	if producer, ok := b.(RedstoneCircuitProducer); ok {
		return e.producerBlock(pos, producer)
	}
	return b
}

func (e *redstoneEngine) producerPower(pos cube.Pos) int {
	return e.producerState(pos).power
}

func (e *redstoneEngine) producerBlock(pos cube.Pos, fallback RedstoneCircuitProducer) Block {
	state := e.producerState(pos)
	if state.output != nil {
		fallback = state.output
	}
	return fallback.RedstoneProducerOutput(state.power)
}

func (e *redstoneEngine) producerState(pos cube.Pos) redstoneProducer {
	if state, active := e.producers[pos]; active {
		return state
	}
	return e.pendingProducers[pos]
}

// ProducerStrength returns the current producer strength and whether its
// component is active. Before registration, strength is the pending setup value.
func (r RedstoneTransaction) ProducerStrength(pos cube.Pos) (power int, active bool) {
	e := r.tx.World().redstone
	_, active = e.producers[pos]
	return e.producerPower(pos), active
}

// SetProducerBlock replaces the visible permutation while retaining the
// producer, as the native daylight interaction does under the circuit lock.
// Ordinary identifier replacements still run normal producer setup.
func (r RedstoneTransaction) SetProducerBlock(pos cube.Pos, b Block) {
	e := r.tx.World().redstone
	state, active := e.producers[pos]
	initial, pending := e.pendingProducers[pos]
	r.tx.SetBlock(pos, b, nil)
	if _, ok := b.(RedstoneCircuitProducer); !ok {
		return
	}
	if active {
		if e.producers == nil {
			e.producers = make(map[cube.Pos]redstoneProducer)
		}
		e.producers[pos] = state
		delete(e.pendingProducers, pos)
	} else if pending {
		e.pendingProducers[pos] = initial
	}
}
