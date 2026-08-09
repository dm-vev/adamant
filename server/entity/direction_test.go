package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestExplosionImpulseAtExactOrigin(t *testing.T) {
	pos := mgl64.Vec3{1, 2, 3}
	src := directionTestExplosionSource{pos: pos}
	if got := ExplosionImpulse(&directionTestEntity{pos: pos}, src, 1); got != (mgl64.Vec3{}) {
		t.Fatalf("non-eyed exact-origin impulse = %v, want zero", got)
	}
	if got, want := ExplosionImpulse(&directionTestEyedEntity{directionTestEntity: directionTestEntity{pos: pos}}, src, 1), (mgl64.Vec3{0, 1, 0}); got != want {
		t.Fatalf("eyed exact-origin impulse = %v, want %v", got, want)
	}
}

func TestExplosionImpulseAddsToEntityTypeVelocity(t *testing.T) {
	for _, test := range []struct {
		name string
		new  func(world.EntitySpawnOpts) *world.EntityHandle
	}{
		{"TNT", func(opts world.EntitySpawnOpts) *world.EntityHandle { return opts.New(TNTType, tntConf) }},
		{"projectile", func(opts world.EntitySpawnOpts) *world.EntityHandle { return opts.New(ArrowType, arrowConf) }},
		{"minecart", func(opts world.EntitySpawnOpts) *world.EntityHandle { return opts.New(MinecartType, minecartConf) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()
			<-w.Exec(func(tx *world.Tx) {
				handle := test.new(world.EntitySpawnOpts{Position: mgl64.Vec3{1, 0, 0}, Velocity: mgl64.Vec3{0, 1, 0}})
				e := tx.AddEntity(handle)
				e.(interface {
					Explode(world.ExplosionSource, float64)
				}).Explode(directionTestExplosionSource{}, 1)
				if got, want := e.(interface{ Velocity() mgl64.Vec3 }).Velocity(), (mgl64.Vec3{1, 1, 0}); got != want {
					t.Fatalf("velocity = %v, want %v", got, want)
				}
			})
		})
	}
}

type directionTestExplosionSource struct{ pos mgl64.Vec3 }

func (s directionTestExplosionSource) Position() mgl64.Vec3 { return s.pos }
func (directionTestExplosionSource) Size() float64          { return 1 }

type directionTestEntity struct{ pos mgl64.Vec3 }

func (*directionTestEntity) Close() error            { return nil }
func (*directionTestEntity) H() *world.EntityHandle  { return nil }
func (e *directionTestEntity) Position() mgl64.Vec3  { return e.pos }
func (*directionTestEntity) Rotation() cube.Rotation { return cube.Rotation{} }

type directionTestEyedEntity struct{ directionTestEntity }

func (*directionTestEyedEntity) EyeHeight() float64 { return 1.62 }
