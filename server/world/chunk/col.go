package chunk

import (
	"github.com/df-mc/dragonfly/server/block/cube"
)

type Column struct {
	Chunk          *Chunk
	Entities       []Entity
	BlockEntities  []BlockEntity
	ScheduledTicks []SavedScheduledTick
	// InhabitedTime is the number of ticks this chunk has been inhabited by a
	// non-spectator player while within the world simulation area.
	InhabitedTime uint64
}

type BlockEntity struct {
	Pos  cube.Pos
	Data map[string]any
}

type Entity struct {
	ID   int64
	Data map[string]any
}

// ScheduledTickKind identifies the Java registry used by a scheduled tick.
type ScheduledTickKind uint8

const (
	BlockTick ScheduledTickKind = iota
	FluidTick
)

// ScheduledTick is a worldless generated tick. Type is the Java registry ID,
// and Priority uses Java TickPriority values (-3 through 3).
type ScheduledTick struct {
	Pos      cube.Pos
	Kind     ScheduledTickKind
	Type     string
	Priority int32
}

// SavedScheduledTick retains a paused delay and the native block state needed
// by BDS pending-tick storage. Type may be empty for records written by BDS;
// the World then derives Java tick identity from NativeBlock.
type SavedScheduledTick struct {
	ScheduledTick
	Delay       int32
	NativeBlock uint32
}
