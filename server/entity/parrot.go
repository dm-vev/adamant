package entity

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// ParrotType is the source minecraft:parrot actor.
var ParrotType parrotType

type parrotType struct{}

func (parrotType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		ParrotConfig{}.Apply(d)
	}
	return &Parrot{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (parrotType) EncodeEntity() string        { return "minecraft:parrot" }
func (parrotType) BBox(world.Entity) cube.BBox { return cube.Box(-.25, 0, -.25, .25, .9, .25) }
func (parrotType) DecodeNBT(m map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	c := ParrotConfig{Variant: int(nbtconv.Int32(m, "Variant")), Tame: nbtconv.Bool(s, "Tame"), Sitting: nbtconv.Bool(s, "Sitting"), Owner: catReadOwner(s["Owner"])}
	if _, ok := s["Health"]; ok {
		c.Health, c.healthSet = float64(nbtconv.Float32(s, "Health")), true
	} else if a, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.healthSet = a.Current, true
	}
	if a, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.MaxHealth = a.Base
	}
	c.Apply(d)
}
func (parrotType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*parrotBehaviour)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: h.MaxHealth(), Current: h.Health(), DefaultMin: 0, DefaultMax: 6, Min: 0, Max: 6}, {Name: "minecraft:movement", Base: .2, Current: .2, DefaultMin: 0, DefaultMax: .2, Min: 0, Max: .2}, {Name: "minecraft:flying_speed", Base: .4, Current: .4, DefaultMin: 0, DefaultMax: .4, Min: 0, Max: .4}, {Name: "minecraft:attack_damage", Base: 3, Current: 3, DefaultMin: 0, DefaultMax: 3, Min: 0, Max: 3}})
	m := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attrs, "Variant": int32(b.variant)})
	extra := MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), map[string]any{"Health": float32(h.Health()), "MaxHealth": float32(h.MaxHealth()), "Tame": byte(catBool(b.tamed)), "Sitting": byte(catBool(b.orderedToSit))})
	if owner := catOwnerNBT(b.ownerUUID); owner != nil {
		extra["Owner"] = owner
	} else {
		delete(extra, "Owner")
	}
	return MergeNativeMobJavaState(m, extra)
}

// ParrotConfig is the configuration of a parrot: its health, variant and
// whether it is tamed, by whom and ordered to sit.
type ParrotConfig struct {
	Health, MaxHealth float64
	Variant           int
	Tame, Sitting     bool
	Owner             string
	healthSet         bool
}

func (c ParrotConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c ParrotConfig) New() *parrotBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 6
	}
	if !c.healthSet && c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	return &parrotBehaviour{health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager(), variant: ((c.Variant % 5) + 5) % 5,
		tamable: tamable{tamed: c.Tame || c.Owner != "", ownerUUID: c.Owner, orderedToSit: c.Sitting, inSittingPose: c.Sitting}, speed: .2}
}

type parrotBehaviour struct {
	health  *HealthManager
	effects *EffectManager
	animalAI
	tamable
	variant     int
	dead, party bool
	speed       float64
	// rideCooldown is ShoulderRidingEntity.rideCooldownCounter, the ticks
	// since the parrot came into the world.
	rideCooldown int
	jukebox      *cube.Pos
	shown        parrotShownState
}

// parrotShownState are the flags of a parrot viewers were last shown.
type parrotShownState struct{ tamed, sitting, party bool }

type Parrot struct{ *Ent }

func NewParrot(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(ParrotType, ParrotConfig{})
}
func (p *Parrot) FinalizeSpawn(_ string, r *vanilla.Random) {
	if r != nil {
		p.b().variant = int(r.NextInt(5))
	}
}
func (*Parrot) SpeciesType() world.EntityType { return ParrotType }
func (p *Parrot) b() *parrotBehaviour         { return p.Behaviour().(*parrotBehaviour) }
func (p *Parrot) Tick(tx *world.Tx, n int64) {
	p.b().effects.Tick(p, tx)
	p.Ent.Tick(tx, n)
}
func (b *parrotBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	p := &Parrot{Ent: e}
	// ShoulderRidingEntity.tick counts the ticks before anything else.
	b.rideCooldown++
	ai := &b.ai
	ai.MovementSpeed, ai.FlyingSpeed, ai.EyeHeight = b.speed, .4, .54
	if !p.Dead() {
		b.beforeAiStep(ai.Mob(e, p))
	}
	mv := ai.Tick(e, p, b.registerGoals)
	if shown := (parrotShownState{b.tamed, b.inSittingPose, b.party}); shown != b.shown {
		b.shown = shown
		p.updateState()
	}
	return mv
}
func (p *Parrot) Variant() int      { return p.b().variant }
func (p *Parrot) Tamed() bool       { return p.b().tamed }
func (p *Parrot) Sitting() bool     { return p.b().inSittingPose }
func (p *Parrot) OwnerUUID() string { return p.b().ownerUUID }
func (p *Parrot) Party() bool       { return p.b().party }

