package entity

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
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
		parrotConfig{}.Apply(d)
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
	c := parrotConfig{Variant: parrotInt(m["Variant"]), Tame: parrotBoolValue(s["Tame"]), Sitting: parrotBoolValue(s["Sitting"]), Owner: parrotOwner(s["Owner"]), Cooldown: parrotInt(m["ShoulderRideCooldown"])}
	if value, ok := parrotNumber(s["Health"]); ok {
		c.Health = value
		c.healthSet = true
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
	extra := map[string]any{"Health": float32(h.Health()), "MaxHealth": float32(h.MaxHealth()), "Tame": byte(parrotBool(b.tame)), "Sitting": byte(parrotBool(b.sitting)), "Owner": b.owner}
	return MergeNativeMobJavaState(m, extra)
}

type parrotConfig struct {
	Health, MaxHealth float64
	Variant, Cooldown int
	Tame, Sitting     bool
	Owner             string
	healthSet         bool
}

func (c parrotConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c parrotConfig) New() *parrotBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 6
	}
	if !c.healthSet && c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	return &parrotBehaviour{health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .04, Drag: .02, DragBeforeGravity: true}, random: java26.NewLegacyRandom(0), variant: ((c.Variant % 5) + 5) % 5, tame: c.Tame, sitting: c.Sitting, owner: c.Owner, speed: .2, nextMove: 1, cooldown: c.Cooldown}
}

type parrotBehaviour struct {
	health                     *HealthManager
	effects                    *EffectManager
	mc                         *MovementComputer
	random                     java26.Random
	variant                    int
	tame, sitting, dead, party bool
	owner                      string
	speed                      float64
	nextMove, ticks            int
	target                     mgl64.Vec3
	hasTarget                  bool
	cooldown                   int
	jukebox                    cube.Pos
	hasJukebox                 bool
}
type Parrot struct{ *Ent }

