package entity

import (
	"hash/fnv"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type PiglinType struct{}

// piglinState is the state of Java's AbstractPiglin, of a Piglin or of a
// PiglinBrute.
type piglinState struct {
	health  *HealthManager
	effects *EffectManager
	ai      MobAI
	br      *Brain
	brute   bool

	baby, cannotHunt, immune, dead bool
	// dancing and chargingCrossbow are Piglin.DATA_IS_DANCING and
	// DATA_IS_CHARGING_CROSSBOW.
	dancing, chargingCrossbow bool
	conversion                int
	main, off                 item.Stack
	armor                     [4]item.Stack
	// inventory is the SimpleContainer of eight slots of a piglin.
	inventory [8]item.Stack
	// guaranteedMain and guaranteedOff are set for items the piglin picked
	// up, which it drops on death, as Mob.setGuaranteedDrop.
	guaranteedMain, guaranteedOff bool
	speed                         float64
}
type Piglin struct{ *Ent }
type piglinConfig struct {
	health, maxHealth              float64
	baby, cannotHunt, immune, brut bool
	conversion                     int
	angryAt                        uuid.UUID
	home                           *globalPos
	main, off                      item.Stack
	armor                          [4]item.Stack
	inventory                      []item.Stack
}

func (PiglinType) EncodeEntity() string { return "minecraft:piglin" }
func (PiglinType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		(piglinConfig{}).Apply(d)
	}
	return &Piglin{&Ent{tx: tx, handle: h, data: d}}
}
func (PiglinType) BBox(e world.Entity) cube.BBox {
	if p, ok := e.(*Piglin); ok && p.p().baby {
		return cube.Box(-.245, 0, -.245, .245, .98, .245)
	}
	return cube.Box(-.3, 0, -.3, .3, 1.95, .3)
}
func (c piglinConfig) Apply(d *world.EntityData) {
	hp, maxHealth := 16., 16.
	if c.brut {
		hp, maxHealth = 50, 50
	}
	if c.health > 0 {
		hp = c.health
	}
	if c.maxHealth > 0 {
		maxHealth = c.maxHealth
	}
	s := &piglinState{health: NewHealthManager(hp, maxHealth), effects: NewEffectManager(), brute: c.brut, baby: c.baby && !c.brut, cannotHunt: c.cannotHunt, immune: c.immune, conversion: c.conversion, main: c.main, off: c.off, armor: c.armor, speed: .35}
	copy(s.inventory[:], c.inventory)
	if c.brut {
		s.br = newPiglinBruteBrain(s)
		if c.home != nil {
			s.br.set(memHome, *c.home)
		}
	} else {
		s.br = newPiglinBrain(s)
	}
	if c.angryAt != uuid.Nil {
		s.br.setWithExpiry(memAngryAt, c.angryAt, 600)
	}
	d.Data = s
}
func (PiglinType) DecodeNBT(n map[string]any, d *world.EntityData) {
	j := ReadNativeMobJavaState(n)
	if j == nil {
		j = n
	}
	eq := ReadNativeMobEquipment(n)
	c := piglinConfig{baby: nbtconv.Bool(j, "IsBaby"), cannotHunt: nbtconv.Bool(j, "CannotHunt"), immune: nbtconv.Bool(j, "IsImmuneToZombification"), conversion: int(nbtconv.Int32(j, "TimeInOverworld")),
		main: piglinStackFromNative(eq.Mainhand), off: piglinStackFromNative(eq.Offhand)}
	c.angryAt, _ = uuid.Parse(nbtconv.String(j, "AngerTarget"))
	for i := range c.armor {
		if i < len(eq.Armor) {
			c.armor[i] = piglinStackFromNative(eq.Armor[i : i+1])
		}
	}
	if inv, ok := j["Inventory"].([]any); ok {
		for _, raw := range inv {
			if tag, ok := raw.(map[string]any); ok {
				if s, err := item.StackFromStructureNBT(tag); err == nil && !s.Empty() {
					c.inventory = append(c.inventory, s)
				}
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
	var prev, prevOff NativeMobItem
	if len(equipment.Mainhand) > 0 {
		prev = equipment.Mainhand[0]
	}
	if len(equipment.Offhand) > 0 {
		prevOff = equipment.Offhand[0]
	}
	equipment.Mainhand = []NativeMobItem{piglinNativeStack(s.main, prev)}
	equipment.Offhand = []NativeMobItem{piglinNativeStack(s.off, prevOff)}
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
		if encoded, err := item.StackToStructureNBT(stack); err == nil {
			sourceEquipment[slot] = encoded
		}
	}
	writeSourceStack("mainhand", s.main)
	writeSourceStack("offhand", s.off)
	for i, slot := range []string{"head", "chest", "legs", "feet"} {
		writeSourceStack(slot, s.armor[i])
	}
	if len(sourceEquipment) == 0 {
		delete(j, "equipment")
	} else {
		j["equipment"] = sourceEquipment
	}
	inv := make([]any, 0, len(s.inventory))
	for _, st := range s.inventory {
		if encoded, err := item.StackToStructureNBT(st); err == nil && !st.Empty() {
			inv = append(inv, encoded)
		}
	}
	for _, old := range []string{"Admiring", "Dancing", "AdmireTicks", "BarterTicks", "AttackCooldown"} {
		delete(j, old)
	}
	j["IsBaby"], j["CannotHunt"], j["IsImmuneToZombification"] = byte(boolByte(s.baby)), byte(boolByte(s.cannotHunt)), byte(boolByte(s.immune))
	j["TimeInOverworld"], j["Inventory"] = int32(s.conversion), inv
	j["AngerTarget"] = ""
	if v, ok := s.br.get(memAngryAt); ok {
		j["AngerTarget"] = v.(uuid.UUID).String()
	}
	if v, ok := s.br.get(memHome); ok {
		p := v.(globalPos)
		j["Brain"] = map[string]any{"memories": map[string]any{"minecraft:home": map[string]any{"value": map[string]any{"dimension": dimensionName(p.dim), "pos": []int32{int32(p.pos[0]), int32(p.pos[1]), int32(p.pos[2])}}}}}
	}
	return MergeNativeMobJavaState(out, j)
}

// dimensionName returns the Java identifier of a dimension.
func dimensionName(dim world.Dimension) string {
	switch dim {
	case world.Nether:
		return "minecraft:the_nether"
	case world.End:
		return "minecraft:the_end"
	}
	return "minecraft:overworld"
}

// dimensionByName returns the dimension of a Java identifier.
func dimensionByName(name string) world.Dimension {
	switch name {
	case "minecraft:the_nether":
		return world.Nether
	case "minecraft:the_end":
		return world.End
	}
	return world.Overworld
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
func (p *Piglin) Health() float64            { return p.p().health.Health() }
func (p *Piglin) MaxHealth() float64         { return p.p().health.MaxHealth() }
func (p *Piglin) SetMaxHealth(v float64)     { p.p().health.SetMaxHealth(v) }
func (p *Piglin) Dead() bool                 { return p.p().dead || p.Health() <= 0 }

// self returns the entity of the piglin: a *PiglinBrute for a brute.
func (s *piglinState) self(e *Ent) world.Entity {
	if s.brute {
		return &PiglinBrute{&Piglin{e}}
	}
	return &Piglin{e}
}

// Hurt ports AbstractPiglin.hurtServer and those of Piglin and PiglinBrute:
// a piglin hurt by a living entity calls on its brain.
func (p *Piglin) Hurt(d float64, src world.DamageSource) (float64, bool) {
	s := p.p()
	if d <= 0 || p.Dead() {
		return 0, false
	}
	var ok bool
	if d, ok = s.ai.DamageCooldown(p.tx, d); !ok {
		return 0, false
	}
	if d, ok = mobEffectDamage(s.effects, d, src); !ok {
		return 0, false
	}
	self := s.self(p.Ent)
	s.health.AddHealth(-d)
	s.ai.Hurt(src)
	p.viewAction(HurtAction{})
	onMobHurt(p.tx, self)
	if p.Dead() {
		s.dead = true
		p.viewAction(DeathAction{})
		dropAllDeathLoot(p.tx, self, src)
		_ = p.CloseIn(p.tx)
		return d, true
	}
	if attacker, ok := mobKiller(src).(Living); ok && attacker != nil {
		m := s.ai.Mob(p.Ent, self)
		if s.brute {
			piglinBruteWasHurtBy(m, s, attacker)
		} else {
			piglinWasHurtBy(m, s, attacker)
		}
	}
	return d, true
}

// dropCustomDeathLoot ports Mob.dropCustomDeathLoot and Piglin's: a piglin
// drops what it carries and maybe what it holds and wears.
func (p *Piglin) dropCustomDeathLoot(killedByPlayer bool, looting int) {
	s := p.p()
	chance := func(guaranteed bool) float32 {
		if guaranteed {
			return 2
		}
		return .085
	}
	dropMobEquipment(p.tx, p, s.main, chance(s.guaranteedMain), killedByPlayer, looting)
	dropMobEquipment(p.tx, p, s.off, chance(s.guaranteedOff), killedByPlayer, looting)
	for _, a := range s.armor {
		dropMobEquipment(p.tx, p, a, .085, killedByPlayer, looting)
	}
	for i, st := range s.inventory {
		if !st.Empty() {
			p.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: p.Position()}, st))
			s.inventory[i] = item.Stack{}
		}
	}
}

