package entity

import (
	"hash/fnv"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type PiglinType struct{}
type piglinState struct {
	health                                                             *HealthManager
	effects                                                            *EffectManager
	mc                                                                 *MovementComputer
	random                                                             java26.Random
	baby, admire, dancing, charging, cannotHunt, dead                  bool
	conversion, attackCooldown, admireTicks, barterTicks, retreatTicks int
	target                                                             string
	home                                                               cube.Pos
	homeSet                                                            bool
	homeDimension                                                      string
	navigator                                                          Navigator
	wander                                                             int
	main                                                               item.Stack
	armor                                                              [4]item.Stack
	speed                                                              float64
}
type Piglin struct{ *Ent }
type piglinConfig struct {
	health, maxHealth                                    float64
	baby, admire, dancing, cannotHunt                    bool
	conversion, attackCooldown, admireTicks, barterTicks int
	target                                               string
	home                                                 cube.Pos
	homeSet                                              bool
	homeDimension                                        string
	main                                                 item.Stack
	armor                                                [4]item.Stack
}

func (PiglinType) EncodeEntity() string { return "minecraft:piglin" }
func (PiglinType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		(piglinConfig{}).Apply(d)
	}
	e := &Ent{tx: tx, handle: h, data: d}
	if h != nil {
		u := h.UUID()
		r := java26.NewLegacyRandom(int64(fnv64(u[:])))
		e.Behaviour().(*piglinState).random = r
	}
	return &Piglin{e}
}
func (PiglinType) BBox(e world.Entity) cube.BBox {
	if p, ok := e.(*Piglin); ok && p.p().baby {
		return cube.Box(-.245, 0, -.245, .245, .98, .245)
	}
	return cube.Box(-.3, 0, -.3, .3, 1.95, .3)
}
func (c piglinConfig) Apply(d *world.EntityData) {
	hp := 16.
	if c.health > 0 {
		hp = c.health
	}
	maxHealth := c.maxHealth
	if maxHealth <= 0 {
		maxHealth = 16
	}
	d.Data = &piglinState{health: NewHealthManager(hp, maxHealth), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, baby: c.baby, admire: c.admire, dancing: c.dancing, cannotHunt: c.cannotHunt, conversion: c.conversion, attackCooldown: c.attackCooldown, admireTicks: c.admireTicks, barterTicks: c.barterTicks, target: c.target, home: c.home, homeSet: c.homeSet, homeDimension: c.homeDimension, main: c.main, armor: c.armor, speed: .35}
}
func (PiglinType) DecodeNBT(n map[string]any, d *world.EntityData) {
	j := ReadNativeMobJavaState(n)
	if j == nil {
		j = n
	}
	eq := ReadNativeMobEquipment(n)
	c := piglinConfig{baby: nbtconv.Bool(j, "IsBaby"), admire: nbtconv.Bool(j, "Admiring"), dancing: nbtconv.Bool(j, "Dancing"), cannotHunt: nbtconv.Bool(j, "CannotHunt"), conversion: int(nbtconv.Int32(j, "TimeInOverworld")), attackCooldown: int(nbtconv.Int32(j, "AttackCooldown")), admireTicks: int(nbtconv.Int32(j, "AdmireTicks")), barterTicks: int(nbtconv.Int32(j, "BarterTicks")), target: nbtconv.String(j, "AngerTarget"), main: piglinStackFromNative(eq.Mainhand)}
	if len(eq.Armor) > 0 {
		for i := range c.armor {
			if i < len(eq.Armor) {
				c.armor[i] = piglinStackFromNative(eq.Armor[i : i+1])
			}
		}
	}
	if x, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health, c.maxHealth = x.Current, x.Base
	} else if _, ok := j["Health"]; ok {
		c.health = float64(nbtconv.Float32(j, "Health"))
	}
	c.Apply(d)
}
func piglinNativeStack(s item.Stack, previous NativeMobItem) NativeMobItem {
	current := nativeMobItemFromStack(s)
	current.Raw = MergeNativeMobNBT(previous.Raw, current.Raw)
	return current
}
func piglinStackFromNative(items []NativeMobItem) item.Stack {
	for _, v := range items {
		if v.Name != "" && v.Count > 0 {
			return item.ReadNBT(v.Raw, nil)
		}
	}
	return item.Stack{}
}
func encodePiglinNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*piglinState)
	maxHealth := s.health.MaxHealth()
	native := map[string]any{"Attributes": EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: maxHealth, Current: s.health.Health(), DefaultMin: 0, DefaultMax: maxHealth, Min: 0, Max: maxHealth}})}
	equipment := ReadNativeMobEquipment(d.NativeNBT)
	var prev NativeMobItem
	if len(equipment.Mainhand) > 0 {
		prev = equipment.Mainhand[0]
	}
	equipment.Mainhand = []NativeMobItem{piglinNativeStack(s.main, prev)}
	if len(equipment.Armor) < len(s.armor) {
		armor := make([]NativeMobItem, len(s.armor))
		copy(armor, equipment.Armor)
		equipment.Armor = armor
	}
	for i, stack := range s.armor {
		equipment.Armor[i] = piglinNativeStack(stack, equipment.Armor[i])
	}
	for k, v := range EncodeNativeMobEquipment(equipment) {
		native[k] = v
	}
	out := MergeNativeMobNBT(d.NativeNBT, native)
	j := ReadNativeMobJavaState(d.NativeNBT)
	if j == nil {
		j = make(map[string]any)
	}
	sourceEquipment, _ := j["equipment"].(map[string]any)
	sourceEquipment = CloneNativeMobNBT(sourceEquipment)
	if sourceEquipment == nil {
		sourceEquipment = make(map[string]any)
	}
	writeSourceStack := func(slot string, stack item.Stack) {
		if stack.Empty() {
			delete(sourceEquipment, slot)
			return
		}
		if encoded, err := item.StackToJavaNBT(stack); err == nil {
			sourceEquipment[slot] = encoded
		}
	}
	writeSourceStack("mainhand", s.main)
	for i, slot := range []string{"head", "chest", "legs", "feet"} {
		writeSourceStack(slot, s.armor[i])
	}
	if len(sourceEquipment) == 0 {
		delete(j, "equipment")
	} else {
		j["equipment"] = sourceEquipment
	}
	j["IsBaby"], j["Admiring"], j["Dancing"], j["CannotHunt"] = byte(boolByte(s.baby)), byte(boolByte(s.admire)), byte(boolByte(s.dancing)), byte(boolByte(s.cannotHunt))
	j["TimeInOverworld"], j["AttackCooldown"], j["AdmireTicks"], j["BarterTicks"], j["AngerTarget"] = int32(s.conversion), int32(s.attackCooldown), int32(s.admireTicks), int32(s.barterTicks), s.target
	return MergeNativeMobJavaState(out, j)
}
func (PiglinType) EncodeNBT(d *world.EntityData) map[string]any { return encodePiglinNBT(d) }
func fnv64(b []byte) uint64                                     { h := fnv.New64a(); _, _ = h.Write(b); return h.Sum64() }
func NewPiglin(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(PiglinType{}, piglinConfig{})
}
func (p *Piglin) p() *piglinState               { return p.Behaviour().(*piglinState) }
func (p *Piglin) SpeciesType() world.EntityType { return PiglinType{} }
func (p *Piglin) viewAction(a world.EntityAction) {
	vs := p.tx.Viewers(p.Position())
	for _, v := range vs {
		v.ViewEntityAction(p, a)
	}
	p.tx.ReleaseViewers(vs)
}
func (p *Piglin) Tick(tx *world.Tx, n int64) { p.p().effects.Tick(p, tx); p.Ent.Tick(tx, n) }
func (s *piglinState) Tick(e *Ent, tx *world.Tx) *Movement {
	if s.attackCooldown > 0 {
		s.attackCooldown--
	}
	if s.admireTicks > 0 {
		s.admireTicks--
	}
	if s.barterTicks > 0 {
		s.barterTicks--
	}
	if tx.World().Dimension() == world.Overworld {
		if s.conversion < 300 {
			s.conversion++
		}
		if s.conversion >= 300 {
			opts := world.EntitySpawnOpts{Position: e.Position(), Rotation: e.Rotation(), SpawnReason: "conversion"}
			z := opts.New(ZombifiedPiglinType{}, zombifiedPiglinConfig{health: s.health.Health(), baby: s.baby, main: s.main, anger: 600, target: s.target, angry: s.target != ""})
			tx.AddEntity(z)
			e.Close()
			return nil
		}
	} else {
		s.conversion = 0
	}
	if s.dancing {
		s.dancing = false
	}
	if s.admireTicks == 0 {
		s.admire = false
	}
	if target, dist := piglinTarget(e, tx, s); target != nil {
		if dist < 1.8 && s.attackCooldown == 0 {
			if l, ok := target.(Living); ok {
				l.Hurt(5, AttackDamageSource{Attacker: e})
			}
			s.attackCooldown = 20
			return piglinStop(s, e, tx)
		}
		return piglinMove(s, e, tx, target.Position())
	}
	if pos := nearbyGoldOrItem(tx, e.Position()); pos != nil {
		return piglinMove(s, e, tx, pos.Vec3())
	}
	return piglinWander(s, e, tx)
}
func piglinTarget(e *Ent, tx *world.Tx, s *piglinState) (world.Entity, float64) {
	r := 16.
	var best world.Entity
	d0 := r
	for x := range tx.EntitiesWithin(cube.Box(-r, -8, -r, r, 8, r).Translate(e.Position())) {
		if x == e {
			continue
		}
		id := x.H().Type().EncodeEntity()
		ok := id == "minecraft:player" || id == "minecraft:hoglin" || id == "minecraft:wither_skeleton"
		if !ok {
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
func nearbyGoldOrItem(tx *world.Tx, p mgl64.Vec3) *cube.Pos {
	start := cube.PosFromVec3(p)
	for x := -6; x <= 6; x++ {
		for y := -3; y <= 3; y++ {
			for z := -6; z <= 6; z++ {
				q := start.Add(cube.Pos{x, y, z})
				id, _ := tx.Block(q).EncodeBlock()
				if id == "minecraft:gold_block" || id == "minecraft:raw_gold_block" || id == "minecraft:nether_gold_ore" {
					v := q
					return &v
				}
			}
		}
	}
	return nil
}
func (p *Piglin) tryBarter(tx *world.Tx) {
	s := p.p()
	if s.baby || s.main.Empty() {
		return
	}
	name, _ := s.main.Item().EncodeItem()
	if name != "minecraft:gold_ingot" || s.barterTicks > 0 {
		return
	}
	s.main = s.main.Grow(-1)
	r := s.random
	stacks, err := loot.Generate("minecraft:gameplay/piglin_bartering", 0, loot.Context{Random: &r, Entity: "minecraft:piglin", Origin: p.Position()})
	if err != nil {
		return
	}
	s.random = r
	for _, stack := range stacks {
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: p.Position()}, stack))
	}
	s.barterTicks = 120
	s.admire = true
	s.admireTicks = 120
}
func piglinMove(s *piglinState, e *Ent, tx *world.Tx, p mgl64.Vec3) *Movement {
	d := p.Sub(e.Position())
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize()
	}
	v := e.data.Vel
	speed := s.speed
	if s.baby {
		speed *= 1.2
	}
	v[0], v[2] = d[0]*speed, d[2]*speed
	r := e.data.Rot
	if d.Len() > 0 {
		r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	}
	m := s.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, r, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func piglinStop(s *piglinState, e *Ent, tx *world.Tx) *Movement {
	v := e.data.Vel
	v[0], v[2] = 0, 0
	m := s.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func piglinWander(s *piglinState, e *Ent, tx *world.Tx) *Movement {
	if s.wander > 0 {
		s.wander--
	}
	if s.navigator.Index >= len(s.navigator.Path) && s.wander == 0 {
		s.wander = 50
		start := cube.PosFromVec3(e.Position())
		goal := start.Add(cube.Pos{int(s.random.NextInt(11)) - 5, 0, int(s.random.NextInt(11)) - 5})
		s.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 80}, navigatorTick(e))
	}
	if q, ok := s.navigator.Tick(e.Position(), navigatorTick(e), .6); ok {
		if q.Sub(e.Position()).Len() < .4 {
			s.navigator.Index++
		} else {
			return piglinMove(s, e, tx, q)
		}
	}
	return piglinStop(s, e, tx)
}
func (p *Piglin) Health() float64        { return p.p().health.Health() }
func (p *Piglin) MaxHealth() float64     { return p.p().health.MaxHealth() }
func (p *Piglin) SetMaxHealth(v float64) { p.p().health.SetMaxHealth(v) }
func (p *Piglin) Dead() bool             { return p.p().dead || p.Health() <= 0 }
func (p *Piglin) Hurt(d float64, src world.DamageSource) (float64, bool) {
	s := p.p()
	if d <= 0 || p.Dead() {
		return 0, false
	}
	if src.Fire() && p.hasEffect(effect.FireResistance) {
		return 0, false
	}
	if r, ok := s.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	s.health.AddHealth(-d)
	p.viewAction(HurtAction{})
	onMobHurt(p.tx, p)
	if p.Dead() {
		s.dead = true
		p.viewAction(DeathAction{})
		dropAllDeathLoot(p.tx, p, src)
		_ = p.CloseIn(p.tx)
	}
	return d, true
}
func (p *Piglin) Interact(tx *world.Tx, u item.User, c *item.UseContext) bool {
	if u == nil {
		return false
	}
	a, b := u.HeldItems()
	for _, st := range []item.Stack{a, b} {
		if st.Empty() {
			continue
		}
		name, _ := st.Item().EncodeItem()
		if name == "minecraft:gold_ingot" {
			s := p.p()
			s.admire = true
			s.admireTicks = 120
			s.main = item.NewStack(st.Item(), 1)
			if c != nil {
				c.SubtractFromCount(1)
			}
			p.tryBarter(tx)
			return true
		}
	}
	return false
}
func (p *Piglin) Heal(h float64, _ world.HealingSource) float64 {
	if h <= 0 || p.Dead() {
		return 0
	}
	old := p.Health()
	p.p().health.AddHealth(h)
	return p.Health() - old
}
func (p *Piglin) KnockBack(src mgl64.Vec3, f, k float64) {
	d := p.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = k
	p.SetVelocity(d)
}
func (p *Piglin) AddEffect(e effect.Effect)  { p.p().effects.Add(e, p) }
func (p *Piglin) RemoveEffect(t effect.Type) { p.p().effects.Remove(t, p) }
func (p *Piglin) Effects() []effect.Effect   { return p.p().effects.Effects() }
func (p *Piglin) Speed() float64             { return p.p().speed }
func (p *Piglin) SetSpeed(v float64)         { p.p().speed = v }
func (p *Piglin) FinalizeSpawn(reason string, r *java26.Random) {
	if r == nil || reason == "conversion" {
		return
	}
	s := p.p()
	if r.NextFloat() < .2 {
		s.baby = true
	} else if r.NextBoolean() {
		s.main = item.NewStack(item.Crossbow{}, 1)
	} else {
		s.main = item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1)
	}
}
func (p *Piglin) HeldItems() (item.Stack, item.Stack) { return p.p().main, item.Stack{} }
func (p *Piglin) SetHeldItems(main, _ item.Stack)     { p.p().main = main }
func (p *Piglin) EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack) {
	a := p.p().armor
	return a[0], a[1], a[2], a[3]
}

var _ world.EntityType = PiglinType{}
var _ Living = (*Piglin)(nil)
var _ Interactable = (*Piglin)(nil)

func (p *Piglin) hasEffect(t effect.Type) bool { _, ok := p.p().effects.Effect(t); return ok }
