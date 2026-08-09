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
		tx.SetBlock(cube.PosFromVec3(pos), block.Slime{}, nil)
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos, Velocity: mgl64.Vec3{0, -1}}.New(Type, Config{Position: pos})).(*Player)
		p.fall(10)
		if got := p.Velocity()[1]; got != 1 {
			t.Fatalf("vertical velocity after landing = %v, want 1", got)
		}
	})
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
