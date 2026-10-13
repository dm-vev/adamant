package entity

import (
	"hash/fnv"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// WitchType is the Java witch entity type.
var WitchType witchType

type witchType struct{}

func (witchType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		WitchConfig{}.Apply(d)
	}
	b := d.Data.(*WitchBehaviour)
	id := h.UUID()
	hash := fnv.New64a()
	_, _ = hash.Write(id[:])
	b.random = java26.NewLegacyRandom(int64(hash.Sum64()))
	return &Witch{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (witchType) EncodeEntity() string        { return "minecraft:witch" }
func (witchType) BBox(world.Entity) cube.BBox { return cube.Box(-.3, 0, -.3, .3, 1.95, .3) }
func (witchType) DecodeNBT(m map[string]any, d *world.EntityData) {
	c := WitchConfig{}
	source := ReadNativeMobJavaState(m)
	if source == nil {
		source = m
	}
	if _, ok := source["Health"]; ok {
		c.Health, c.healthSet = float64(nbtconv.Float32(source, "Health")), true
	} else if health, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.healthSet = health.Current, true
	}
	c.drinking = nbtconv.Bool(source, "DrinkingPotion")
	c.PotionUseTime = int(nbtconv.Int32(source, "PotionUseTime"))
	c.DrinkPotion = potion.From(nbtconv.Int32(source, "DrinkPotionId"))
	c.RaidID, c.RaidWave, c.RaidLeader = nbtconv.Int64(source, "RaidID"), int(nbtconv.Int32(source, "RaidWave")), nbtconv.Bool(source, "RaidLeader")
	c.Apply(d)
}
func (witchType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*WitchBehaviour)
	attributes := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.health.MaxHealth(), Current: b.health.Health(), DefaultMin: 0, DefaultMax: b.health.MaxHealth(), Min: 0, Max: b.health.MaxHealth()}})
	native := CloneNativeMobNBT(d.NativeNBT)
	if native == nil {
		native = make(map[string]any)
	}
	native["Attributes"] = attributes
	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), map[string]any{
		"Health": float32(b.health.Health()), "MaxHealth": float32(b.health.MaxHealth()),
		"DrinkingPotion": byte(boolByte(b.drinking)), "PotionUseTime": int32(b.potionUseTime), "DrinkPotionId": int32(b.drinkPotion.Uint8()),
		"RaidID": b.raidID, "RaidWave": int32(b.raidWave), "RaidLeader": b.raidLeader,
	})
	return MergeNativeMobJavaState(native, javaState)
}

type WitchConfig struct {
	Health              float64
	PotionUseTime       int
	DrinkPotion         potion.Potion
	RaidID              int64
	RaidWave            int
	RaidLeader          bool
	drinking, healthSet bool
}

func (c WitchConfig) Apply(d *world.EntityData) {
	health := c.Health
	if !c.healthSet && health <= 0 {
		health = 26
	}
	d.Data = &WitchBehaviour{
		health: NewHealthManager(health, 26), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true},
		speed: .25, drinking: c.drinking, potionUseTime: c.PotionUseTime, drinkPotion: c.DrinkPotion, random: java26.NewLegacyRandom(0),
		raidID: c.RaidID, raidWave: c.RaidWave, raidLeader: c.RaidLeader,
	}
}

type WitchBehaviour struct {
	health                                    *HealthManager
	effects                                   *EffectManager
	mc                                        *MovementComputer
	random                                    java26.Random
	speed                                     float64
	navigator                                 Navigator
	nextWander, attackCooldown, potionUseTime int
	drinking, dead                            bool
	drinkPotion                               potion.Potion
	raidID                                    int64
	raidWave                                  int
	raidLeader                                bool
}
type Witch struct{ *Ent }

