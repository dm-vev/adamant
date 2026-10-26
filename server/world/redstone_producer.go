package world

import "github.com/df-mc/dragonfly/server/block/cube"

// RedstoneCircuitProducer has a scene-graph strength independent of its block
// permutation. Setup initializes a pending component; actor callbacks update
// only the active component map, as in the native daylight detector.
type RedstoneCircuitProducer interface {
	RedstoneProducerPower() int
	RedstoneProducerOutput(power int) Block
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
		e.pendingProducers = make(map[cube.Pos]int)
	}
	e.pendingProducers[pos] = ClampRedstonePower(p.RedstoneProducerPower())
}

func (e *redstoneEngine) activateProducers() {
	if len(e.pendingProducers) == 0 {
		return
	}
	if e.producers == nil {
		e.producers = make(map[cube.Pos]int)
	}
	for p, power := range e.pendingProducers {
		e.producers[p] = power
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
	if before == power {
		return
	}
	e.producers[pos] = power
	b, loaded := r.tx.BlockLoaded(pos)
	if loaded {
		e.invalidateAroundBlockChange(pos, b, b, RedstoneUpdateCauseScheduledTick, r.tx.Range())
	}
}

func (e *redstoneEngine) producerOutput(pos cube.Pos, b Block) Block {
	if producer, ok := b.(RedstoneCircuitProducer); ok {
		return producer.RedstoneProducerOutput(e.producerPower(pos))
	}
	return b
}

func (e *redstoneEngine) producerPower(pos cube.Pos) int {
	if power, active := e.producers[pos]; active {
		return power
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
	power, active := e.producers[pos]
	initial, pending := e.pendingProducers[pos]
	r.tx.SetBlock(pos, b, nil)
	if _, ok := b.(RedstoneCircuitProducer); !ok {
		return
	}
	if active {
		if e.producers == nil {
			e.producers = make(map[cube.Pos]int)
		}
		e.producers[pos] = power
		delete(e.pendingProducers, pos)
	} else if pending {
		e.pendingProducers[pos] = initial
	}
}
