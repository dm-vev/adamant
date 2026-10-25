package entity

import (
	"hash/fnv"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// UsingItem reports if the witch drinks a potion.
func (w *Witch) UsingItem() bool { return w.behaviour().drinking }

// HeldItems returns the potion the witch drinks, if any.
func (w *Witch) HeldItems() (item.Stack, item.Stack) {
	b := w.behaviour()
	if !b.drinking {
		return item.Stack{}, item.Stack{}
	}
	return item.NewStack(item.Potion{Type: b.drinkPotion}, 1), item.Stack{}
}

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
	b.random = vanilla.NewLegacyRandom(int64(hash.Sum64()))
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
		speed: .25, drinking: c.drinking, potionUseTime: c.PotionUseTime, drinkPotion: c.DrinkPotion, random: vanilla.NewLegacyRandom(0),
		raidID: c.RaidID, raidWave: c.RaidWave, raidLeader: c.RaidLeader,
	}
}

type WitchBehaviour struct {
	health        *HealthManager
	effects       *EffectManager
	mc            *MovementComputer
	random        vanilla.Random
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
	attack      *NearestAttackableTargetGoal
	attackAllow bool
	// lastFireDamage is the world tick the witch last took fire damage.
	lastFireDamage int64
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
	return b.aiTick(e, tx)
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
	d, ok := b.ai.DamageCooldown(w.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	d = witchMagicAbsorb(w, d, s)
	if s.Fire() {
		b.lastFireDamage = w.tx.CurrentTick()
	}
	b.health.AddHealth(-d)
	b.ai.Hurt(s)
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
	knockBack(w, src, f, h)
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
