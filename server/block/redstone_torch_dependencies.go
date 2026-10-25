package block

import (
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// CacheCircuitGroup follows the common-support circular-reference path in
// RedstoneTorchCapacitor::updateDependencies (0x66d9cd0). PushCircularReference
// (0x66dadc0) enqueues north, west, south, east around the powered support.
// CircuitSystem's chunk/vector traversal, rather than this ring order,
// determines which member caches first.
func (s torchCircuitState) CacheCircuitGroup(pos cube.Pos, tx *world.Tx, states map[cube.Pos]world.RedstoneCircuitState, order []cube.Pos) map[cube.Pos]world.RedstoneCircuitState {
	t, ok := tx.BlockLoaded(pos)
	torch, isTorch := t.(RedstoneTorch)
	if !ok || !isTorch {
		return nil
	}
	support := pos.Side(s.direction)
	if above, loaded := tx.BlockLoaded(support.Side(cube.FaceUp)); !loaded {
		return nil
	} else if _, wire := above.(RedstoneWire); !wire {
		return nil
	}
	inputs := torch.circuitInputs(pos, tx)
	sources := make(map[cube.Pos]bool)
	for _, parent := range inputs {
		if parent.group != torchSourcePoweredBlock && parent.group != 0x20000 {
			continue
		}
		for _, child := range parent.sources {
			if child.direct && child.componentType == torchSourceTorch {
				sources[child.pos] = true
			}
		}
	}
	var members []cube.Pos
	var nodes []torchCircuitState
	for _, face := range [...]cube.Face{cube.FaceNorth, cube.FaceWest, cube.FaceSouth, cube.FaceEast} {
		p := support.Side(face)
		if !sources[p] {
			continue
		}
		b, loaded := tx.BlockLoaded(p)
		candidate, ok := b.(RedstoneTorch)
		if !loaded || !ok {
			continue
		}
		state, ok := states[p].(torchCircuitState)
		if !ok || p.Side(state.direction) != support {
			continue
		}
		above := p.Side(cube.FaceUp)
		if b, loaded := tx.BlockLoaded(above); !loaded || !world.RedstoneFullPowerConductor(above, b, tx) {
			continue
		}
		state.block = candidate
		members = append(members, p)
		nodes = append(nodes, state)
	}
	if len(members) < 2 || !slices.Contains(members, pos) {
		return nil
	}
	changed := false
	for _, state := range nodes {
		changed = changed || !slices.Equal(state.group, members)
	}
	if changed {
		// The native dependency constructor clears the first/last counters
		// and holds the last one at 16 while linking the deque into a ring.
		nodes[0].selfPowerCount = 0
		nodes[len(nodes)-1].selfPowerCount = 16
	}
	index := make(map[cube.Pos]int, len(members))
	for i, p := range members {
		index[p] = i
		nodes[i].group = members
		nodes[i].nextOrder = &nodes[(i+1)%len(nodes)].torchCircuit
	}
	for _, p := range order {
		if i, ok := index[p]; ok {
			nodes[i].cache(p, nodes[i].block.circuitInputs(p, tx))
		}
	}
	out := make(map[cube.Pos]world.RedstoneCircuitState, len(nodes))
	for i, p := range members {
		out[p] = nodes[i]
	}
	return out
}