func NewParrot(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(ParrotType, parrotConfig{})
}
func (p *Parrot) FinalizeSpawn(_ string, r *java26.Random) {
	if r != nil {
		p.b().variant = int(r.NextInt(5))
	}
}
func (*Parrot) SpeciesType() world.EntityType { return ParrotType }
func (p *Parrot) b() *parrotBehaviour         { return p.Behaviour().(*parrotBehaviour) }
func (p *Parrot) Tick(tx *world.Tx, n int64) {
	b := p.b()
	b.effects.Tick(p, tx)
	b.ticks++
	if b.hasJukebox {
		blockName, _ := tx.Block(b.jukebox).EncodeBlock()
		if p.Position().Sub(b.jukebox.Vec3()).Len() > 3.46 || blockName != "minecraft:jukebox" {
			b.hasJukebox = false
			b.party = false
		}
	}
	if b.ticks%400 == 0 && b.random.NextInt(2) == 0 {
		p.imitateNearby(tx)
	}
	p.Ent.Tick(tx, n)
}
func (b *parrotBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.cooldown > 0 {
		b.cooldown--
	}
	if b.sitting {
		b.hasTarget = false
		return b.mc.TickMovement(e, e.data.Pos, mgl64.Vec3{}, e.data.Rot, e.data.Rot, tx)
	}
	var owner world.Entity
	if b.tame && b.owner != "" {
		for p := range tx.Players() {
			if p.H().UUID().String() == b.owner {
				owner = p
				break
			}
		}
	}
	if owner != nil && owner.Position().Sub(e.Position()).Len() > 4 {
		b.target = owner.Position().Add(mgl64.Vec3{0, 1, 0})
		b.hasTarget = true
		b.nextMove = 0
	} else if !b.hasTarget || b.nextMove <= 0 {
		b.nextMove = 20 + int(b.random.NextInt(31))
		b.target = e.Position().Add(mgl64.Vec3{float64(b.random.NextInt(17) - 8), float64(b.random.NextInt(9) - 4), float64(b.random.NextInt(17) - 8)})
		b.hasTarget = true
	}
	delta := b.target.Sub(e.Position())
	if delta.Len() < 1 {
		b.hasTarget = false
	} else {
		delta = delta.Normalize().Mul(b.speed * .4)
		if delta[1] < -.025*(b.speed/.2) {
			delta[1] = -.025 * (b.speed / .2)
		}
		if delta[1] > .035*(b.speed/.2) {
			delta[1] = .035 * (b.speed / .2)
		}
	}
	b.nextMove--
	b.mc.Gravity = .04
	return b.mc.TickMovement(e, e.data.Pos, delta, e.data.Rot, e.data.Rot, tx)
}
func (p *Parrot) Variant() int      { return p.b().variant }
func (p *Parrot) Tamed() bool       { return p.b().tame }
func (p *Parrot) Sitting() bool     { return p.b().sitting }
func (p *Parrot) OwnerUUID() string { return p.b().owner }
func (p *Parrot) Party() bool       { return p.b().party }
func (p *Parrot) SetJukeboxPlaying(pos cube.Pos, playing bool) {
	b := p.b()
	previous := b.party
	if playing {
		b.jukebox = pos
		b.hasJukebox = true
		b.party = true
	} else if !b.hasJukebox || b.jukebox == pos {
		b.hasJukebox = false
		b.party = false
	}
	if previous != b.party {
		p.updateState()
	}
}
func (p *Parrot) imitateNearby(tx *world.Tx) {
	b := p.b()
	if b.dead {
		return
	}
	sounds := map[string]string{"minecraft:blaze": "entity.parrot.imitate.blaze", "minecraft:creeper": "entity.parrot.imitate.creeper", "minecraft:drowned": "entity.parrot.imitate.drowned", "minecraft:elder_guardian": "entity.parrot.imitate.elder_guardian", "minecraft:ender_dragon": "entity.parrot.imitate.ender_dragon", "minecraft:enderman": "entity.parrot.imitate.enderman", "minecraft:endermite": "entity.parrot.imitate.endermite", "minecraft:evoker": "entity.parrot.imitate.evoker", "minecraft:ghast": "entity.parrot.imitate.ghast", "minecraft:guardian": "entity.parrot.imitate.guardian", "minecraft:hoglin": "entity.parrot.imitate.hoglin", "minecraft:husk": "entity.parrot.imitate.husk", "minecraft:illusioner": "entity.parrot.imitate.illusioner", "minecraft:magma_cube": "entity.parrot.imitate.magma_cube", "minecraft:phantom": "entity.parrot.imitate.phantom", "minecraft:piglin": "entity.parrot.imitate.piglin", "minecraft:piglin_brute": "entity.parrot.imitate.piglin_brute", "minecraft:pillager": "entity.parrot.imitate.pillager", "minecraft:ravager": "entity.parrot.imitate.ravager", "minecraft:shulker": "entity.parrot.imitate.shulker", "minecraft:silverfish": "entity.parrot.imitate.silverfish", "minecraft:skeleton": "entity.parrot.imitate.skeleton", "minecraft:slime": "entity.parrot.imitate.slime", "minecraft:spider": "entity.parrot.imitate.spider", "minecraft:stray": "entity.parrot.imitate.stray", "minecraft:vex": "entity.parrot.imitate.vex", "minecraft:vindicator": "entity.parrot.imitate.vindicator", "minecraft:warden": "entity.parrot.imitate.warden", "minecraft:witch": "entity.parrot.imitate.witch", "minecraft:wither": "entity.parrot.imitate.wither", "minecraft:wither_skeleton": "entity.parrot.imitate.wither_skeleton", "minecraft:zoglin": "entity.parrot.imitate.zoglin", "minecraft:zombie": "entity.parrot.imitate.zombie", "minecraft:zombie_villager": "entity.parrot.imitate.zombie_villager"}
	var candidates []string
	for e := range tx.Entities() {
		if e == p || e.Position().Sub(p.Position()).Len() > 20 {
			continue
		}
		if name, ok := sounds[e.H().Type().EncodeEntity()]; ok {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) > 0 {
		tx.PlaySound(p.Position(), sound.Custom{Name: candidates[b.random.NextInt(int32(len(candidates)))], Volume: .7, Pitch: 1})
	}
}
func (p *Parrot) CanLandOnShoulder(owner world.Entity) bool {
	return p.b().tame && p.b().owner != "" && owner != nil && owner.H().UUID().String() == p.b().owner && !p.b().sitting && p.b().cooldown <= 0 && p.Position().Sub(owner.Position()).Len() < 1.5
}

// SetOnShoulder starts the source 100-tick dismount cooldown. The caller stores
// EncodeNBT() as the player's ShoulderEntity compound before removing the actor.
func (p *Parrot) SetOnShoulder(owner world.Entity) map[string]any {
	if !p.CanLandOnShoulder(owner) {
		return nil
	}
	p.b().cooldown = 100
	m := ParrotType.EncodeNBT(p.data)
	m["id"] = "minecraft:parrot"
	m["ShoulderRideCooldown"] = int32(100)
	return m
}
func (p *Parrot) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil {
		return false
	}
	held, _ := u.HeldItems()
	name, _ := held.Item().EncodeItem()
	b := p.b()
	if !b.tame && parrotSeed(name) {
		if ctx != nil {
			ctx.SubtractFromCount(1)
		}
		tx.PlaySound(p.Position(), sound.Custom{Name: "entity.parrot.eat", Volume: 1, Pitch: 1})
		if b.random.NextInt(10) == 0 {
			b.tame = true
			b.owner = u.H().UUID().String()
			b.sitting = false
			p.updateState()
		}
		return true
	}
	if parrotPoison(name) {
		if ctx != nil {
			ctx.SubtractFromCount(1)
		}
		p.AddEffect(effect.New(effect.Poison, 0, 900*time.Second/20))
		if user, ok := u.(world.Entity); ok {
			p.Hurt(math.MaxFloat64, AttackDamageSource{Attacker: user})
		}
		return true
	}
	if b.tame && b.owner == u.H().UUID().String() {
		b.sitting = !b.sitting
		b.hasTarget = false
		p.updateState()
		return true
	}
	return false
}
func parrotSeed(n string) bool {
	switch n {
	case "minecraft:wheat_seeds", "minecraft:melon_seeds", "minecraft:pumpkin_seeds", "minecraft:beetroot_seeds", "minecraft:torchflower_seeds", "minecraft:pitcher_pod":
		return true
	}
	return false
}
func parrotPoison(n string) bool         { return n == "minecraft:cookie" }
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
func (p *Parrot) Hurt(d float64, s world.DamageSource) (float64, bool) {
	b := p.b()
	if p.Dead() || d <= 0 {
		return 0, false
	}
	b.sitting = false
	b.health.AddHealth(-d)
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
	d := p.Position().Sub(src)
	if d.Len() == 0 {
		return
	}
	p.data.Vel = p.data.Vel.Add(d.Normalize().Mul(f)).Add(mgl64.Vec3{0, h, 0})
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
func parrotOwner(v any) string {
	switch x := v.(type) {
	case string:
		return x
	}
	return ""
}
func parrotInt(v any) int {
	if n, ok := v.(int32); ok {
		return int(n)
	}
	if n, ok := v.(int64); ok {
		return int(n)
	}
	if n, ok := v.(float64); ok {
		return int(n)
	}
	return 0
}
func parrotBoolValue(v any) bool {
	switch n := v.(type) {
	case byte:
		return n == 1
	case bool:
		return n
	case float64:
		return n == 1
	}
	return false
}
func parrotNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
func parrotBool(v bool) byte {
	if v {
		return 1
	}
	return 0
}

var _ Living = (*Parrot)(nil)
var _ Interactable = (*Parrot)(nil)