func NewWitch(o world.EntitySpawnOpts) *world.EntityHandle { return o.New(WitchType, WitchConfig{}) }
func (*Witch) SpeciesType() world.EntityType               { return WitchType }
func (w *Witch) behaviour() *WitchBehaviour                { return w.Behaviour().(*WitchBehaviour) }
func (w *Witch) Tick(tx *world.Tx, n int64)                { w.behaviour().effects.Tick(w, tx); w.Ent.Tick(tx, n) }
func (w *Witch) RaidID() int64                             { return w.behaviour().raidID }
func (w *Witch) RaidWave() int                             { return w.behaviour().raidWave }
func (w *Witch) IsRaidLeader() bool                        { return w.behaviour().raidLeader }
func (w *Witch) CloseIn(tx *world.Tx) error                { tx.RaiderDied(w); return w.Ent.CloseIn(tx) }
func (b *WitchBehaviour) joinRaid(e *Ent, tx *world.Tx) {
	raid := tx.RaidForRaider(e)
	if raid != nil {
		if !raid.Active() {
			tx.RaiderDied(e)
			b.raidID, b.raidWave, b.raidLeader = 0, 0, false
			return
		}
		b.raidID = raid.ID()
		return
	}
	if b.raidID == 0 {
		return
	}
	if tx.RegisterRaider(b.raidID, e) {
		return
	}
	b.raidID, b.raidWave, b.raidLeader = 0, 0, false
}
func (b *WitchBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	b.joinRaid(e, tx)
	if b.attackCooldown > 0 {
		b.attackCooldown--
	}
	var target world.Entity
	best := math.MaxFloat64
	for player := range tx.Players() {
		distance := player.Position().Sub(e.Position()).LenSqr()
		if distance < best && distance < 64*64 {
			target, best = player, distance
		}
	}
	if b.drinking {
		if b.potionUseTime <= 0 {
			b.drinking = false
			for _, eff := range b.drinkPotion.Effects() {
				(&Witch{Ent: e}).AddEffect(eff)
			}
		} else {
			b.potionUseTime--
		}
	} else {
		b.tryDrinkPotion(e, tx, target, best)
	}
	if target == nil {
		b.wander(e, tx)
		return nil
	}
	delta := target.Position().Sub(e.Position())
	distance := math.Hypot(delta[0], delta[2])
	if !b.drinking && b.attackCooldown == 0 && best < 100 {
		if living, ok := target.(Living); ok {
			p := potion.Harming()
			switch {
			case distance >= 8 && !witchHasEffect(living.Effects(), effect.Slowness):
				p = potion.Slowness()
			case living.Health() >= 8 && !witchHasEffect(living.Effects(), effect.Poison):
				p = potion.Poison()
			case distance <= 3 && !witchHasEffect(living.Effects(), effect.Weakness) && b.random.NextFloat() < .25:
				p = potion.Weakness()
			}
			aim := delta.Add(mgl64.Vec3{0, 1 + distance*.2, 0})
			if aim.Len() > 0 {
				speed := .75
				if distance <= 2 {
					speed = .45
				}
				opts := world.EntitySpawnOpts{Position: e.Position().Add(mgl64.Vec3{0, 1.4, 0}), Velocity: aim.Normalize().Mul(speed)}
				tx.AddEntity(NewSplashPotion(opts, p, e))
			}
		}
		b.attackCooldown = 60
	}
	if b.drinking || distance <= 8 {
		b.stop(e, tx)
	} else {
		b.moveToward(e, tx, target.Position())
	}
	return nil
}

func (b *WitchBehaviour) tryDrinkPotion(e *Ent, tx *world.Tx, target world.Entity, targetDistance float64) {
	var drink potion.Potion
	pos := e.Position().Add(mgl64.Vec3{0, 1.6, 0})
	_, inWater := tx.Liquid(cube.PosFromVec3(pos))
	switch {
	case b.random.NextFloat() < .15 && inWater && !witchHasEffect(b.effects.Effects(), effect.WaterBreathing):
		drink = potion.WaterBreathing()
	case b.random.NextFloat() < .15 && e.OnFireDuration() > 0 && !witchHasEffect(b.effects.Effects(), effect.FireResistance):
		drink = potion.FireResistance()
	case b.random.NextFloat() < .05 && b.health.Health() < b.health.MaxHealth():
		drink = potion.Healing()
	case b.random.NextFloat() < .5 && target != nil && !witchHasEffect(b.effects.Effects(), effect.Speed) && targetDistance > 121:
		drink = potion.Swiftness()
	}
	if drink.Uint8() != 0 {
		b.drinking, b.potionUseTime, b.drinkPotion = true, 32, drink
	}
}

