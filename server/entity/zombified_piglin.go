package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type ZombifiedPiglinType struct{}
type ZombifiedPiglin struct{ *Ent }
type zombifiedPiglinState struct {
	health     *HealthManager
	effects    *EffectManager
	mc         *MovementComputer
	baby, dead bool
	speed      float64
	mobGear

	neutralAnger
	ai MobAI
	// firstAngerSoundIn and alertIn count down to the first angry sound
	// and the next time the piglin alerts others.
	firstAngerSoundIn, alertIn int
}
type zombifiedPiglinConfig struct {
	health                float64
	angry                 bool
	anger, attackCooldown int
	target                string
	baby                  bool
	gear                  mobGear
}

func (ZombifiedPiglinType) EncodeEntity() string { return "minecraft:zombie_pigman" }
func (ZombifiedPiglinType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		newZombifiedPiglinConfig().Apply(d)
	}
	return &ZombifiedPiglin{&Ent{tx: tx, handle: h, data: d}}
}
func (ZombifiedPiglinType) BBox(e world.Entity) cube.BBox {
	if z, ok := e.(*ZombifiedPiglin); ok && z.z().baby {
		return cube.Box(-.245, 0, -.245, .245, .98, .245)
	}
	return cube.Box(-.3, 0, -.3, .3, 1.95, .3)
}
func (c zombifiedPiglinConfig) Apply(d *world.EntityData) {
	h := 20.
	if c.health > 0 {
		h = c.health
	}
	target, _ := uuid.Parse(c.target)
	d.Data = &zombifiedPiglinState{health: NewHealthManager(h, 20), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true},
		neutralAnger: neutralAnger{angerTicks: c.anger, angerTarget: target}, baby: c.baby, mobGear: c.gear, speed: .23}
}
func (ZombifiedPiglinType) DecodeNBT(n map[string]any, d *world.EntityData) {
	j := ReadNativeMobJavaState(n)
	if j == nil {
		j = n
	}
	c := zombifiedPiglinConfig{angry: nbtconv.Bool(j, "Angry"), anger: int(nbtconv.Int32(j, "AngerTime")), target: nbtconv.String(j, "AngerTarget"), attackCooldown: int(nbtconv.Int32(j, "AttackCooldown")), baby: nbtconv.Bool(j, "IsBaby"), gear: decodeGear(n)}
	_, native := n["Mainhand"]
	if _, java := n["equipment"]; !native && !java {
		// Saves that know nothing of its hands hold the golden sword it
		// spawns with.
		c.gear.items[slotMainhand] = item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1)
	}
	if x, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health = x.Current
	}
	c.Apply(d)
}
func (ZombifiedPiglinType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*zombifiedPiglinState)
	n := map[string]any{"Attributes": EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: 20, Current: s.health.Health(), DefaultMin: 0, DefaultMax: 20, Min: 0, Max: 20}})}
	javaGear := s.encodeGear(d.NativeNBT, n)
	out := MergeNativeMobNBT(d.NativeNBT, n)
	target := ""
	if s.angerTarget != uuid.Nil {
		target = s.angerTarget.String()
	}
	j := map[string]any{"Angry": byte(boolByte(s.angry())), "AngerTime": int32(s.angerTicks), "AngerTarget": target, "IsBaby": byte(boolByte(s.baby))}
	for k, v := range javaGear {
		j[k] = v
	}
	return MergeNativeMobJavaState(out, j)
}
func NewZombifiedPiglin(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(ZombifiedPiglinType{}, newZombifiedPiglinConfig())
}

// newZombifiedPiglinConfig returns the config of a zombified piglin holding
// the golden sword ZombifiedPiglin.populateDefaultEquipmentSlots gives it.
func newZombifiedPiglinConfig() zombifiedPiglinConfig {
	var c zombifiedPiglinConfig
	c.gear.items[slotMainhand] = item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1)
	return c
}
func (z *ZombifiedPiglin) z() *zombifiedPiglinState      { return z.Behaviour().(*zombifiedPiglinState) }
func (z *ZombifiedPiglin) SpeciesType() world.EntityType { return ZombifiedPiglinType{} }
func (z *ZombifiedPiglin) Tick(tx *world.Tx, n int64)    { z.z().effects.Tick(z, tx); z.Ent.Tick(tx, n) }

// validPlayerTarget ports NeutralMob.isValidPlayerTarget.
func validPlayerTarget(tx *world.Tx, x world.Entity) bool {
	if !isPlayerEntity(x) || tx.World().Difficulty() == world.DifficultyPeaceful {
		return false
	}
	if m, ok := x.(interface{ GameMode() world.GameMode }); ok {
		mode := m.GameMode()
		return mode != world.GameModeCreative && mode != world.GameModeSpectator
	}
	return true
}