// SetJukeboxPlaying ports Parrot.setRecordPlayingNearby: a parrot dances
// while a jukebox nearby plays.
func (p *Parrot) SetJukeboxPlaying(pos cube.Pos, playing bool) {
	b := p.b()
	b.jukebox, b.party = &pos, playing
}

// Interact ports Parrot.mobInteract: seeds tame a wild parrot one time in
// ten, a cookie poisons and kills it, and its owner makes a landed parrot sit
// or stand.
func (p *Parrot) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil {
		return false
	}
	held, _ := u.HeldItems()
	name := ""
	if !held.Empty() {
		name, _ = held.Item().EncodeItem()
	}
	b := p.b()
	r := b.ai.Mob(p.Ent, p).Random()
	switch {
	case !b.tamed && parrotFood(name):
		if ctx != nil {
			ctx.SubtractFromCount(1)
		}
		tx.PlaySound(p.Position(), sound.Custom{Name: "entity.parrot.eat", Volume: 1, Pitch: float64(1 + (r.NextFloat()-r.NextFloat())*.2)})
		if r.NextInt(10) == 0 {
			b.tamed, b.owner, b.ownerUUID = true, u.H(), u.H().UUID().String()
			p.viewAction(TameAction{Succeeded: true})
		} else {
			p.viewAction(TameAction{})
		}
		return true
	case name == "minecraft:cookie":
		if ctx != nil {
			ctx.SubtractFromCount(1)
		}
		p.AddEffect(effect.New(effect.Poison, 1, 900*time.Second/20))
		if user, ok := u.(world.Entity); ok {
			p.Hurt(math.MaxFloat32, AttackDamageSource{Attacker: user})
		}
		return true
	case b.ai.onGround && b.ownedBy(u):
		b.orderedToSit = !b.orderedToSit
		return true
	}
	return false
}

// parrotFood reports if an item is in #parrot_food.
func parrotFood(n string) bool {
	switch n {
	case "minecraft:wheat_seeds", "minecraft:melon_seeds", "minecraft:pumpkin_seeds", "minecraft:beetroot_seeds", "minecraft:torchflower_seeds", "minecraft:pitcher_pod":
		return true
	}
	return false
}
func (p *Parrot) Health() float64        { return p.b().health.Health() }
func (p *Parrot) MaxHealth() float64     { return p.b().health.MaxHealth() }
func (p *Parrot) SetMaxHealth(h float64) { p.b().health.SetMaxHealth(h) }
func (p *Parrot) Dead() bool             { return p.b().dead || p.Health() <= 0 }
func (p *Parrot) Heal(h float64, _ world.HealingSource) float64 {
	if p.Dead() || h <= 0 {
		return 0
	}
	old := p.Health()
	p.b().health.AddHealth(h)
	return p.Health() - old
}

// Hurt ports Parrot.hurtServer: a hurt parrot is no longer ordered to sit.
func (p *Parrot) Hurt(d float64, s world.DamageSource) (float64, bool) {
	b := p.b()
	if p.Dead() || d <= 0 {
		return 0, false
	}
	b.orderedToSit = false
	d, ok := b.ai.DamageCooldown(p.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	b.health.AddHealth(-d)
	b.ai.Hurt(s)
	p.viewAction(HurtAction{})
	onMobHurt(p.tx, p)
	if p.Dead() {
		b.dead = true
		p.viewAction(DeathAction{})
		dropAllDeathLoot(p.tx, p, s)
		_ = p.CloseIn(p.tx)
	}
	return d, true
}
func (p *Parrot) KnockBack(src mgl64.Vec3, f, h float64) {
	knockBack(p, src, f, h)
}
func (p *Parrot) AddEffect(e effect.Effect)  { p.b().effects.Add(e, p) }
func (p *Parrot) RemoveEffect(t effect.Type) { p.b().effects.Remove(t, p) }
func (p *Parrot) Effects() []effect.Effect   { return p.b().effects.Effects() }
func (p *Parrot) Speed() float64             { return p.b().speed }
func (p *Parrot) SetSpeed(v float64)         { p.b().speed = v }
func (p *Parrot) viewAction(a world.EntityAction) {
	vs := p.tx.Viewers(p.Position())
	for _, v := range vs {
		v.ViewEntityAction(p, a)
	}
	p.tx.ReleaseViewers(vs)
}

var _ Living = (*Parrot)(nil)
var _ Interactable = (*Parrot)(nil)
