package redstone

import (
	"github.com/df-mc/dragonfly/server/block/cube"
)

// Graph represents the local redstone network for a single chunk.
type Graph struct {
	Gen       uint64
	Palette   []Node
	Offsets   []uint32
	Adjacency []NodeID
	Ports     []EdgePort

	States   []NodeState
	posIndex map[cube.Pos]int
	idIndex  map[NodeID]int
}

// Node describes a logical component in the redstone graph.
type Node struct {
	ID   NodeID
	Kind NodeKind
	Data uint16
	Pos  cube.Pos
}

// NodeKind identifies the behaviour of a node.
type NodeKind uint8

const (
	NodeUnknown NodeKind = iota
	NodePowerSource
	NodeWire
	NodeRepeater
	NodeComparator
	NodeLamp
	NodeObserver
	NodeConsumer
)

// EdgePort declares a cross-chunk connection at the chunk boundary.
type EdgePort struct {
	Dir      cube.Direction
	Neighbor ChunkID
	Node     NodeID
}

// NodeState stores mutable simulation data for a node.
type NodeState struct {
	Power        uint8
	Active       bool
	PendingTick  int64
	PendingPower uint8
	LastInput    uint8
}

// Clone returns a deep copy of the graph data.
func (g *Graph) Clone() Graph {
	if g == nil {
		return Graph{}
	}
	out := Graph{
		Gen: g.Gen,
	}
	if len(g.Palette) > 0 {
		out.Palette = append([]Node(nil), g.Palette...)
	}
	if len(g.Offsets) > 0 {
		out.Offsets = append([]uint32(nil), g.Offsets...)
	}
	if len(g.Adjacency) > 0 {
		out.Adjacency = append([]NodeID(nil), g.Adjacency...)
	}
	if len(g.Ports) > 0 {
		out.Ports = append([]EdgePort(nil), g.Ports...)
	}
	if len(g.States) > 0 {
		out.States = append([]NodeState(nil), g.States...)
	}
	if g.posIndex != nil {
		out.posIndex = make(map[cube.Pos]int, len(g.posIndex))
		for k, v := range g.posIndex {
			out.posIndex[k] = v
		}
	}
	if g.idIndex != nil {
		out.idIndex = make(map[NodeID]int, len(g.idIndex))
		for k, v := range g.idIndex {
			out.idIndex[k] = v
		}
	}
	out.prepare()
	return out
}

// prepare rebuilds internal indices and resizes state slices.
func (g *Graph) prepare() {
	if g == nil {
		return
	}
	if g.posIndex == nil || len(g.posIndex) != len(g.Palette) {
		g.posIndex = make(map[cube.Pos]int, len(g.Palette))
		for i := range g.Palette {
			g.posIndex[g.Palette[i].Pos] = i
		}
	}
	if g.idIndex == nil || len(g.idIndex) != len(g.Palette) {
		g.idIndex = make(map[NodeID]int, len(g.Palette))
		for i := range g.Palette {
			g.idIndex[g.Palette[i].ID] = i
		}
	}
	if len(g.States) < len(g.Palette) {
		g.States = append(g.States, make([]NodeState, len(g.Palette)-len(g.States))...)
	}
}

// Reindex rebuilds fast lookup tables (used by graph builders and tests).
func (g *Graph) Reindex() {
	g.prepare()
}

func (g *Graph) nodeByID(id NodeID) (int, *Node, *NodeState, bool) {
	g.prepare()
	idx, ok := g.idIndex[id]
	if !ok || idx < 0 || idx >= len(g.Palette) {
		return -1, nil, nil, false
	}
	return idx, &g.Palette[idx], &g.States[idx], true
}

func (g *Graph) nodeByPos(pos cube.Pos) (int, *Node, *NodeState, bool) {
	g.prepare()
	idx, ok := g.posIndex[pos]
	if !ok || idx < 0 || idx >= len(g.Palette) {
		return -1, nil, nil, false
	}
	return idx, &g.Palette[idx], &g.States[idx], true
}

func (g *Graph) neighbours(idx int) []NodeID {
	if idx < 0 || idx >= len(g.Palette) {
		return nil
	}
	start := 0
	if idx < len(g.Offsets) {
		start = int(g.Offsets[idx])
	}
	end := len(g.Adjacency)
	if idx+1 < len(g.Offsets) {
		end = int(g.Offsets[idx+1])
	}
	if start >= end || start >= len(g.Adjacency) {
		return nil
	}
	return g.Adjacency[start:end]
}
