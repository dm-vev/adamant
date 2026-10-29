package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Native container consumers sample after source evaluation and publish in
// the next chunk pass. Dispensers/droppers/crafters queue their four-GT action;
// hopper block actors instead read the published lock during normal ticks.
type containerCircuit struct {
	block                poweredContainer
	input, previousInput int
	activationDelay      int
	evaluated            bool
}

type poweredContainer interface {
	world.Block
	circuitPowered() bool
	withCircuitPowered(bool) poweredContainer
}

func (d Dispenser) RedstoneCircuit() world.RedstoneCircuitState {
	return containerCircuit{block: d, activationDelay: 4}
}
func (d Dispenser) circuitPowered() bool { return d.Triggered }
func (d Dispenser) withCircuitPowered(powered bool) poweredContainer {
	d.Triggered = powered
	return d
}

// DropperBlock registers DispenserBlock::_onRedstoneUpdate in the native
// constructor (0x9791b62). Both blocks share the consumer and queue cadence.
func (d Dropper) RedstoneCircuit() world.RedstoneCircuitState {
	return containerCircuit{block: d, activationDelay: 4}
}
func (d Dropper) circuitPowered() bool { return d.Triggered }
func (d Dropper) withCircuitPowered(powered bool) poweredContainer {
	d.Triggered = powered
	return d
}
func (d Dropper) RedstoneCircuitOutput(output world.Block) world.Block {
	if next, ok := output.(Dropper); ok {
		d.Triggered = next.Triggered
		return d
	}
	return nil
}
func (d Dispenser) RedstoneCircuitOutput(output world.Block) world.Block {
	if next, ok := output.(Dispenser); ok {
		d.Triggered = next.Triggered
		return d
	}
	return nil
}

// CrafterBlock::_onRedstoneUpdate (0x6ec3580) queues the same four-GT rising
// edge. Falling power changes triggered_bit only; the actor's visual clock
// continues independently. Merge into the live actor so an evaluated circuit
// cannot overwrite inventory, disabled slots or crafting progress.
func (c Crafter) RedstoneCircuit() world.RedstoneCircuitState {
	return containerCircuit{block: c, activationDelay: 4}
}
func (c Crafter) circuitPowered() bool { return c.Triggered }
func (c Crafter) withCircuitPowered(powered bool) poweredContainer {
	c.Triggered = powered
	return c
}
func (c Crafter) RedstoneCircuitOutput(output world.Block) world.Block {
	if next, ok := output.(Crafter); ok {
		c.Triggered = next.Triggered
		return c
	}
	return nil
}

// NoteBlock::_onRedstoneUpdate uses the same native consumer clock, storing
// power in its actor rather than a permutation. Keep the live pitch when
// publishing an evaluated edge.
func (n Note) RedstoneCircuit() world.RedstoneCircuitState { return containerCircuit{block: n} }
func (n Note) circuitPowered() bool                        { return n.Powered }
func (n Note) withCircuitPowered(powered bool) poweredContainer {
	n.Powered = powered
	return n
}
func (n Note) RedstoneCircuitOutput(output world.Block) world.Block {
	if next, ok := output.(Note); ok {
		n.Powered = next.Powered
		return n
	}
	return nil
}

// HopperBlock::_onRedstoneUpdate (0x66baf90) publishes the toggle flag without
// a scheduled dispense action. Keep the inventory and cooldowns from the live
// actor when applying that flag.
func (h Hopper) RedstoneCircuit() world.RedstoneCircuitState {
	return containerCircuit{block: h}
}
func (h Hopper) circuitPowered() bool { return h.Powered }
func (h Hopper) withCircuitPowered(powered bool) poweredContainer {
	h.Powered = powered
	return h
}
func (h Hopper) RedstoneCircuitOutput(output world.Block) world.Block {
	if next, ok := output.(Hopper); ok {
		h.Powered = next.Powered
		return h
	}
	return nil
}

func (s containerCircuit) Cache(cube.Pos, *world.Tx) world.RedstoneCircuitState { return s }
func (s containerCircuit) CacheAfterSources(pos cube.Pos, tx *world.Tx) world.RedstoneCircuitState {
	s.block = tx.Block(pos).(poweredContainer)
	s.previousInput, s.input = s.input, 0
	for _, face := range cube.Faces() {
		s.input = max(s.input, tx.RedstonePowerFrom(pos, face))
	}
	return s
}
func (s containerCircuit) Evaluate() (world.RedstoneCircuitState, world.Block) {
	first := !s.evaluated
	s.evaluated = true
	if _, note := s.block.(Note); note {
		// NoteBlock::_onRedstoneUpdate returns for mIsFirstTime, without
		// changing its actor's power history. Only subsequent input edges
		// reach that actor, even if initial scene power was already positive.
		if first || s.input == s.previousInput {
			return s, nil
		}
	} else {
		// A saved triggered/toggle flag may differ from the initial zero-strength
		// consumer. Its first publication must reconcile that flag even without
		// an electrical edge (ConsumerComponent::evaluate's initial update flag).
		if s.input == s.previousInput && s.block.circuitPowered() == (s.input > 0) {
			return s, nil
		}
	}
	s.block = s.block.withCircuitPowered(s.input > 0)
	return s, s.block
}
func (s containerCircuit) CircuitInputChange() (int, int) { return s.previousInput, s.input }
func (s containerCircuit) CircuitActorOutput() bool {
	_, note := s.block.(Note)
	return note
}
func (s containerCircuit) CircuitPublished(pos cube.Pos, tx *world.Tx, before, after world.Block) {
	if c, ok := after.(Crafter); ok {
		c.NotifyTriggered()
	}
	if n, ok := after.(Note); ok {
		n.RedstonePowerPostUpdate(pos, tx, before, after, s.previousInput, s.input)
	}
	if s.activationDelay == 0 {
		return
	}
	if old, ok := before.(poweredContainer); ok && !old.circuitPowered() {
		if next, ok := after.(poweredContainer); ok && next.circuitPowered() {
			tx.ScheduleBlockUpdate(pos, after, gameTicks(s.activationDelay))
		}
	}
}
func (s containerCircuit) Refresh(_, after world.Block) world.RedstoneCircuitState {
	d, ok := after.(poweredContainer)
	if !ok {
		return nil
	}
	s.block = d
	return s
}
