package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// oceanWildlifeType is the codec shared by the four aquatic wildlife species;
// each type opens a distinct public actor and retains species-specific state.
type oceanWildlifeType struct {
	id                       string
	width, height, maxHealth float64
}

func (t oceanWildlifeType) EncodeEntity() string { return t.id }
func (t oceanWildlifeType) BBox(e world.Entity) cube.BBox {
	w, h := t.width, t.height
	if ageable, ok := e.(interface{ Age() int }); ok && ageable.Age() < 0 {
		switch t.id {
		case "minecraft:turtle":
			w, h = w*.3, h*.3
		case "minecraft:squid", "minecraft:glow_squid":
			// Squid.BABY_DIMENSIONS.
			w, h = .5, .5
		}
	}
	return cube.Box(-w/2, 0, -w/2, w/2, h, w/2)
}
func (t oceanWildlifeType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		(oceanWildlifeConfig{id: t.id, maxHealth: t.maxHealth}).Apply(d)
	}
	base := &oceanWildlife{Ent: &Ent{tx: tx, handle: h, data: d}}
	switch t.id {
	case "minecraft:squid":
		return &Squid{base}
	case "minecraft:glow_squid":
		return &GlowSquid{base}
	case "minecraft:turtle":
		return &Turtle{base}
	default:
		return &Dolphin{base}
	}
}
func (t oceanWildlifeType) DecodeNBT(m map[string]any, d *world.EntityData) {
	c := oceanWildlifeConfig{id: t.id, maxHealth: t.maxHealth}
	if _, ok := m["Health"]; ok {
		c.health = float64(nbtconv.Float32(m, "Health"))
		c.healthSet = true
	}
	c.age = int(nbtconv.Int32(m, "Age"))
	c.air = int(nbtconv.Int16(m, "Air"))
	c.darkTicks = int(nbtconv.Int32(m, "DarkTicksRemaining"))
	c.hasEgg = nbtconv.Bool(m, "has_egg")
	c.gotFish = nbtconv.Bool(m, "GotFish")
	c.hasTreasure = nbtconv.Bool(m, "HasTreasure")
	c.treasureX = int(nbtconv.Int32(m, "TreasurePosX"))
	c.treasureY = int(nbtconv.Int32(m, "TreasurePosY"))
	c.treasureZ = int(nbtconv.Int32(m, "TreasurePosZ"))
	c.moistness = int(nbtconv.Int32(m, "Moistness"))
	if t.id == "minecraft:turtle" {
		c.homeX, c.homeY, c.homeZ, c.homeSet = turtleHomePos(m["home_pos"])
	}
	c.Apply(d)
	if t.id == "minecraft:turtle" {
		d.Data.(*oceanWildlifeBehaviour).breeding.decodeNBT(m)
	}
	if equipment := ReadNativeMobEquipment(m); t.id == "minecraft:dolphin" && len(equipment.Mainhand) > 0 {
		d.Data.(*oceanWildlifeBehaviour).held = item.ReadNBT(equipment.Mainhand[0].Raw, nil)
	}
}

// turtleHomePos reads the home_pos tag of a turtle, an int array as Java's
// BlockPos codec writes, or the X, Y and Z compound older saves hold.
func turtleHomePos(v any) (x, y, z int, ok bool) {
	switch pos := v.(type) {
	case []int32:
		if len(pos) == 3 {
			return int(pos[0]), int(pos[1]), int(pos[2]), true
		}
	case map[string]any:
		return int(nbtconv.Int32(pos, "X")), int(nbtconv.Int32(pos, "Y")), int(nbtconv.Int32(pos, "Z")), true
	}
	return 0, 0, 0, false
}
func (t oceanWildlifeType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*oceanWildlifeBehaviour)
	m := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Health": float32(b.health.Health()), "Age": int32(b.age), "Air": int16(b.air)})
	if b.id == "minecraft:turtle" {
		m = MergeNativeMobNBT(m, b.breeding.encodeNBT(map[string]any{"home_pos": []int32{int32(b.homeX), int32(b.homeY), int32(b.homeZ)}, "has_egg": boolByte(b.hasEgg)}))
	}
	if b.id == "minecraft:dolphin" {
		m = MergeNativeMobNBT(m, map[string]any{"GotFish": boolByte(b.gotFish), "HasTreasure": boolByte(b.hasTreasure), "TreasurePosX": int32(b.treasureX), "TreasurePosY": int32(b.treasureY), "TreasurePosZ": int32(b.treasureZ), "Moistness": int32(b.moistness)})
		if !b.held.Empty() {
			name, damage := b.held.Item().EncodeItem()
			equipment := NativeMobEquipment{Mainhand: []NativeMobItem{{Name: name, Count: byte(b.held.Count()), Damage: int32(damage), WasPickedUp: 1, Raw: item.WriteNBT(b.held, true)}}}
			m = MergeNativeMobNBT(m, EncodeNativeMobEquipment(equipment))
		}
	}
	if b.id == "minecraft:glow_squid" {
		m = MergeNativeMobNBT(m, map[string]any{"DarkTicksRemaining": int32(b.darkTicks)})
	}
	return m
}

