package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type ZombifiedPiglinType struct{}
type ZombifiedPiglin struct{ *Ent }
type zombifiedPiglinState struct {
	health         *HealthManager
	effects        *EffectManager
	mc             *MovementComputer
	angry          bool
	attack         *world.EntityHandle
	alertIn        int
	anger          int
	target         string
	attackCooldown int
	baby, dead     bool
	main           item.Stack
	speed          float64
}
type zombifiedPiglinConfig struct {
	health                float64
	angry                 bool
	anger, attackCooldown int
	target                string
	baby                  bool
	main                  item.Stack
}

func (ZombifiedPiglinType) EncodeEntity() string { return "minecraft:zombie_pigman" }
func (ZombifiedPiglinType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		(zombifiedPiglinConfig{main: item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1)}).Apply(d)
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
	d.Data = &zombifiedPiglinState{health: NewHealthManager(h, 20), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, angry: c.angry, anger: c.anger, target: c.target, attackCooldown: c.attackCooldown, baby: c.baby, main: c.main, speed: .23}
}
func (ZombifiedPiglinType) DecodeNBT(n map[string]any, d *world.EntityData) {
	j := ReadNativeMobJavaState(n)
	if j == nil {
		j = n
	}
	equipment := ReadNativeMobEquipment(n)
	c := zombifiedPiglinConfig{angry: nbtconv.Bool(j, "Angry"), anger: int(nbtconv.Int32(j, "AngerTime")), target: nbtconv.String(j, "AngerTarget"), attackCooldown: int(nbtconv.Int32(j, "AttackCooldown")), baby: nbtconv.Bool(j, "IsBaby"), main: item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1)}
	if main := piglinStackFromNative(equipment.Mainhand); !main.Empty() {
		c.main = main
	}
	if x, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health = x.Current
	}
	c.Apply(d)
}
func (ZombifiedPiglinType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*zombifiedPiglinState)
	n := map[string]any{"Attributes": EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: 20, Current: s.health.Health(), DefaultMin: 0, DefaultMax: 20, Min: 0, Max: 20}})}
	equipment := ReadNativeMobEquipment(d.NativeNBT)
	var prev NativeMobItem
	if len(equipment.Mainhand) > 0 {
		prev = equipment.Mainhand[0]
	}
	equipment.Mainhand = []NativeMobItem{piglinNativeStack(s.main, prev)}
	for k, v := range EncodeNativeMobEquipment(equipment) {
		n[k] = v
	}
	out := MergeNativeMobNBT(d.NativeNBT, n)
	j := map[string]any{"Angry": byte(boolByte(s.angry)), "AngerTime": int32(s.anger), "AngerTarget": s.target, "AttackCooldown": int32(s.attackCooldown), "IsBaby": byte(boolByte(s.baby))}
	return MergeNativeMobJavaState(out, j)
}
func NewZombifiedPiglin(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(ZombifiedPiglinType{}, zombifiedPiglinConfig{main: item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1)})
}
func (z *ZombifiedPiglin) z() *zombifiedPiglinState      { return z.Behaviour().(*zombifiedPiglinState) }
func (z *ZombifiedPiglin) SpeciesType() world.EntityType { return ZombifiedPiglinType{} }
func (z *ZombifiedPiglin) Tick(tx *world.Tx, n int64)    { z.z().effects.Tick(z, tx); z.Ent.Tick(tx, n) }

// zombifiedPiglinFollowRange is the zombie FOLLOW_RANGE attribute.
const zombifiedPiglinFollowRange = 35.0

// Tick ports the zombified piglin's NeutralMob AI: it only attacks players it
// is angry at (its persistent anger target, or every player under
// universal_anger), refreshes its 20-39 second anger while it has a target,
// and alerts nearby zombified piglins every 4-6 seconds.
func (s *zombifiedPiglinState) Tick(e *Ent, tx *world.Tx) *Movement {
	if s.attackCooldown > 0 {
		s.attackCooldown--
	}
	if s.anger > 0 {
		s.anger--
	}
	target := s.updateAnger(e, tx)
	if target == nil && rand.IntN(5) == 0 {
		target = s.findAngerTarget(e, tx)
	}
	s.attack = nil
	if target == nil {
		s.angry = s.anger > 0
		return piglinStop(&piglinState{mc: s.mc, speed: s.speed}, e, tx)
	}
	s.attack, s.angry = target.H(), true
	if s.alertIn--; s.alertIn <= 0 {
		s.alertIn = 80 + rand.IntN(41)
		if dragonLineOfSight(tx, EyePosition(e), EyePosition(target)) {
			zombifiedPiglinAlert(e, tx, target)
		}
	}
	dist := target.Position().Sub(e.Position()).Len()
	if dist < 1.8 && s.attackCooldown == 0 {
		if l, ok := target.(Living); ok {
			l.Hurt(5, AttackDamageSource{Attacker: e})
		}
		s.attackCooldown = 20
		return piglinStop(&piglinState{mc: s.mc, speed: s.speed, baby: s.baby}, e, tx)
	}
	return piglinMove(&piglinState{mc: s.mc, speed: s.speed, baby: s.baby}, e, tx, target.Position())
}

