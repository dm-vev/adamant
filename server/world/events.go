package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/event"
)

// WorldTickEvent is emitted on each active simulation tick of a World.
type WorldTickEvent struct {
	Tx   *Tx
	Tick int64
}

// BlockChangeEvent is emitted before a block at Pos is modified by SetBlock or
// SetBlockRID. Listeners may call Cancel to prevent the block change.
type BlockChangeEvent struct {
	event.Cancellable
	Tx     *Tx
	Pos    cube.Pos
	Old    Block
	New    Block
	OldRID uint32
	NewRID uint32
}

// EntitySpawnEvent is emitted when an entity is spawned into a World through
// Tx.AddEntity.
type EntitySpawnEvent struct {
	Tx     *Tx
	Entity Entity
}

// EntityDespawnEvent is emitted when an entity is removed from a World through
// Tx.RemoveEntity or unloaded with an unfinished column.
type EntityDespawnEvent struct {
	Tx     *Tx
	Entity Entity
}

// ChunkLoadEvent is emitted once when a loaded or generated column becomes
// ready in a World.
type ChunkLoadEvent struct {
	Tx     *Tx
	Pos    ChunkPos
	Column *Column
}

// ChunkUnloadEvent is emitted when a column is unloaded and closed in a World.
type ChunkUnloadEvent struct {
	Tx     *Tx
	Pos    ChunkPos
	Column *Column
}
