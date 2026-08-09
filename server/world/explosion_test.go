package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

func TestEntityExplosionSourceSnapshotsOrigin(t *testing.T) {
	entity := &movingExplosionSourceEntity{pos: mgl64.Vec3{1, 2, 3}}
	sources := []ExplosionSource{
		NewEntityExplosionSource(entity, 4),
		SnapshotExplosionSource(EntityExplosionSource{Entity: entity, ExplosionSize: 4}),
	}
	entity.pos = mgl64.Vec3{10, 20, 30}
	for _, src := range sources {
		if got, want := src.Position(), (mgl64.Vec3{1, 2, 3}); got != want {
			t.Fatalf("explosion origin = %v after entity movement, want %v", got, want)
		}
	}
}

type movingExplosionSourceEntity struct {
	pos mgl64.Vec3
}

func (*movingExplosionSourceEntity) Close() error            { return nil }
func (*movingExplosionSourceEntity) H() *EntityHandle        { return nil }
func (e *movingExplosionSourceEntity) Position() mgl64.Vec3  { return e.pos }
func (*movingExplosionSourceEntity) Rotation() cube.Rotation { return cube.Rotation{} }
