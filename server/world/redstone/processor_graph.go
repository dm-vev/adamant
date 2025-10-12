package redstone

// NewGraphProcessor returns the default redstone processor implementation.
func NewGraphProcessor() Processor {
	return graphProcessor{}
}

type graphProcessor struct{}

func (graphProcessor) HandleEvent(_ ChunkID, g *Graph, ev Event, emit Emitter) {
	if g == nil {
		return
	}
	idx, node, state := locateNode(g, ev)
	if node == nil || state == nil {
		return
	}
	switch node.Kind {
	case NodePowerSource:
		handleSource(g, idx, node, state, ev, emit)
	case NodeWire:
		handleWire(g, idx, node, state, ev, emit)
	case NodeRepeater:
		handleRepeater(g, idx, node, state, ev, emit)
	case NodeComparator:
		handleComparator(g, idx, node, state, ev, emit)
	case NodeLamp, NodeConsumer:
		handleConsumer(g, idx, node, state, ev, emit)
	case NodeObserver:
		handleObserver(g, idx, node, state, ev, emit)
	default:
		// Unknown nodes are ignored for now.
	}
}

func locateNode(g *Graph, ev Event) (int, *Node, *NodeState) {
	if ev.Kind == EventTick || ev.Node != 0 {
		if idx, node, state, ok := g.nodeByID(ev.Node); ok {
			return idx, node, state
		}
	}
	if idx, node, state, ok := g.nodeByPos(ev.Pos); ok {
		return idx, node, state
	}
	return -1, nil, nil
}

// --- Source handling -------------------------------------------------------

const (
	sourceGeneric uint16 = iota
	sourceLever
	sourceButton
	sourcePressurePlate
	sourceTorch
	sourceObserver
	sourceDaylight
)

const (
	// SourceSubtypeGeneric represents a source without specialised behaviour.
	SourceSubtypeGeneric = sourceGeneric
	// SourceSubtypeLever represents a lever source.
	SourceSubtypeLever = sourceLever
	// SourceSubtypeTorch represents a redstone torch source.
	SourceSubtypeTorch = sourceTorch
)

func handleSource(g *Graph, idx int, node *Node, state *NodeState, ev Event, emit Emitter) {
	subtype := node.Data & 0x7
	switch subtype {
	case sourceTorch:
		handleTorch(g, idx, node, state, ev, emit)
		return
	}

	switch ev.Kind {
	case EventSignalRise, EventBlockUpdate:
		newPower := ev.Power
		if newPower == 0 {
			newPower = 15
		}
		if subtype == sourceButton || subtype == sourcePressurePlate {
			duration := int64(ev.Meta)
			if duration <= 0 {
				duration = 10
			}
			state.PendingTick = ev.Tick + duration
			state.PendingPower = 0
			emit.Local(Event{
				Pos:  node.Pos,
				Kind: EventTick,
				Tick: state.PendingTick,
				Node: node.ID,
			})
		} else {
			state.PendingTick = 0
		}
		updateSourcePower(g, idx, node, state, newPower, ev.Tick, emit)
	case EventSignalFall:
		state.PendingTick = 0
		updateSourcePower(g, idx, node, state, 0, ev.Tick, emit)
	case EventTick:
		if state.PendingTick != 0 && ev.Tick >= state.PendingTick {
			state.PendingTick = 0
			updateSourcePower(g, idx, node, state, state.PendingPower, ev.Tick, emit)
		}
	case EventPowerChange:
		// lever receiving neighbour power only matters for inversion-style sources.
		if subtype == sourceLever {
			if ev.Power > 0 {
				updateSourcePower(g, idx, node, state, 0, ev.Tick, emit)
			}
		}
	}
}

func updateSourcePower(g *Graph, idx int, node *Node, state *NodeState, newPower uint8, tick int64, emit Emitter) {
	if state.Power == newPower {
		return
	}
	state.Power = newPower
	state.Active = newPower > 0
	propagatePower(g, idx, node, newPower, tick, emit)
}

func handleTorch(g *Graph, idx int, node *Node, state *NodeState, ev Event, emit Emitter) {
	switch ev.Kind {
	case EventPowerChange:
		if ev.Power > 0 {
			updateSourcePower(g, idx, node, state, 0, ev.Tick, emit)
		} else {
			updateSourcePower(g, idx, node, state, 15, ev.Tick, emit)
		}
	case EventSignalRise, EventBlockUpdate:
		updateSourcePower(g, idx, node, state, 15, ev.Tick, emit)
	case EventSignalFall:
		updateSourcePower(g, idx, node, state, 0, ev.Tick, emit)
	}
}

// --- Wire handling ---------------------------------------------------------

func handleWire(g *Graph, idx int, node *Node, state *NodeState, ev Event, emit Emitter) {
	switch ev.Kind {
	case EventPowerChange, EventSignalRise, EventSignalFall:
		newPower := ev.Power
		if ev.Kind == EventSignalRise && newPower == 0 {
			newPower = 15
		}
		if newPower > 15 {
			newPower = 15
		}
		if state.Power == newPower {
			return
		}
		state.Power = newPower
		state.Active = newPower > 0
		emit.Output(Event{
			Kind:  EventOutput,
			Pos:   node.Pos,
			Power: newPower,
			Tick:  ev.Tick,
			Node:  node.ID,
			Meta:  uint32(newPower),
		})
		propagatePower(g, idx, node, newPower, ev.Tick, emit)
	}
}

