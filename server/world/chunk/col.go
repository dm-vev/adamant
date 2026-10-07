package chunk

import (
	"github.com/df-mc/dragonfly/server/block/cube"
)

type Column struct {
	Chunk           *Chunk
	Entities        []Entity
	BlockEntities   []BlockEntity
	Tick            int64
	ScheduledBlocks []ScheduledBlockUpdate
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

type ScheduledBlockUpdate struct {
	Pos   cube.Pos
	Block uint32
	Tick  int64
}
