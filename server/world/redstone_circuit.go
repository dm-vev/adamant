package world

import (
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// RedstoneCircuitComponent owns a stateful Bedrock circuit component. Its
// transient state belongs to the world, independently of block permutations.
type RedstoneCircuitComponent interface {
	RedstoneCircuit() RedstoneCircuitState
	// RedstoneCircuitOutput merges an evaluated output into the live block.
	// Return nil if the output belongs to a different component orientation.
	RedstoneCircuitOutput(output Block) Block
}

// RedstoneCircuitState is an immutable circuit snapshot. Cache reads all inputs
// before any component is evaluated. Evaluate returns the next snapshot and an
// optional block permutation to publish after all evaluations finish.
type RedstoneCircuitState interface {
	Cache(pos cube.Pos, tx *Tx) RedstoneCircuitState
	Evaluate() (next RedstoneCircuitState, output Block)
	// Refresh applies a native component setup/updateDelay callback. A bare
	// permutation change need not execute that callback.
	Refresh(before, after Block) RedstoneCircuitState
}

// RedstoneCircuitCacheGroup caches components whose native dependency state
// can update another member's counters. The input map is read-only; returned
// snapshots are installed together before evaluation. order is the current
// world cache traversal through registered chunks and their component vectors.
type RedstoneCircuitCacheGroup interface {
	CacheCircuitGroup(pos cube.Pos, tx *Tx, states map[cube.Pos]RedstoneCircuitState, order []cube.Pos) map[cube.Pos]RedstoneCircuitState
}

// RedstoneCircuitStrength is a capacitor whose electrical output is separate
// from its visible block permutation, as for the observer's PulseCapacitor.
type RedstoneCircuitStrength interface {
	CircuitStrength() int
	WithCircuitStrength(power int) RedstoneCircuitState
}

// RedstoneCircuitDirection retains a native component's configured direction
// independently of permutation properties. A bare property edit need not run
// the block's component setup callback.
type RedstoneCircuitDirection interface{ CircuitDirection() cube.Face }

// RedstoneCircuitAnalogInput samples analog inputs once per circuit interval,
// in the chunk's block-actor pass. ComparatorCapacitor consumes
// the preceding actor sample; direct electrical inputs are cached on the circuit step.
type RedstoneCircuitAnalogInput interface {
	SampleAnalogInput(pos cube.Pos, tx *Tx) RedstoneCircuitState
}

// redstoneCircuitPublicationRefresher distinguishes a capacitor publishing its
// own state from an external identifier replacement that reruns native setup.
// External identifier replacement also discards the old capacitor's pending
// publication. RefreshPublished preserves that capacitor's transient state.
// Other components retain their ordinary Refresh behavior on publication.
type redstoneCircuitPublicationRefresher interface {
	RefreshPublished(before, after Block) RedstoneCircuitState
}

// RedstoneCircuitLateInput models a native consumer evaluated after source
// capacitors. Unlike transporters/repeaters, it reads this step's new source
// strengths, while still publishing its block permutation in the next pass.
type RedstoneCircuitLateInput interface {
	CacheAfterSources(pos cube.Pos, tx *Tx) RedstoneCircuitState
}

// RedstoneCircuitInputChange supplies consumer input values for publication
// events. A consumer such as a lamp has no electrical output of its own.
type RedstoneCircuitInputChange interface {
	CircuitInputChange() (oldPower, newPower int)
}

// RedstoneCircuitConsumer exposes a consumer's evaluated scene strength to its
// block actor. Unlike a capacitor, it supplies no electrical output to neighbours.
type RedstoneCircuitConsumer interface {
	CircuitConsumerStrength() int
}

// ConsumerPower reads the scene component, independently of the actor's visible
// state. The actor observes a circuit evaluation on its next tick.
func (r RedstoneTransaction) ConsumerPower(pos cube.Pos) (int, bool) {
	consumer, ok := r.tx.World().redstone.circuits[pos].(RedstoneCircuitConsumer)
	if !ok {
		return 0, false
	}
	return ClampRedstonePower(consumer.CircuitConsumerStrength()), true
}

// RedstoneCircuitHalfPulse publishes after ordinary components in the same
// chunk pass, as CircuitSystem::updateBlocks does for torch half-pulses.
type RedstoneCircuitHalfPulse interface{ CircuitHalfPulse() bool }

// RedstoneCircuitNeighbourChange handles a native neighbour notification,
// including notifications emitted by other circuit components.
type RedstoneCircuitNeighbourChange interface {
	CircuitNeighbourChange(pos, changed cube.Pos, tx *Tx) RedstoneCircuitState
}

// RedstoneCircuitBurnout is the world-owned capacitor's burnout status.
type RedstoneCircuitBurnout interface {
	CircuitBurnoutStatus() (burnedOut, recoverable bool)
	ClearCircuitBurnout() RedstoneCircuitState
}

// RedstoneCircuitPostPublisher applies side effects only after an uncancelled
// capacitor permutation has actually been published.
type RedstoneCircuitPostPublisher interface {
	CircuitPublished(pos cube.Pos, tx *Tx, before, after Block)
}

func (e *redstoneEngine) replaceCircuit(pos cube.Pos, before, after Block, publication bool) {
	e.replaceProducer(pos, before, after)
	e.circuitTraversal.replace(pos, after)
	component, ok := after.(RedstoneCircuitComponent)
	if !ok {
		delete(e.circuits, pos)
		delete(e.circuitOutputs, pos)
		delete(e.circuitPowers, pos)
		delete(e.circuitPowerOutputs, pos)
		delete(e.circuitCauses, pos)
		delete(e.circuitOutputCauses, pos)
		return
	}
	previous, _ := before.Hash()
	current, _ := after.Hash()
	if _, exists := e.circuits[pos]; exists && previous == current {
		beforeName, _ := before.EncodeBlock()
		afterName, _ := after.EncodeBlock()
		if beforeName != afterName {
			state, reinitialises := e.circuits[pos].(redstoneCircuitPublicationRefresher)
			if publication && reinitialises {
				e.circuits[pos] = state.RefreshPublished(before, after)
			} else {
				e.circuits[pos] = e.circuits[pos].Refresh(before, after)
				if reinitialises {
					delete(e.circuitOutputs, pos)
					delete(e.circuitPowerOutputs, pos)
					delete(e.circuitOutputCauses, pos)
				}
			}
		}
		return
	}
	if e.circuits == nil {
		e.circuits = make(map[cube.Pos]RedstoneCircuitState)
	}
	delete(e.circuitOutputs, pos)
	delete(e.circuitPowers, pos)
	delete(e.circuitPowerOutputs, pos)
	delete(e.circuitCauses, pos)
	delete(e.circuitOutputCauses, pos)
	e.circuits[pos] = component.RedstoneCircuit()
}

func (e *redstoneEngine) tickCircuits(tx *Tx, tick int64) {
	e.activateProducers()
	for pos, power := range e.circuitPowerOutputs {
		delete(e.circuitPowerOutputs, pos)
		b, loaded := tx.BlockLoaded(pos)
		if !loaded {
			continue
		} else if _, ok := b.(RedstoneCircuitComponent); !ok {
			continue
		}
		if power != e.circuitPowers[pos] {
			if e.circuitPowers == nil {
				e.circuitPowers = make(map[cube.Pos]int)
			}
			storeRedstonePower(e.circuitPowers, pos, power)
			e.invalidateAroundBlockChange(pos, b, b, RedstoneUpdateCauseScheduledTick, tx.Range())
		}
	}
	// CircuitSystem::evaluate changes capacitor strengths after the chunk's
	// block update pass. LevelChunk::tickImpl publishes them in the next pass.
	// Publish before collecting this step's inputs, keeping the two phases
	// separate even if there are no ordinary neighbor invalidations.
	positions := make([]cube.Pos, 0, len(e.circuitOutputs))
	for pos := range e.circuitOutputs {
		positions = append(positions, pos)
	}
	slices.SortFunc(positions, func(a, b cube.Pos) int {
		half := func(pos cube.Pos) bool {
			s, ok := e.circuits[pos].(RedstoneCircuitHalfPulse)
			return ok && s.CircuitHalfPulse()
		}
		if half(a) != half(b) {
			if half(a) {
				return 1
			}
			return -1
		}
		return compareBlockPos(a, b)
	})
	for _, pos := range positions {
		output := e.circuitOutputs[pos]
		delete(e.circuitOutputs, pos)
		d := e.circuitOutputCauses[pos]
		delete(e.circuitOutputCauses, pos)
		b, loaded := tx.BlockLoaded(pos)
		if !loaded {
			continue
		}
		component, ok := b.(RedstoneCircuitComponent)
		if !ok {
			continue
		}
		output = component.RedstoneCircuitOutput(output)
		if output == nil {
			continue
		}
		power := 0
		if source, ok := output.(RedstonePowerSource); ok {
			for _, face := range cube.Faces() {
				power = max(power, source.RedstonePower(pos, tx, face))
			}
		}
		d.cause = RedstoneUpdateCauseScheduledTick
		if !d.hasSource {
			d.source = pos
			d.hasSource = true
			d.sourceRedstoneRelevant = isRedstoneRelevant(b)
		}
		oldPower, newPower := e.sourcePower(pos, tx), ClampRedstonePower(power)
		if consumer, ok := e.circuits[pos].(RedstoneCircuitInputChange); ok {
			oldPower, newPower = consumer.CircuitInputChange()
		}
		update := d.redstoneUpdate(pos, b, oldPower, newPower, tick)
		update.After = output
		if e.redstoneUpdateAllowed(tx, update) {
			tx.SetBlock(pos, output, &SetOpts{DisableRedstoneUpdates: true, circuitPublication: true})
			e.invalidateAroundWith(pos, d.propagatedFrom(pos), tx.Range())
			if state, ok := e.circuits[pos].(RedstoneCircuitPostPublisher); ok {
				state.CircuitPublished(pos, tx, b, output)
			}
		}
	}
	// Dimension::tickRedstone uses CIRCUIT_TICK_RATE=2. Keep every component
	// on this shared clock; a neighbor update must not start a private clock.
	if tick&1 != 0 {
		return
	}
	e.circuitTraversal.flush()
	if len(e.circuits) == 0 {
		return
	}
	defer func() { clear(e.circuitInputs) }()
	positions = e.circuitTraversal.positions(e.circuits)
	for _, pos := range positions {
		b, loaded := tx.BlockLoaded(pos)
		if !loaded {
			delete(e.circuits, pos)
			continue
		}
		if _, ok := b.(RedstoneCircuitComponent); !ok {
			delete(e.circuits, pos)
			continue
		}
	}
	// Transporter sources are a flattened graph with accumulated attenuation,
	// including nonadjacent dust steps. Cache the whole network once rather
	// than treating a wire's six immediate faces as its complete source list.
	var relayers []cube.Pos
	for _, pos := range positions {
		if b, loaded := tx.BlockLoaded(pos); loaded {
			if _, ok := b.(RedstonePowerRelayer); ok {
				relayers = append(relayers, pos)
			}
		}
	}
	if len(relayers) != 0 {
		graph := e.compile(tx, relayers)
		powers := e.graphPower(tx, graph)
		if e.circuitInputs == nil {
			e.circuitInputs = make(map[cube.Pos]int)
		}
		for i, node := range graph.nodes {
			if b, loaded := tx.BlockLoaded(node.pos); loaded {
				if _, ok := b.(RedstonePowerRelayer); ok {
					e.circuitInputs[node.pos] = powers[i]
					if e.power[node.pos] != powers[i] {
						if e.circuitCauses == nil {
							e.circuitCauses = make(map[cube.Pos]redstoneDirty)
						}
						e.circuitCauses[node.pos] = redstoneDirtyContext(e.dirty, node.pos)
					}
				}
			}
		}
	}
	groupCached := make(map[cube.Pos]bool)
	for _, pos := range positions {
		state := e.circuits[pos]
		if state == nil || groupCached[pos] {
			continue
		}
		if _, late := state.(RedstoneCircuitLateInput); late {
			continue
		}
		if group, ok := state.(RedstoneCircuitCacheGroup); ok {
			if snapshots := group.CacheCircuitGroup(pos, tx, e.circuits, positions); len(snapshots) != 0 {
				for member, next := range snapshots {
					e.circuits[member], groupCached[member] = next, true
				}
				continue
			}
		}
		if cached := state.Cache(pos, tx); cached != nil {
			e.circuits[pos] = cached
		} else {
			delete(e.circuits, pos)
		}
	}
	clear(e.circuitInputs)
	for _, pos := range positions {
		state := e.circuits[pos]
		if state == nil {
			continue
		}
		if _, late := state.(RedstoneCircuitLateInput); late {
			continue
		}
		next, output := state.Evaluate()
		e.circuits[pos] = next
		if source, ok := next.(RedstoneCircuitStrength); ok {
			if e.circuitPowerOutputs == nil {
				e.circuitPowerOutputs = make(map[cube.Pos]int)
			}
			e.circuitPowerOutputs[pos] = ClampRedstonePower(source.CircuitStrength())
		}
		if output != nil {
			if e.circuitOutputs == nil {
				e.circuitOutputs = make(map[cube.Pos]Block)
			}
			e.circuitOutputs[pos] = output
			if e.circuitOutputCauses == nil {
				e.circuitOutputCauses = make(map[cube.Pos]redstoneDirty)
			}
			e.circuitOutputCauses[pos] = e.circuitCauses[pos]
		}
	}
	e.cacheLateCircuitInputs(tx, positions)
	for _, pos := range positions {
		state := e.circuits[pos]
		if _, late := state.(RedstoneCircuitLateInput); !late {
			continue
		}
		next, output := state.Evaluate()
		e.circuits[pos] = next
		if output != nil {
			if e.circuitOutputs == nil {
				e.circuitOutputs = make(map[cube.Pos]Block)
			}
			e.circuitOutputs[pos] = output
			if e.circuitOutputCauses == nil {
				e.circuitOutputCauses = make(map[cube.Pos]redstoneDirty)
			}
			e.circuitOutputCauses[pos] = e.circuitCauses[pos]
		}
	}
}

// SampleAnalogInput runs the component's block-actor input read at its position
// in the chunk tick order, rather than sampling all comparators after all actors.
func (r RedstoneTransaction) SampleAnalogInput(pos cube.Pos) {
	e := r.tx.World().redstone
	if actor, ok := e.circuits[pos].(RedstoneCircuitAnalogInput); ok {
		e.circuits[pos] = actor.SampleAnalogInput(pos, r.tx)
	}
}

func (e *redstoneEngine) cacheLateCircuitInputs(tx *Tx, positions []cube.Pos) {
	previous := e.readingCircuitOutputs
	e.readingCircuitOutputs = true
	defer func() { e.readingCircuitOutputs = previous }()
	for _, pos := range positions {
		if state, late := e.circuits[pos].(RedstoneCircuitLateInput); late {
			e.circuits[pos] = state.CacheAfterSources(pos, tx)
		}
	}
}

// indexCircuitChunk installs components from saved or generated permutations.
// Palette filtering avoids visiting blocks in subchunks without components.
func (w *World) indexCircuitChunk(pos ChunkPos, col *Column) {
	col.circuitsIndexed = true
	for subIndex, sub := range col.Chunk.Sub() {
		if len(sub.Layers()) == 0 {
			continue
		}
		palette := sub.Layers()[0].Palette()
		components := make(map[uint32]Block)
		for i := range palette.Len() {
			rid := palette.Value(uint16(i))
			b := w.conf.Blocks.BlockByRuntimeIDOrAir(rid)
			if _, ok := b.(RedstoneCircuitComponent); ok || isRedstoneRelevant(b) {
				components[rid] = b
			}
		}
		if len(components) == 0 {
			continue
		}
		baseY := int(col.Chunk.SubY(int16(subIndex)))
		for x := range 16 {
			for z := range 16 {
				for y := max(baseY, w.ra.Min()); y <= min(baseY+15, w.ra.Max()); y++ {
					b, ok := components[col.Chunk.Block(uint8(x), int16(y), uint8(z), 0)]
					if !ok {
						continue
					}
					blockPos := cube.Pos{int(pos[0])*16 + x, y, int(pos[1])*16 + z}
					if nbt, ok := col.BlockEntities[blockPos]; ok {
						b = nbt
					}
					w.redstone.replaceCircuit(blockPos, b, b, false)
				}
			}
		}
	}
}
