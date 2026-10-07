package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type ZombifiedPiglinType struct{}
type ZombifiedPiglin struct{ *Ent }
type zombifiedPiglinState struct {
	health         *HealthManager
	effects        *EffectManager
	mc             *MovementComputer
	angry          bool
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
func (s *zombifiedPiglinState) Tick(e *Ent, tx *world.Tx) *Movement {
	if s.attackCooldown > 0 {
		s.attackCooldown--
	}
	if s.anger > 0 {
		s.anger--
	} else {
		s.angry = false
		s.target = ""
	}
	target, dist := zombiePiglinTarget(e, tx, s)
	if target != nil {
		if !s.angry {
			zombifiedPiglinAlert(e, tx, target)
		}
		s.angry = true
		s.target = target.H().UUID().String()
		s.anger = 600
		if dist < 1.8 && s.attackCooldown == 0 {
			if l, ok := target.(Living); ok {
				l.Hurt(5, AttackDamageSource{Attacker: e})
			}
			s.attackCooldown = 20
			return piglinStop(&piglinState{mc: s.mc, speed: s.speed, baby: s.baby}, e, tx)
		}
		return piglinMove(&piglinState{mc: s.mc, speed: s.speed, baby: s.baby}, e, tx, target.Position())
	}
	return piglinStop(&piglinState{mc: s.mc, speed: s.speed}, e, tx)
}
func zombiePiglinTarget(e *Ent, tx *world.Tx, s *zombifiedPiglinState) (world.Entity, float64) {
	r := 16.
	box := cube.Box(-r, -8, -r, r, 8, r).Translate(e.Position())
	var best world.Entity
	d0 := r
	for x := range tx.EntitiesWithin(box) {
		if x == e {
			continue
		}
		if x.H().Type().EncodeEntity() != "minecraft:player" {
			continue
		}
		l, ok := x.(Living)
		if !ok || l.Dead() {
			continue
		}
		d := x.Position().Sub(e.Position()).Len()
		if d < d0 {
			d0, best = d, x
		}
	}
	return best, d0
}
func zombifiedPiglinAlert(e *Ent, tx *world.Tx, target world.Entity) {
	for x := range tx.EntitiesWithin(cube.Box(-32, -16, -32, 32, 16, 32).Translate(e.Position())) {
		if z, ok := x.(*ZombifiedPiglin); ok && z != nil {
			z.z().angry = true
			z.z().anger = 600
			z.z().target = target.H().UUID().String()
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