type oceanWildlifeConfig struct {
	id                                                                                   string
	maxHealth, health                                                                    float64
	healthSet                                                                            bool
	age, air, moistness, darkTicks, homeX, homeY, homeZ, treasureX, treasureY, treasureZ int
	hasEgg, gotFish, hasTreasure, homeSet                                                bool
}

func (c oceanWildlifeConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c oceanWildlifeConfig) New() *oceanWildlifeBehaviour {
	h := c.health
	if !c.healthSet && h <= 0 {
		h = c.maxHealth
	}
	air := c.air
	if air == 0 {
		air = 300
	}
	if c.id == "minecraft:dolphin" && c.air == 0 {
		air = 4800
	}
	moistness := c.moistness
	if c.id == "minecraft:dolphin" && moistness == 0 {
		moistness = 2400
	}
	return &oceanWildlifeBehaviour{id: c.id, health: NewHealthManager(h, c.maxHealth), effects: NewEffectManager(), speed: .25, age: c.age, air: air, moistness: moistness, darkTicks: c.darkTicks, homeX: c.homeX, homeY: c.homeY, homeZ: c.homeZ, homeSet: c.homeSet, hasEgg: c.hasEgg, gotFish: c.gotFish, hasTreasure: c.hasTreasure, treasureX: c.treasureX, treasureY: c.treasureY, treasureZ: c.treasureZ}
}

type Squid struct{ *oceanWildlife }
type GlowSquid struct{ *oceanWildlife }
type Turtle struct{ *oceanWildlife }
type Dolphin struct{ *oceanWildlife }
type oceanWildlife struct{ *Ent }
type oceanWildlifeBehaviour struct {
	id                                                                                   string
	health                                                                               *HealthManager
	effects                                                                              *EffectManager
	speed                                                                                float64
	age, air, moistness, darkTicks, homeX, homeY, homeZ, treasureX, treasureY, treasureZ int
	breeding                                                                             breeding
	hasEgg, gotFish, hasTreasure, homeSet, dead                                          bool
	// ai is the MobAI squids and dolphins run on.
	ai    MobAI
	squid squidState
	// held is the item a dolphin plays with.
	held item.Stack
	// turtle, turtleShown and turtlePanic are the state of a turtle, the
	// flags its viewers were last shown and its panic goal.
	turtle      turtleState
	turtleShown turtleShownState
	turtlePanic *PanicGoal
}

func (m *oceanWildlife) b() *oceanWildlifeBehaviour { return m.Behaviour().(*oceanWildlifeBehaviour) }
func (m *oceanWildlife) Tick(tx *world.Tx, current int64) {
	b := m.b()
	b.effects.Tick(m, tx)
	m.Ent.Tick(tx, current)
}
func (b *oceanWildlifeBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.dead {
		return nil
	}
	if b.squidKind() {
		return b.tickSquid(e, tx)
	}
	if b.id == "minecraft:dolphin" {
		return b.tickDolphin(e, tx)
	}
	return b.tickTurtle(e, tx)
}