// --- Repeater handling -----------------------------------------------------

func handleRepeater(g *Graph, idx int, node *Node, state *NodeState, ev Event, emit Emitter) {
	delay := 1 + int(node.Data&0x3)
	targetTick := ev.Tick + int64(delay)
	switch ev.Kind {
	case EventPowerChange, EventSignalRise, EventSignalFall:
		input := ev.Power
		state.LastInput = input
		state.PendingTick = targetTick
		if input > 0 {
			state.PendingPower = 15
		} else {
			state.PendingPower = 0
		}
		emit.Local(Event{
			Pos:  node.Pos,
			Kind: EventTick,
			Tick: targetTick,
			Node: node.ID,
		})
	case EventTick:
		if state.PendingTick != 0 && ev.Tick >= state.PendingTick {
			state.PendingTick = 0
			if state.Power == state.PendingPower {
				return
			}
			state.Power = state.PendingPower
			state.Active = state.Power > 0
			propagatePower(g, idx, node, state.Power, ev.Tick, emit)
		}
	}
}

// --- Comparator handling ---------------------------------------------------

func handleComparator(g *Graph, idx int, node *Node, state *NodeState, ev Event, emit Emitter) {
	switch ev.Kind {
	case EventPowerChange, EventSignalRise, EventSignalFall:
		newPower := ev.Power
		if newPower > 15 {
			newPower = 15
		}
		if state.Power == newPower {
			return
		}
		state.Power = newPower
		state.Active = newPower > 0
		propagatePower(g, idx, node, newPower, ev.Tick, emit)
	}
}

// --- Consumer handling -----------------------------------------------------

func handleConsumer(_ *Graph, _ int, node *Node, state *NodeState, ev Event, emit Emitter) {
	switch ev.Kind {
	case EventPowerChange, EventSignalRise, EventSignalFall:
		newPower := ev.Power
		if ev.Kind == EventSignalRise && newPower == 0 {
			newPower = 15
		}
		active := newPower > 0
		if state.Active == active && state.Power == newPower {
			return
		}
		state.Active = active
		state.Power = newPower
		emit.Output(Event{
			Kind:  EventOutput,
			Pos:   node.Pos,
			Power: newPower,
			Tick:  ev.Tick,
			Node:  node.ID,
			Meta:  boolToMeta(active),
		})
	}
}

// --- Observer handling -----------------------------------------------------

func handleObserver(g *Graph, idx int, node *Node, state *NodeState, ev Event, emit Emitter) {
	switch ev.Kind {
	case EventBlockUpdate, EventPowerChange, EventSignalRise, EventSignalFall:
		input := uint8(ev.Meta & 0xFF)
		if state.LastInput == input {
			return
		}
		state.LastInput = input
		state.Power = 15
		state.Active = true
		propagatePower(g, idx, node, 15, ev.Tick, emit)
		state.PendingTick = ev.Tick + 1
		state.PendingPower = 0
		emit.Local(Event{
			Pos:  node.Pos,
			Kind: EventTick,
			Tick: state.PendingTick,
			Node: node.ID,
		})
	case EventTick:
		if state.PendingTick != 0 && ev.Tick >= state.PendingTick {
			state.PendingTick = 0
			if state.Power == 0 {
				return
			}
			state.Power = 0
			state.Active = false
			propagatePower(g, idx, node, 0, ev.Tick, emit)
		}
	}
}

// --- Helpers ----------------------------------------------------------------

func propagatePower(g *Graph, idx int, node *Node, power uint8, tick int64, emit Emitter) {
	neighbours := g.neighbours(idx)
	for _, nbID := range neighbours {
		_, nbNode, _, ok := g.nodeByID(nbID)
		if !ok || nbNode == nil {
			continue
		}
		targetPower := adjustPowerForTarget(power, nbNode.Kind)
		emit.Local(Event{
			Pos:   nbNode.Pos,
			Kind:  EventPowerChange,
			Power: targetPower,
			Tick:  tick,
			Node:  nbNode.ID,
		})
	}
	for _, port := range g.Ports {
		if port.Node != node.ID {
			continue
		}
		targetPower := adjustPowerForTarget(power, NodeUnknown)
		emit.Remote(port.Neighbor, Event{
			Pos:   node.Pos,
			Kind:  EventPowerChange,
			Power: targetPower,
			Tick:  tick,
		})
	}
}

func adjustPowerForTarget(power uint8, kind NodeKind) uint8 {
	if power == 0 {
		return 0
	}
	switch kind {
	case NodeWire:
		if power <= 1 {
			return 0
		}
		return power - 1
	default:
		return power
	}
}

func boolToMeta(v bool) uint32 {
	if v {
		return 1
	}
	return 0
}