func (z *ZombifiedPiglin) Health() float64        { return z.z().health.Health() }
func (z *ZombifiedPiglin) MaxHealth() float64     { return z.z().health.MaxHealth() }
func (z *ZombifiedPiglin) SetMaxHealth(v float64) { z.z().health.SetMaxHealth(v) }
func (z *ZombifiedPiglin) Dead() bool             { return z.z().dead || z.Health() <= 0 }
func (z *ZombifiedPiglin) viewAction(a world.EntityAction) {
	if z.tx == nil {
		return
	}
	vs := z.tx.Viewers(z.Position())
	for _, v := range vs {
		v.ViewEntityAction(z, a)
	}
	z.tx.ReleaseViewers(vs)
}
func (z *ZombifiedPiglin) Hurt(d float64, src world.DamageSource) (float64, bool) {
	s := z.z()
	if d <= 0 || z.Dead() || src.Fire() {
		return 0, false
	}
	d, ok := s.ai.DamageCooldown(z.tx, d)
	if !ok {
		return 0, false
	}
	// Zombies have an armour of 2.
	d = mobArmourAbsorb(s, 2, d, src)
	if r, ok := s.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	d = mobProtectionAbsorb(s, d, src)
	s.health.AddHealth(-d)
	s.ai.Hurt(src)
	z.viewAction(HurtAction{})
	onMobHurt(z.tx, z)
	if z.Dead() {
		s.dead = true
		z.viewAction(DeathAction{})
		dropAllDeathLoot(z.tx, z, src)
		_ = z.CloseIn(z.tx)
	}
	return d, true
}
func (z *ZombifiedPiglin) Heal(h float64, _ world.HealingSource) float64 {
	if h <= 0 || z.Dead() {
		return 0
	}
	old := z.Health()
	z.z().health.AddHealth(h)
	return z.Health() - old
}
func (z *ZombifiedPiglin) KnockBack(src mgl64.Vec3, f, k float64) { piglinKnock(z.Ent, src, f, k) }
func (z *ZombifiedPiglin) AddEffect(e effect.Effect) {
	if !hostileUndead("minecraft:zombified_piglin") || e.Type() != effect.Poison && e.Type() != effect.FatalPoison {
		z.z().effects.Add(e, z)
	}
}
func (z *ZombifiedPiglin) RemoveEffect(t effect.Type) { z.z().effects.Remove(t, z) }
func (z *ZombifiedPiglin) Effects() []effect.Effect   { return z.z().effects.Effects() }
func (z *ZombifiedPiglin) Speed() float64             { return z.z().speed }
func (z *ZombifiedPiglin) SetSpeed(v float64)         { z.z().speed = v }

// FinalizeSpawn ports Zombie.finalizeSpawn for a zombified piglin, which
// ZombifiedPiglin.populateDefaultEquipmentSlots arms with a golden sword.
func (z *ZombifiedPiglin) FinalizeSpawn(reason string, r *vanilla.Random) {
	if r == nil || z.tx == nil {
		return
	}
	s := z.z()
	difficulty := z.tx.LocalDifficulty(cube.PosFromVec3(z.Position()))
	baby, _, pickup, jockey := finalizeZombieSpawn(reason, difficulty, r, z.H().SpawnGroup(), s, func() {
		s.items[slotMainhand] = item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1)
	})
	s.baby = s.baby || baby
	if reason != "conversion" {
		s.pickUpLoot = pickup
	}
	rideChickenJockey(z.tx, z, jockey)
}

// HeldItems returns what the zombified piglin holds.
func (z *ZombifiedPiglin) HeldItems() (item.Stack, item.Stack) { return z.z().heldItems() }

// EquipmentArmour returns what the zombified piglin wears, head first.
func (z *ZombifiedPiglin) EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack) {
	return z.z().armour()
}

// dropCustomDeathLoot ports Mob.dropCustomDeathLoot for the equipment of the
// zombified piglin.
func (z *ZombifiedPiglin) dropCustomDeathLoot(killedByPlayer bool, looting int) {
	dropEquipmentOnDeath(z.tx, z, z.z(), killedByPlayer, looting)
}

// experienceEquipment counts the items the zombified piglin has that may
// drop, which Mob.getBaseExperienceReward rewards.
func (z *ZombifiedPiglin) experienceEquipment() int {
	s, n := z.z(), 0
	for slot := range equipmentSlots {
		if !s.items[slot].Empty() && s.dropChance(slot) <= 1 {
			n++
		}
	}
	return n
}
func (z *ZombifiedPiglin) hasEffect(t effect.Type) bool { _, ok := z.z().effects.Effect(t); return ok }
func piglinKnock(e *Ent, p mgl64.Vec3, f, k float64) {
	d := e.Position().Sub(p)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = k
	e.SetVelocity(d)
}

var _ world.EntityType = ZombifiedPiglinType{}
var _ Living = (*ZombifiedPiglin)(nil)

// UsingItem reports if the zombified piglin holds its spear out.
func (z *ZombifiedPiglin) UsingItem() bool { return !z.z().ai.useItem.Empty() }
