package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestEnderPearlFallDamageSetting(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })

	mustDo(t, w, func(tx *world.Tx) {
		target := enderPearlResult{pos: mgl64.Vec3{1, 2, 3}}
		for _, enabled := range []bool{false, true} {
			w.SetFallDamage(enabled)
			owner := tx.AddEntity(world.EntitySpawnOpts{}.New(enderPearlOwnerType{}, enderPearlOwnerConfig{})).(*enderPearlOwner)
			pearl := tx.AddEntity(NewEnderPearl(world.EntitySpawnOpts{}, owner)).(*Ent)
			teleport(pearl, tx, target)

			want := 0.0
			if enabled {
				want = 5
			}
			if owner.state.damage != want {
				t.Fatalf("FallDamage(%v) pearl damage = %v, want %v", enabled, owner.state.damage, want)
			}
			if owner.Position() != target.pos {
				t.Fatalf("FallDamage(%v) owner position = %v, want %v", enabled, owner.Position(), target.pos)
			}
			if enabled {
				if _, ok := owner.state.source.(FallDamageSource); !ok {
					t.Fatalf("pearl damage source = %T, want entity.FallDamageSource", owner.state.source)
				}
			}
		}
	})
}

type enderPearlResult struct{ pos mgl64.Vec3 }

func (r enderPearlResult) BBox() cube.BBox      { return cube.BBox{} }
func (r enderPearlResult) Position() mgl64.Vec3 { return r.pos }
func (r enderPearlResult) Face() cube.Face      { return cube.FaceUp }

type enderPearlOwnerConfig struct{}

func (enderPearlOwnerConfig) Apply(data *world.EntityData) { data.Data = new(enderPearlOwnerState) }

type enderPearlOwnerType struct{}

func (enderPearlOwnerType) Open(_ *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	return &enderPearlOwner{h: h, data: data, state: data.Data.(*enderPearlOwnerState)}
}
func (enderPearlOwnerType) EncodeEntity() string                        { return "test:ender_pearl_owner" }
func (enderPearlOwnerType) BBox(world.Entity) cube.BBox                 { return cube.BBox{} }
func (enderPearlOwnerType) DecodeNBT(map[string]any, *world.EntityData) {}
func (enderPearlOwnerType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type enderPearlOwner struct {
	h     *world.EntityHandle
	data  *world.EntityData
	state *enderPearlOwnerState
}

type enderPearlOwnerState struct {
	damage float64
	source world.DamageSource
}

func (e *enderPearlOwner) H() *world.EntityHandle                    { return e.h }
func (e *enderPearlOwner) Position() mgl64.Vec3                      { return e.data.Pos }
func (e *enderPearlOwner) Rotation() cube.Rotation                   { return e.data.Rot }
func (e *enderPearlOwner) Close() error                              { return nil }
func (e *enderPearlOwner) Teleport(pos mgl64.Vec3)                   { e.data.Pos = pos }
func (e *enderPearlOwner) Health() float64                           { return 20 - e.state.damage }
func (e *enderPearlOwner) MaxHealth() float64                        { return 20 }
func (e *enderPearlOwner) SetMaxHealth(float64)                      {}
func (e *enderPearlOwner) Dead() bool                                { return false }
func (e *enderPearlOwner) Heal(float64, world.HealingSource) float64 { return 0 }
func (e *enderPearlOwner) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	e.state.damage += damage
	e.state.source = src
	return damage, true
}
func (e *enderPearlOwner) KnockBack(mgl64.Vec3, float64, float64) {}
func (e *enderPearlOwner) Velocity() mgl64.Vec3                   { return e.data.Vel }
func (e *enderPearlOwner) SetVelocity(v mgl64.Vec3)               { e.data.Vel = v }
func (e *enderPearlOwner) AddEffect(effect.Effect)                {}
func (e *enderPearlOwner) RemoveEffect(effect.Type)               {}
func (e *enderPearlOwner) Effects() []effect.Effect               { return nil }
func (e *enderPearlOwner) Speed() float64                         { return 0 }
func (e *enderPearlOwner) SetSpeed(float64)                       {}
