package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// defaultExplosionSize is the size used if a source does not specify one.
const defaultExplosionSize = 4

// ExplosionSource represents the source of an explosion.
type ExplosionSource interface {
	// Position returns the position at the centre of the explosion. It must
	// return the same position for the duration of an explosion.
	Position() mgl64.Vec3
	// Size returns the radius which entities/blocks are affected within.
	Size() float64
}

// EntityExplosionSource is used for an explosion caused by an entity.
type EntityExplosionSource struct {
	// Entity is the entity that caused the explosion.
	Entity Entity
	// ExplosionSize is the size of the explosion. Defaults to 4 if 0.
	ExplosionSize float64
	origin        mgl64.Vec3
	hasOrigin     bool
}

// NewEntityExplosionSource returns an entity explosion source with its origin fixed at the entity's current position.
func NewEntityExplosionSource(entity Entity, size float64) EntityExplosionSource {
	return EntityExplosionSource{Entity: entity, ExplosionSize: size, origin: entity.Position(), hasOrigin: true}
}

// Position ...
func (e EntityExplosionSource) Position() mgl64.Vec3 {
	if e.hasOrigin {
		return e.origin
	}
	return e.Entity.Position()
}

// Size ...
func (e EntityExplosionSource) Size() float64 {
	if e.ExplosionSize == 0 {
		return defaultExplosionSize
	}
	return e.ExplosionSize
}

// SnapshotExplosionSource fixes the position of entity explosion sources while preserving other source types.
func SnapshotExplosionSource(src ExplosionSource) ExplosionSource {
	switch s := src.(type) {
	case EntityExplosionSource:
		if !s.hasOrigin {
			s.origin, s.hasOrigin = s.Entity.Position(), true
		}
		return s
	case *EntityExplosionSource:
		snapshot := *s
		if !snapshot.hasOrigin {
			snapshot.origin, snapshot.hasOrigin = snapshot.Entity.Position(), true
		}
		return snapshot
	default:
		return src
	}
}

// BlockExplosionSource is used for an explosion caused by a block.
type BlockExplosionSource struct {
	// Block is the block that caused the explosion.
	Block Block
	// Pos is the position of the block that caused the explosion.
	Pos cube.Pos
	// ExplosionSize is the size of the explosion. Defaults to 4 if 0.
	ExplosionSize float64
}

// Position ...
func (b BlockExplosionSource) Position() mgl64.Vec3 {
	return b.Pos.Vec3Centre()
}

// Size ...
func (b BlockExplosionSource) Size() float64 {
	if b.ExplosionSize == 0 {
		return defaultExplosionSize
	}
	return b.ExplosionSize
}