func (m *oceanWildlife) Health() float64        { return m.b().health.Health() }
func (m *oceanWildlife) MaxHealth() float64     { return m.b().health.MaxHealth() }
func (m *oceanWildlife) SetMaxHealth(h float64) { m.b().health.SetMaxHealth(h) }
func (m *oceanWildlife) Dead() bool             { return m.b().dead || m.Health() <= 0 }
func (m *oceanWildlife) Hurt(d float64, src world.DamageSource) (float64, bool) {
	b := m.b()
	if m.Dead() || d <= 0 {
		return 0, false
	}
	var ok bool
	if d, ok = b.ai.DamageCooldown(m.tx, d); !ok {
		return 0, false
	}
	b.ai.Hurt(src)
	b.health.AddHealth(-d)
	if b.id == "minecraft:turtle" {
		resetLove(&Turtle{m})
	}
	m.viewAction(HurtAction{})
	onMobHurt(m.tx, m)
	if b.squidKind() {
		// Squid.hurtServer: a squid hurt by a mob squirts ink.
		if attacker, _ := b.ai.Mob(m.Ent, m.living()).LastHurtBy(); attacker != nil {
			name := "entity.squid.squirt"
			if b.id == "minecraft:glow_squid" {
				name = "entity.glow_squid.squirt"
			}
			m.tx.PlaySound(m.Position(), sound.Custom{Name: name, Volume: .4, Pitch: 1})
			m.viewAction(SquidInkAction{})
		}
		if b.id == "minecraft:glow_squid" {
			b.darkTicks = 100
		}
	}
	if m.Dead() {
		b.dead = true
		m.viewAction(DeathAction{})
		if !b.held.Empty() {
			// Dolphin.pickUpItem guarantees the drop of what it picked up.
			m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, b.held))
			b.held = item.Stack{}
		}
		dropAllDeathLoot(m.tx, m, src)
		_ = m.CloseIn(m.tx)
	}
	return d, true
}
func (m *oceanWildlife) Heal(h float64, _ world.HealingSource) float64 {
	if m.Dead() || h <= 0 {
		return 0
	}
	old := m.Health()
	m.b().health.AddHealth(h)
	return m.Health() - old
}
func (m *oceanWildlife) KnockBack(src mgl64.Vec3, force, height float64) {
	d := m.Position().Sub(src)
	if d.Len() != 0 {
		d = d.Normalize().Mul(force)
	}
	d[1] = height
	m.SetVelocity(d)
}
func (m *oceanWildlife) viewAction(action world.EntityAction) {
	viewers := m.tx.Viewers(m.Position())
	for _, viewer := range viewers {
		viewer.ViewEntityAction(m, action)
	}
	m.tx.ReleaseViewers(viewers)
}
func (m *oceanWildlife) AddEffect(e effect.Effect)  { m.b().effects.Add(e, m) }
func (m *oceanWildlife) RemoveEffect(t effect.Type) { m.b().effects.Remove(t, m) }
func (m *oceanWildlife) Effects() []effect.Effect   { return m.b().effects.Effects() }
func (m *oceanWildlife) AirSupply() int             { return m.b().air }
func (m *oceanWildlife) SetAirSupply(v int)         { m.b().air = v }
func (m *oceanWildlife) Age() int                   { return m.b().age }
func (m *oceanWildlife) SetAge(v int)               { m.b().age = v }
func (m *oceanWildlife) Baby() bool                 { return m.b().age < 0 }
func (m *oceanWildlife) Breathing() bool            { return m.b().air > 0 }
func (m *oceanWildlife) Speed() float64             { return m.b().speed }
func (m *oceanWildlife) SetSpeed(v float64)         { m.b().speed = v }
func (m *oceanWildlife) Interact(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if user == nil || ctx == nil {
		return false
	}
	b := m.b()
	held, _ := user.HeldItems()
	switch b.id {
	case "minecraft:turtle":
		return feedBreeder(&Turtle{m}, user, ctx)
	case "minecraft:dolphin":
		// Dolphin.mobInteract: a fish makes a baby dolphin grow up faster
		// and leads an adult to treasure.
		if !dolphinFood(held) {
			return false
		}
		tx.PlaySound(m.Position(), sound.Custom{Name: "entity.dolphin.eat", Volume: 1, Pitch: 1})
		ctx.SubtractFromCount(1)
		if b.age < 0 {
			// AgeableMob.ageUp with getSpeedUpSecondsWhenFeeding.
			b.age = min(0, b.age+int(float32(-b.age/20)*.1)*20)
		} else {
			b.gotFish = true
		}
		return true
	}
	return false
}

// dolphinFood reports if a stack is in #fishes.
func dolphinFood(s item.Stack) bool {
	if s.Empty() {
		return false
	}
	switch s.Item().(type) {
	case item.Cod, item.Salmon, item.TropicalFish, item.Pufferfish:
		return true
	}
	return false
}

