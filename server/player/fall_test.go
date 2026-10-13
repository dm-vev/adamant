package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestFallDamageSetting(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })

	<-w.Exec(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.5, 1, 0.5}
		tx.SetBlock(cube.PosFromVec3(pos), block.Cobblestone{}, nil)
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos})).(*Player)
		h := new(fallDamageHandler)
		p.Handle(h)

		w.SetFallDamage(false)
		p.fall(10)
		if got := p.Health(); got != 20 || h.calls != 0 {
			t.Fatalf("disabled fall damage: health=%v hurt events=%d", got, h.calls)
		}

		w.SetFallDamage(true)
		p.fall(10)
		if got := p.Health(); got != 13 || h.calls != 1 {
			t.Fatalf("enabled fall damage: health=%v hurt events=%d", got, h.calls)
		}
		if _, ok := h.source.(entity.FallDamageSource); !ok {
			t.Fatalf("fall damage source = %T, want entity.FallDamageSource", h.source)
		}
	})
}

func TestFallDamageSettingPreservesEntityLand(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetFallDamage(false)

	<-w.Exec(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.5, 1, 0.5}
		tx.SetBlock(cube.PosFromVec3(pos).Sub(cube.Pos{0, 1}), block.Slime{}, nil)
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos, Velocity: mgl64.Vec3{0, -1}}.New(Type, Config{Position: pos})).(*Player)
		p.fall(10)
		if got := p.Velocity()[1]; got != 1 {
			t.Fatalf("vertical velocity after landing = %v, want 1", got)
		}
	})
}

func TestLandingOnStalagmiteTipDoublesFallDamage(t *testing.T) {
	cases := []struct {
		name       string
		stalagmite block.PointedDripstone
		feet       float64
		fallDamage bool
		health     float64
		source     world.DamageSource
	}{
		// ceil((5 + 2.5 - 3) * 2) = 9.
		{"tip impales", block.PointedDripstone{Thickness: block.SpeleothemTip}, 11.0 / 16, true, 11, block.StalagmiteDamageSource{}},
		{"frustum is ordinary ground", block.PointedDripstone{Thickness: block.SpeleothemFrustum}, 1, true, 18, entity.FallDamageSource{}},
		{"fall_damage disabled", block.PointedDripstone{Thickness: block.SpeleothemTip}, 11.0 / 16, false, 20, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			t.Cleanup(func() { _ = w.Close() })
			w.SetFallDamage(c.fallDamage)
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 0, 0}, c.stalagmite, nil)
				pos := mgl64.Vec3{0.5, c.feet, 0.5}
				p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos})).(*Player)
				h := new(fallDamageHandler)
				p.Handle(h)
				p.fall(5)
				if got := p.Health(); got != c.health || h.source != c.source {
					t.Fatalf("health=%v source=%T, want %v and %T", got, h.source, c.health, c.source)
				}
			})
		})
	}
}

type fallDamageHandler struct {
	NopHandler
	calls  int
	source world.DamageSource
}

func (h *fallDamageHandler) HandleHurt(_ *Context, _ *float64, _ bool, _ *time.Duration, src world.DamageSource) {
	h.calls++
	h.source = src
}
