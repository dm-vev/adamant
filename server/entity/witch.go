package entity

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/entity/effect"
	"github.com/dm-vev/adamant/server/internal/nbtconv"
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/potion"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// UsingItem reports if the witch drinks a potion.
func (w *witch) UsingItem() bool { return w.behaviour().drinking }

// HeldItems returns the potion the witch drinks, if any.
func (w *witch) HeldItems() (item.Stack, item.Stack) {
	b := w.behaviour()
	if !b.drinking {
		return item.Stack{}, item.Stack{}
	}
	return item.NewStack(item.Potion{Type: b.drinkPotion}, 1), item.Stack{}
}

// WitchType is the Java witch entity type.
var entityWitchType witchType

type witchType struct{}

func (witchType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		witchConfig{}.Apply(d)
	}
	b := d.Data.(*witchBehaviour)
	if !b.randomSeeded {
		b.random, b.randomSeeded = openRandom(tx, h), true
	}
	return &witch{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (witchType) EncodeEntity() string        { return "minecraft:witch" }
func (witchType) BBox(world.Entity) cube.BBox { return cube.Box(-.3, 0, -.3, .3, 1.95, .3) }
func (witchType) DecodeNBT(m map[string]any, d *world.EntityData) {
	c := witchConfig{}
	source := readNativeMobJavaState(m)
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
	b := d.Data.(*witchBehaviour)
	attributes := encodeNativeMobAttributes(readNativeMobAttributes(d.NativeNBT), []nativeMobAttr{{Name: "minecraft:health", Base: b.health.MaxHealth(), Current: b.health.Health(), DefaultMin: 0, DefaultMax: b.health.MaxHealth(), Min: 0, Max: b.health.MaxHealth()}})
	native := cloneNativeMobNBT(d.NativeNBT)
	if native == nil {
		native = make(map[string]any)
	}
	native["Attributes"] = attributes
	javaState := mergeNativeMobNBT(readNativeMobJavaState(d.NativeNBT), map[string]any{
		"Health": float32(b.health.Health()), "MaxHealth": float32(b.health.MaxHealth()),
		"DrinkingPotion": byte(boolByte(b.drinking)), "PotionUseTime": int32(b.potionUseTime), "DrinkPotionId": int32(b.drinkPotion.Uint8()),
		"RaidID": b.raidID, "RaidWave": int32(b.raidWave), "RaidLeader": b.raidLeader,
	})
	return mergeNativeMobJavaState(native, javaState)
}

type witchConfig struct {
	Health              float64
	PotionUseTime       int
	DrinkPotion         potion.Potion
	RaidID              int64
	RaidWave            int
	RaidLeader          bool
	drinking, healthSet bool
}

func (c witchConfig) Apply(d *world.EntityData) {
	health := c.Health
	if !c.healthSet && health <= 0 {
		health = 26
	}
	d.Data = &witchBehaviour{
		health: NewHealthManager(health, 26), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true},
		speed: .25, drinking: c.drinking, potionUseTime: c.PotionUseTime, drinkPotion: c.DrinkPotion, random: vanilla.NewLegacyRandom(0),
		raidID: c.RaidID, raidWave: c.RaidWave, raidLeader: c.RaidLeader,
	}
}

type witchBehaviour struct {
	health  *HealthManager
	effects *EffectManager
	mc      *MovementComputer
	random  vanilla.Random
	// randomSeeded is set once random has been seeded.
	randomSeeded  bool
	speed         float64
	potionUseTime int
	drinking      bool
	dead          bool
	drinkPotion   potion.Potion
	raidID        int64
	raidWave      int
	raidLeader    bool

	raiderState
	ai          MobAI
	heal        *nearestHealableRaiderTargetGoal
	attack      *nearestAttackableTargetGoal
	attackAllow bool
	// lastFireDamage is one more than the world tick the witch last took
	// fire damage, or zero if it never did.
	lastFireDamage int64
}
type witch struct{ *Ent }

