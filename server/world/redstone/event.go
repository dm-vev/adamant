package redstone

import "github.com/df-mc/dragonfly/server/block/cube"

// EventKind enumerates the intent of a redstone event flowing through the system.
type EventKind uint8

const (
	EventUnknown EventKind = iota
	EventSignalRise
	EventSignalFall
	EventTick
	EventComparator
	EventObserver
	EventPowerChange
	EventBlockUpdate
	EventNeighbourChange
	EventOutput
)

// NodeID uniquely identifies a node within a chunk-local redstone graph.
type NodeID uint32

// Event represents a unit of work for a chunk worker.
type Event struct {
	Pos   cube.Pos
	Kind  EventKind
	Power uint8
	Tick  int64
	Node  NodeID
	Meta  uint32
}

// Key collapses the event down to a coalescing key.
func (e Event) Key() EventKey {
	return EventKey{
		Pos:  e.Pos,
		Kind: e.Kind,
		Node: e.Node,
	}
}

// EventKey is used for coalescing duplicate events when inboxes overflow.
type EventKey struct {
	Pos  cube.Pos
	Kind EventKind
	Node NodeID
}

// ChunkID identifies a chunk in Morton space without tying the package to world.ChunkPos.
type ChunkID struct {
	X, Z int32
}

// Morton returns the deterministic order value for the chunk.
func (id ChunkID) Morton() uint64 {
	return morton2(toUnsigned(id.X), toUnsigned(id.Z))
}

// mortonKey returns a sortable key for ordering events deterministically.
func (k EventKey) mortonKey() uint64 {
	return morton2(toUnsigned(int32(k.Pos[0])), toUnsigned(int32(k.Pos[2])))<<16 |
		uint64(k.Kind)<<8 |
		uint64(k.Node&0xff)
}

func toUnsigned(v int32) uint32 {
	return uint32(v) ^ (1 << 31)
}

func splitBy1(x uint32) uint64 {
	x64 := uint64(x)
	x64 = (x64 | x64<<16) & 0x0000FFFF0000FFFF
	x64 = (x64 | x64<<8) & 0x00FF00FF00FF00FF
	x64 = (x64 | x64<<4) & 0x0F0F0F0F0F0F0F0F
	x64 = (x64 | x64<<2) & 0x3333333333333333
	x64 = (x64 | x64<<1) & 0x5555555555555555
	return x64
}

func morton2(x, z uint32) uint64 {
	return splitBy1(x) | splitBy1(z)<<1
}

// sortEventsDeterministic sorts in place using the deterministic morton key.
func sortEventsDeterministic(events []Event) {
	if len(events) < 2 {
		return
	}
	slicesSortFunc(events, func(a, b Event) int {
		ka := a.Key().mortonKey()
		kb := b.Key().mortonKey()
		switch {
		case ka < kb:
			return -1
		case ka > kb:
			return 1
		default:
			return 0
		}
	})
}

// slicesSortFunc is a local wrapper to avoid importing slices in every file.
func slicesSortFunc[E any](s []E, cmp func(a, b E) int) {
	if len(s) < 2 {
		return
	}
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && cmp(s[j-1], s[j]) > 0 {
			s[j-1], s[j] = s[j], s[j-1]
			j--
		}
	}
}