func dropTurtleGrowth(e *Ent, tx *world.Tx) {
	const id = "minecraft:gameplay/turtle_grow"
	sequence, err := loot.RandomSequence(id)
	if err != nil {
		return
	}
	drops, err := loot.Generate(id, 0, loot.Context{Entity: "minecraft:turtle", Origin: e.Position(), Random: tx.LootRandomSequence(sequence, 0)})
	if err != nil {
		return
	}
	for _, stack := range drops {
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: e.Position()}, stack))
	}
}
func (m *oceanWildlife) FinalizeSpawn(_ string, _ *vanilla.Random) {
	if m.b().id == "minecraft:turtle" {
		p := cube.PosFromVec3(m.Position())
		m.b().homeX, m.b().homeY, m.b().homeZ = p.X(), p.Y(), p.Z()
		m.b().homeSet = true
	}
}
func NewSquid(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(SquidType, oceanWildlifeConfig{id: "minecraft:squid", maxHealth: 10})
}
func NewGlowSquid(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(GlowSquidType, oceanWildlifeConfig{id: "minecraft:glow_squid", maxHealth: 10})
}
func NewTurtle(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(TurtleType, oceanWildlifeConfig{id: "minecraft:turtle", maxHealth: 30})
}
func NewDolphin(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(DolphinType, oceanWildlifeConfig{id: "minecraft:dolphin", maxHealth: 10})
}
func (m *oceanWildlife) SpeciesType() world.EntityType {
	switch m.b().id {
	case "minecraft:squid":
		return SquidType
	case "minecraft:glow_squid":
		return GlowSquidType
	case "minecraft:turtle":
		return TurtleType
	default:
		return DolphinType
	}
}

var (
	SquidType     = oceanWildlifeType{id: "minecraft:squid", width: .8, height: .8, maxHealth: 10}
	GlowSquidType = oceanWildlifeType{id: "minecraft:glow_squid", width: .8, height: .8, maxHealth: 10}
	TurtleType    = oceanWildlifeType{id: "minecraft:turtle", width: 1.2, height: .4, maxHealth: 30}
	DolphinType   = oceanWildlifeType{id: "minecraft:dolphin", width: .9, height: .6, maxHealth: 10}
)

func (t *Turtle) SetHomePos(pos cube.Pos) {
	b := t.b()
	b.homeX, b.homeY, b.homeZ = pos.X(), pos.Y(), pos.Z()
	b.homeSet = true
}
func (t *Turtle) HomePos() cube.Pos { b := t.b(); return cube.Pos{b.homeX, b.homeY, b.homeZ} }

// Panicking reports whether the turtle runs from danger.
func (t *Turtle) Panicking() bool { p := t.b().turtlePanic; return p != nil && p.running }

// InLove reports whether the turtle is in love.
func (t *Turtle) InLove() bool { return t.b().breeding.InLove() }

func (t *Turtle) breedingState() *breeding { return &t.b().breeding }
func (t *Turtle) ageTicks() *int           { return &t.b().age }

// breedingFood reports whether the stack is in #turtle_food, seagrass.
func (t *Turtle) breedingFood(stack item.Stack) bool { return itemNamed(stack, "minecraft:seagrass") }

// canStartBreeding ports TurtleBreedGoal.canUse: a turtle carrying an egg
// does not look for another partner.
func (t *Turtle) canStartBreeding(*world.Tx) bool { return !t.b().hasEgg }

func (t *Turtle) breedOffspring(*world.Tx, breeder) *world.EntityHandle { return nil }

// spawnChildFromBreeding ports TurtleBreedGoal.breed: instead of a baby, the
// turtle gets an egg to lay at its home beach.
func (t *Turtle) spawnChildFromBreeding(tx *world.Tx, partner breeder) {
	t.b().hasEgg = true
	setAge(t, parentAgeAfterBreeding)
	setAge(partner, parentAgeAfterBreeding)
	resetLove(t)
	resetLove(partner)
	if tx.World().GameRuleBool("mob_drops") {
		for _, orb := range NewExperienceOrbs(t.Position(), 1+rand.IntN(7)) {
			tx.AddEntity(orb)
		}
	}
}

var _ world.EntityType = oceanWildlifeType{}
var _ Living = (*oceanWildlife)(nil)
var _ Interactable = (*oceanWildlife)(nil)