func NewWitch(o world.EntitySpawnOpts) *world.EntityHandle { return o.New(entityWitchType, witchConfig{}) }
func (*witch) SpeciesType() world.EntityType               { return entityWitchType }
func (w *witch) behaviour() *witchBehaviour                { return w.Behaviour().(*witchBehaviour) }
func (w *witch) Tick(tx *world.Tx, n int64)                { w.behaviour().effects.Tick(w, tx); w.Ent.Tick(tx, n) }
func (w *witch) RaidID() int64                             { return w.behaviour().raidID }
func (w *witch) RaidWave() int                             { return w.behaviour().raidWave }
func (w *witch) IsRaidLeader() bool                        { return w.behaviour().raidLeader }
func (w *witch) CloseIn(tx *world.Tx) error                { tx.Raids().RaiderDied(w); return w.Ent.CloseIn(tx) }
func (b *witchBehaviour) joinRaid(e *Ent, tx *world.Tx) {
	raid := tx.Raids().ForRaider(e)
	if raid != nil {
		if !raid.Active() {
			tx.Raids().RaiderDied(e)
			b.raidID, b.raidWave, b.raidLeader = 0, 0, false
			return
		}
		b.raidID = raid.ID()
		return
	}
	if b.raidID == 0 {
		return
	}
	if tx.Raids().RegisterRaider(b.raidID, e) {
		return
	}
	b.raidID, b.raidWave, b.raidLeader = 0, 0, false
}
func (b *witchBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	b.joinRaid(e, tx)
	return b.aiTick(e, tx)
}

func (w *witch) Health() float64        { return w.behaviour().health.Health() }
func (w *witch) MaxHealth() float64     { return w.behaviour().health.MaxHealth() }
func (w *witch) SetMaxHealth(v float64) { w.behaviour().health.SetMaxHealth(v) }
func (w *witch) Dead() bool             { return w.behaviour().dead || w.Health() <= 0 }
func (w *witch) Hurt(d float64, s world.DamageSource) (float64, bool) {
	if w.Dead() || d <= 0 {
		return 0, false
	}
	b := w.behaviour()
	d, ok := b.ai.DamageCooldown(w.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	d = witchMagicAbsorb(w, d, s)
	if s.Fire() {
		b.lastFireDamage = w.tx.CurrentTick() + 1
	}
	b.health.AddHealth(-d)
	b.ai.Hurt(s)
	onMobHurt(w.tx, w)
	if w.Dead() {
		b.dead = true
		w.viewAction(DeathAction{})
		w.tx.Raids().RaiderKilled(w, raidDamageAttacker(s))
		dropAllDeathLoot(w.tx, w, s)
		_ = w.CloseIn(w.tx)
	} else {
		w.viewAction(HurtAction{})
	}
	return d, true
}
func (w *witch) Heal(h float64, _ world.HealingSource) float64 {
	if h <= 0 || w.Dead() {
		return 0
	}
	old := w.Health()
	w.behaviour().health.AddHealth(h)
	return w.Health() - old
}
func (w *witch) KnockBack(src mgl64.Vec3, f, h float64) {
	knockBack(w, src, f, h)
}
func (w *witch) AddEffect(e effect.Effect) {
	if e.Type() == effect.Poison || e.Type() == effect.FatalPoison {
		return
	}
	w.behaviour().effects.Add(e, w)
}
func (w *witch) RemoveEffect(t effect.Type) { w.behaviour().effects.Remove(t, w) }
func (w *witch) Effects() []effect.Effect   { return w.behaviour().effects.Effects() }
func (w *witch) Speed() float64             { return w.behaviour().speed }
func (w *witch) SetSpeed(v float64)         { w.behaviour().speed = v }
func (w *witch) viewAction(a world.EntityAction) {
	world.BroadcastEntityAction(w.tx, w, a)
}

var _ world.EntityType = witchType{}
var _ Living = (*witch)(nil)

func (b *witchBehaviour) effectManager() *EffectManager { return b.effects }

// removeWhenFarAway ports Raider.removeWhenFarAway for a witch.
func (b *witchBehaviour) removeWhenFarAway(m *Mob, distSqr float64) bool {
	return raiderRemoveWhenFarAway(m, &b.raiderState, distSqr)
}

// requiresCustomPersistence ports Raider.requiresCustomPersistence for a
// witch.
func (b *witchBehaviour) requiresCustomPersistence(m *Mob) bool {
	return m.tx.Raids().ForRaider(m.Self) != nil
}
