package entity

import (
	"hash/fnv"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// FrogType is the Java frog entity type, used as the real destination of tadpole metamorphosis.
var FrogType frogType

type frogType struct{}

func (frogType) EncodeEntity() string { return "minecraft:frog" }
func (frogType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil { frogConfig{}.Apply(data) }
	if h != nil {
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(h.UUID().String()))
		data.Data.(*frogBehaviour).random = java26.NewLegacyRandom(int64(hash.Sum64()))
	}
	return &Frog{Ent: &Ent{tx: tx, handle: h, data: data}}
}
func (frogType) BBox(world.Entity) cube.BBox { return cube.Box(-.25, 0, -.25, .25, .5, .25) }
func (frogType) DecodeNBT(nbt map[string]any, data *world.EntityData) {
	c := frogConfig{}
	if _, ok := nbt["Health"]; ok { c.health, c.healthSet = float64(nbtconv.Float32(nbt, "Health")), true }
	if _, ok := nbt["Variant"]; ok { c.variant = int(nbtconv.Int32(nbt, "Variant")) }
	c.Apply(data)
}
func (frogType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*frogBehaviour)
	return map[string]any{"Health": float32(b.health.Health()), "Variant": int32(b.variant)}
}

type frogConfig struct { health float64; variant int; healthSet bool }
func (c frogConfig) Apply(data *world.EntityData) {
	h := c.health
	if !c.healthSet { h = 10 }
	data.Data = &frogBehaviour{health: NewHealthManager(h, 10), effects: NewEffectManager(), variant: c.variant, movement: &MovementComputer{Gravity: .08, Drag: .1, DragBeforeGravity: true}, random: java26.NewLegacyRandom(int64(c.variant)), speed: 1, nextWander: 1}
}
type frogBehaviour struct { health *HealthManager; effects *EffectManager; movement *MovementComputer; random java26.Random; variant, nextWander int; speed float64; dead bool }

// Frog is a living amphibian runtime entity. Tadpoles convert into this runtime at Java's age threshold.
type Frog struct { *Ent }
func (f *Frog) behaviour() *frogBehaviour { return f.Behaviour().(*frogBehaviour) }
func (f *Frog) Tick(tx *world.Tx, current int64) { f.behaviour().effects.Tick(f, tx); f.Ent.Tick(tx, current) }
func (b *frogBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.dead { return nil }
	if b.nextWander > 0 { b.nextWander-- }
	vel := e.data.Vel
	if b.nextWander == 0 {
		b.nextWander = 40 + int(b.random.NextInt(41))
		vel = vel.Add(mgl64.Vec3{(float64(b.random.NextFloat())*2-1)*b.speed*.1, 0, (float64(b.random.NextFloat())*2-1)*b.speed*.1})
	}
	return b.movement.TickMovement(e, e.Position(), vel, e.data.Rot, e.data.Rot, tx)
}
func (f *Frog) Health() float64 { return f.behaviour().health.Health() }
func (f *Frog) MaxHealth() float64 { return f.behaviour().health.MaxHealth() }
func (f *Frog) SetMaxHealth(v float64) { f.behaviour().health.SetMaxHealth(v) }
func (f *Frog) Dead() bool { return f.behaviour().dead || f.Health() <= 0 }
func (f *Frog) Hurt(d float64, _ world.DamageSource) (float64, bool) {
	if f.Dead() || d <= 0 { return 0, false }
	b := f.behaviour(); before := b.health.Health(); b.health.AddHealth(-d); dealt := before-b.health.Health()
	f.viewAction(HurtAction{})
	if f.Dead() { b.dead = true; f.viewAction(DeathAction{}); _ = f.CloseIn(f.tx) }
	return dealt, true
}
func (f *Frog) Heal(h float64, _ world.HealingSource) float64 { if f.Dead() || h <= 0 { return 0 }; before := f.Health(); f.behaviour().health.AddHealth(h); return f.Health()-before }
func (f *Frog) KnockBack(src mgl64.Vec3, force, height float64) { d:=f.Position().Sub(src); if d.Len()!=0 { d=d.Normalize().Mul(force) }; d[1]=height; f.SetVelocity(d) }
func (f *Frog) AddEffect(e effect.Effect) { f.behaviour().effects.Add(e, f) }
func (f *Frog) RemoveEffect(t effect.Type) { f.behaviour().effects.Remove(t, f) }
func (f *Frog) Effects() []effect.Effect { return f.behaviour().effects.Effects() }
func (f *Frog) Speed() float64 { return f.behaviour().speed }
func (f *Frog) SetSpeed(v float64) { f.behaviour().speed=v }
func (f *Frog) viewAction(action world.EntityAction) { viewers:=f.tx.Viewers(f.Position()); for _,v:=range viewers { v.ViewEntityAction(f, action) }; f.tx.ReleaseViewers(viewers) }

func NewFrog(opts world.EntitySpawnOpts) *world.EntityHandle { return opts.New(FrogType, frogConfig{}) }

var _ world.EntityType = frogType{}
var _ Living = (*Frog)(nil)