// Interact ports Piglin.mobInteract: an adult piglin takes a gold ingot from
// a player to admire.
func (p *Piglin) Interact(tx *world.Tx, u item.User, c *item.UseContext) bool {
	s := p.p()
	if u == nil || s.brute {
		return false
	}
	held, _ := u.HeldItems()
	m := s.ai.Mob(p.Ent, p)
	if !piglinCanAdmire(m, s, held) {
		return false
	}
	if c != nil {
		c.SubtractFromCount(1)
	}
	piglinHoldInOffhand(m, s, held.Grow(1-held.Count()))
	piglinAdmireGoldItem(m)
	piglinStopWalking(m)
	return true
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

// FinalizeSpawn ports Piglin.finalizeSpawn: one piglin in five is a baby,
// and adults take up a crossbow, or a golden sword or now and then a golden
// spear, and may wear golden armour. PiglinAi.initMemories puts off its
// first hunt.
func (p *Piglin) FinalizeSpawn(reason string, r *vanilla.Random) {
	if r == nil || reason == "conversion" {
		return
	}
	s := p.p()
	if reason != "structure" {
		if r.NextFloat() < .2 {
			s.baby = true
		} else if r.NextFloat() < .5 {
			s.main = item.NewStack(item.Crossbow{}, 1)
		} else if r.NextInt(10) == 0 {
			s.main = item.NewStack(item.Spear{Tier: item.ToolTierGold}, 1)
		} else {
			s.main = item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1)
		}
	}
	s.br.setWithExpiry(memHuntedRecently, true, int64(600+r.NextInt(1801)))
	if !s.baby {
		for i, a := range []item.Stack{item.NewStack(item.Helmet{Tier: item.ArmourTierGold{}}, 1), item.NewStack(item.Chestplate{Tier: item.ArmourTierGold{}}, 1),
			item.NewStack(item.Leggings{Tier: item.ArmourTierGold{}}, 1), item.NewStack(item.Boots{Tier: item.ArmourTierGold{}}, 1)} {
			if r.NextFloat() < .1 {
				s.armor[i] = a
			}
		}
	}
}
func (p *Piglin) HeldItems() (item.Stack, item.Stack) { return p.p().main, p.p().off }
func (p *Piglin) SetHeldItems(main, off item.Stack) {
	s := p.p()
	s.main, s.off = main, off
}
func (p *Piglin) EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack) {
	a := p.p().armor
	return a[0], a[1], a[2], a[3]
}

