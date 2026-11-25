package world

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/event"
	"github.com/go-gl/mathgl/mgl64"
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

// ExplosionEvent is emitted before an explosion damages entities and blocks.
// Listeners may modify Entities, Blocks, ItemDropChance, and SpawnFire, or
// call Cancel to abort the explosion.
type ExplosionEvent struct {
	event.Cancellable
	Tx             *Tx
	Source         ExplosionSource
	Entities       []Entity
	Blocks         []cube.Pos
	ItemDropChance float64
	SpawnFire      bool
}

// WeatherChangeEvent is emitted before a World's rain or thunder state changes.
// Listeners may call Cancel to prevent the weather change.
type WeatherChangeEvent struct {
	event.Cancellable
	World      *World
	Raining    bool
	Thundering bool
	Duration   time.Duration
}

// ProjectileHitEvent is emitted when a projectile hits a block or entity.
// Listeners may call Cancel to suppress impact handling.
type ProjectileHitEvent struct {
	event.Cancellable
	Tx         *Tx
	Projectile Entity
	Owner      Entity
	Pos        mgl64.Vec3
	BlockPos   cube.Pos
	HasBlock   bool
	HitEntity  Entity
}

// EntityDamageEvent is emitted before a non-player living entity takes damage.
// Listeners may modify Damage or call Cancel to prevent the damage.
type EntityDamageEvent struct {
	event.Cancellable
	Tx     *Tx
	Entity Entity
	Damage float64
	Source DamageSource
}

// EntityDeathEvent is emitted when a non-player living entity dies.
type EntityDeathEvent struct {
	Tx     *Tx
	Entity Entity
	Source DamageSource
}