func witchHasEffect(effects []effect.Effect, t effect.Type) bool {
	for _, active := range effects {
		if active.Type() == t {
			return true
		}
	}
	return false
}
func (b *WitchBehaviour) moveToward(e *Ent, tx *world.Tx, p mgl64.Vec3) {
	d := p.Sub(e.data.Pos)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize()
	}
	v := e.data.Vel
	v[0], v[2] = d[0]*b.speed, d[2]*b.speed
	r := e.data.Rot
	r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	m := b.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, r, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
}
func (b *WitchBehaviour) stop(e *Ent, tx *world.Tx) {
	v := e.data.Vel
	v[0], v[2] = 0, 0
	m := b.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
}
func (b *WitchBehaviour) wander(e *Ent, tx *world.Tx) {
	if b.nextWander > 0 {
		b.nextWander--
	}
	if b.navigator.Index >= len(b.navigator.Path) && b.nextWander == 0 {
		b.nextWander = 40
		start := cube.PosFromVec3(e.Position())
		b.navigator.MoveTo(tx, start, start.Add(cube.Pos{3, 0, -2}), GroundProfile{MaxVisitedNodes: 64}, navigatorTick(e))
	}
	if p, ok := b.navigator.Tick(e.Position(), navigatorTick(e), .6); ok {
		if p.Sub(e.Position()).Len() < .4 {
			b.navigator.Index++
		} else {
			b.moveToward(e, tx, p)
		}
	} else {
		b.stop(e, tx)
	}
}
func (w *Witch) Health() float64        { return w.behaviour().health.Health() }
func (w *Witch) MaxHealth() float64     { return w.behaviour().health.MaxHealth() }
func (w *Witch) SetMaxHealth(v float64) { w.behaviour().health.SetMaxHealth(v) }
func (w *Witch) Dead() bool             { return w.behaviour().dead || w.Health() <= 0 }
func (w *Witch) Hurt(d float64, s world.DamageSource) (float64, bool) {
	if w.Dead() || d <= 0 {
		return 0, false
	}
	b := w.behaviour()
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	b.health.AddHealth(-d)
	onMobHurt(w.tx, w)
	if w.Dead() {
		b.dead = true
		w.viewAction(DeathAction{})
		w.tx.RaiderKilled(w, raidDamageAttacker(s))
		dropAllDeathLoot(w.tx, w, s)
		_ = w.CloseIn(w.tx)
	} else {
		w.viewAction(HurtAction{})
	}
	return d, true
}
func (w *Witch) Heal(h float64, _ world.HealingSource) float64 {
	if h <= 0 || w.Dead() {
		return 0
	}
	old := w.Health()
	w.behaviour().health.AddHealth(h)
	return w.Health() - old
}
func (w *Witch) KnockBack(src mgl64.Vec3, f, h float64) {
	d := w.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	w.SetVelocity(d)
}
func (w *Witch) AddEffect(e effect.Effect) {
	if e.Type() == effect.Poison || e.Type() == effect.FatalPoison {
		return
	}
	w.behaviour().effects.Add(e, w)
}
func (w *Witch) RemoveEffect(t effect.Type) { w.behaviour().effects.Remove(t, w) }
func (w *Witch) Effects() []effect.Effect   { return w.behaviour().effects.Effects() }
func (w *Witch) Speed() float64             { return w.behaviour().speed }
func (w *Witch) SetSpeed(v float64)         { w.behaviour().speed = v }
func (w *Witch) viewAction(a world.EntityAction) {
	v := w.tx.Viewers(w.Position())
	for _, x := range v {
		x.ViewEntityAction(w, a)
	}
	w.tx.ReleaseViewers(v)
}

var _ world.EntityType = witchType{}
var _ Living = (*Witch)(nil)