// updateAnger ports NeutralMob.updatePersistentAnger with
// stayAngryIfTargetPresent: it returns the current attack target, keeping
// the anger timer running while the target remains valid.
func (s *zombifiedPiglinState) updateAnger(e *Ent, tx *world.Tx) world.Entity {
	var target world.Entity
	if s.attack != nil {
		target, _ = s.attack.Entity(tx)
	}
	if target != nil {
		if l, ok := target.(Living); ok && l.Dead() {
			if !isPlayerEntity(target) && s.target == target.H().UUID().String() {
				s.stopBeingAngry()
				return nil
			}
			target = nil
		} else if target.Position().Sub(e.Position()).Len() > zombifiedPiglinFollowRange {
			target = nil
		}
	}
	if target != nil {
		s.target = target.H().UUID().String()
		s.startAngerTimer()
	}
	if s.target != "" && s.anger <= 0 && (target == nil || !validPlayerTarget(tx, target)) {
		s.stopBeingAngry()
		return nil
	}
	if s.target != "" {
		if id, err := uuid.Parse(s.target); err == nil {
			if p, ok := tx.EntityByUUID(id); ok && isPlayerEntity(p) && !validPlayerTarget(tx, p) {
				s.stopBeingAngry()
				return nil
			}
		}
	}
	if target != nil && isPlayerEntity(target) && !validPlayerTarget(tx, target) {
		return nil
	}
	return target
}

// findAngerTarget ports NearestAttackableTargetGoal<Player> with the
// isAngryAt predicate: the nearest visible player within follow range that
// the piglin is angry at.
func (s *zombifiedPiglinState) findAngerTarget(e *Ent, tx *world.Tx) world.Entity {
	r := zombifiedPiglinFollowRange
	var best world.Entity
	bestDist := r
	for x := range tx.EntitiesWithin(cube.Box(-r, -r, -r, r, r, r).Translate(e.Position())) {
		if !isPlayerEntity(x) || !s.angryAt(tx, x) {
			continue
		}
		if d := x.Position().Sub(e.Position()).Len(); d < bestDist && dragonLineOfSight(tx, EyePosition(e), EyePosition(x)) {
			best, bestDist = x, d
		}
	}
	return best
}

// angryAt ports NeutralMob.isAngryAt.
func (s *zombifiedPiglinState) angryAt(tx *world.Tx, x world.Entity) bool {
	if l, ok := x.(Living); !ok || l.Dead() {
		return false
	}
	if validPlayerTarget(tx, x) && s.universalAnger(tx) {
		return true
	}
	return s.target != "" && s.target == x.H().UUID().String()
}

// universalAnger ports NeutralMob.isAngryAtAllPlayers.
func (s *zombifiedPiglinState) universalAnger(tx *world.Tx) bool {
	return tx.World().GameRuleBool("universal_anger") && s.anger > 0 && s.target == ""
}

// startAngerTimer samples PERSISTENT_ANGER_TIME (20-39 seconds).
func (s *zombifiedPiglinState) startAngerTimer() { s.anger = 400 + rand.IntN(381) }

// stopBeingAngry ports NeutralMob.stopBeingAngry.
func (s *zombifiedPiglinState) stopBeingAngry() {
	s.target, s.attack, s.anger, s.angry = "", nil, 0, false
}

// setAttackTarget ports ZombifiedPiglin.setTarget followed by the next
// updatePersistentAnger: the target becomes the persistent anger target and
// acquiring a first target schedules the next alert.
func (s *zombifiedPiglinState) setAttackTarget(target world.Entity) {
	if s.attack == nil {
		s.alertIn = 80 + rand.IntN(41)
	}
	s.attack, s.angry = target.H(), true
	s.target = target.H().UUID().String()
	s.startAngerTimer()
}

// hurtBy ports HurtByTargetGoal(this).setAlertOthers(): the attacker becomes
// the target of this piglin and of nearby untargeted piglins. Under
// universal_anger, ResetUniversalAngerTargetGoal then clears this piglin's
// own target and refreshes its anger, making it angry at every player.
func (s *zombifiedPiglinState) hurtBy(e *Ent, tx *world.Tx, attacker world.Entity) {
	if attacker == nil || attacker.H() == e.H() {
		return
	}
	if isPlayerEntity(attacker) && !validPlayerTarget(tx, attacker) {
		return
	}
	zombifiedPiglinAlert(e, tx, attacker)
	if isPlayerEntity(attacker) && tx.World().GameRuleBool("universal_anger") {
		s.stopBeingAngry()
		s.startAngerTimer()
		return
	}
	s.setAttackTarget(attacker)
}

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

// zombifiedPiglinAlert ports ZombifiedPiglin.alertOthers: piglins within
// follow range horizontally and 10 blocks vertically without a target take
// the target over.
func zombifiedPiglinAlert(e *Ent, tx *world.Tx, target world.Entity) {
	r := zombifiedPiglinFollowRange
	lo := e.Position()
	box := cube.Box(lo[0]-r, lo[1]-10, lo[2]-r, lo[0]+1+r, lo[1]+11, lo[2]+1+r)
	for x := range tx.EntitiesWithin(box) {
		if z, ok := x.(*ZombifiedPiglin); ok && z.H() != e.H() && z.z().attack == nil {
			z.z().setAttackTarget(target)
		}
	}
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
	if r, ok := s.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	s.health.AddHealth(-d)
	s.hurtBy(z.Ent, z.tx, mobKiller(src))
	z.viewAction(HurtAction{})
	if z.Dead() {
		s.dead = true
		z.viewAction(DeathAction{})
		dropMobLoot(z.tx, z.Position(), "minecraft:zombified_piglin", src)
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
func (z *ZombifiedPiglin) RemoveEffect(t effect.Type)               { z.z().effects.Remove(t, z) }
func (z *ZombifiedPiglin) Effects() []effect.Effect                 { return z.z().effects.Effects() }
func (z *ZombifiedPiglin) Speed() float64                           { return z.z().speed }
func (z *ZombifiedPiglin) SetSpeed(v float64)                       { z.z().speed = v }
func (z *ZombifiedPiglin) FinalizeSpawn(_ string, _ *java26.Random) {}
func (z *ZombifiedPiglin) hasEffect(t effect.Type) bool             { _, ok := z.z().effects.Effect(t); return ok }
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