// Admiring reports if the piglin admires the loved item in its off hand, as
// Bedrock shows.
func (p *Piglin) Admiring() bool { return piglinLovedItem(p.p().off) }

// Celebrating reports if the piglin celebrates the death of what it fought.
func (p *Piglin) Celebrating() bool { return p.p().br.has(memCelebrateLocation) }

// CelebratingSpecial reports if the piglin dances, as Piglin.isDancing.
func (p *Piglin) CelebratingSpecial() bool { return p.p().dancing }

// Charging reports if the piglin loads its crossbow.
func (p *Piglin) Charging() bool { return p.p().chargingCrossbow }

// CrossbowCharged reports if the piglin holds a loaded crossbow.
func (p *Piglin) CrossbowCharged() bool {
	c, ok := p.p().main.Item().(item.Crossbow)
	return ok && !c.Item.Empty()
}

// CanHunt ports AbstractPiglin.canHunt: piglins hunt hoglins unless they were
// born of a hunt, brutes never.
func (p *Piglin) CanHunt() bool { s := p.p(); return !s.brute && !s.cannotHunt }

func (p *Piglin) hasEffect(t effect.Type) bool { _, ok := p.p().effects.Effect(t); return ok }

// finishConversion ports AbstractPiglin.finishConversion and Piglin's: the
// piglin drops what it carries and becomes a zombified piglin, sick for ten
// seconds.
func (s *piglinState) finishConversion(m *Mob) {
	if !s.brute {
		piglinCancelAdmiring(m, s)
		for i, st := range s.inventory {
			if !st.Empty() {
				m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.data.Pos}, st))
				s.inventory[i] = item.Stack{}
			}
		}
	}
	opts := world.EntitySpawnOpts{Position: m.data.Pos, Rotation: m.data.Rot, SpawnReason: "conversion"}
	z := m.tx.AddEntity(opts.New(ZombifiedPiglinType{}, zombifiedPiglinConfig{health: s.health.Health(), baby: s.baby, main: s.main}))
	z.(*ZombifiedPiglin).AddEffect(effect.New(effect.Nausea, 1, 10*time.Second))
	s.dead = true
	_ = m.CloseIn(m.tx)
}

// makeSound ports Mob.makeSound for piglins.
func (s *piglinState) makeSound(m *Mob, name string) {
	r := m.Random()
	pitch := float64((r.NextFloat()-r.NextFloat())*.2 + 1)
	if s.baby {
		pitch = float64((r.NextFloat()-r.NextFloat())*.2 + 1.5)
	}
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: name, Volume: 1, Pitch: pitch})
}

// piglinEyeHeight is the height of the eyes of an adult piglin.
const piglinEyeHeight = 1.79

var _ world.EntityType = PiglinType{}
var _ Living = (*Piglin)(nil)
var _ Interactable = (*Piglin)(nil)

// piglinSpeed returns the movement speed of a piglin: babies are a fifth
// faster.
func (s *piglinState) movementSpeed() float64 {
	if s.baby {
		return s.speed * 1.2
	}
	return s.speed
}

// UsingItem reports if the piglin holds its spear out.
func (p *Piglin) UsingItem() bool { return !p.p().ai.useItem.Empty() }
