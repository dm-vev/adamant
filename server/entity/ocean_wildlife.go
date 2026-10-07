package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
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
	if t.id == "minecraft:turtle" {
		if ageable, ok := e.(interface{ Age() int }); ok && ageable.Age() < 0 {
			w, h = w*.3, h*.3
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
	c.loveTicks = int(nbtconv.Int32(m, "InLove"))
	c.Apply(d)
}
func (t oceanWildlifeType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*oceanWildlifeBehaviour)
	m := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Health": float32(b.health.Health()), "Age": int32(b.age), "Air": int16(b.air), "InLove": int32(b.loveTicks)})
	if b.id == "minecraft:turtle" {
		m = MergeNativeMobNBT(m, map[string]any{"home_pos": map[string]any{"X": int32(b.homeX), "Y": int32(b.homeY), "Z": int32(b.homeZ)}, "has_egg": boolByte(b.hasEgg)})
	}
	if b.id == "minecraft:dolphin" {
		m = MergeNativeMobNBT(m, map[string]any{"GotFish": boolByte(b.gotFish), "HasTreasure": boolByte(b.hasTreasure), "TreasurePosX": int32(b.treasureX), "TreasurePosY": int32(b.treasureY), "TreasurePosZ": int32(b.treasureZ), "Moistness": int32(b.moistness)})
	}
	if b.id == "minecraft:glow_squid" {
		m = MergeNativeMobNBT(m, map[string]any{"DarkTicksRemaining": int32(b.darkTicks)})
	}
	return m
}

type oceanWildlifeConfig struct {
	id                                                                                              string
	maxHealth, health                                                                               float64
	healthSet                                                                                       bool
	age, air, moistness, darkTicks, homeX, homeY, homeZ, treasureX, treasureY, treasureZ, loveTicks int
	hasEgg, gotFish, hasTreasure, homeSet                                                           bool
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
	return &oceanWildlifeBehaviour{id: c.id, health: NewHealthManager(h, c.maxHealth), effects: NewEffectManager(), movement: &MovementComputer{Gravity: .02, Drag: .04, DragBeforeGravity: true}, speed: .25, age: c.age, air: air, moistness: moistness, darkTicks: c.darkTicks, homeX: c.homeX, homeY: c.homeY, homeZ: c.homeZ, homeSet: c.homeSet, hasEgg: c.hasEgg, gotFish: c.gotFish, hasTreasure: c.hasTreasure, treasureX: c.treasureX, treasureY: c.treasureY, treasureZ: c.treasureZ, loveTicks: c.loveTicks, nextWander: 1}
}

type Squid struct{ *oceanWildlife }
type GlowSquid struct{ *oceanWildlife }
type Turtle struct{ *oceanWildlife }
type Dolphin struct{ *oceanWildlife }
type oceanWildlife struct{ *Ent }
type oceanWildlifeBehaviour struct {
	id                                                                                                                                        string
	health                                                                                                                                    *HealthManager
	effects                                                                                                                                   *EffectManager
	movement                                                                                                                                  *MovementComputer
	speed                                                                                                                                     float64
	age, air, moistness, ticks, nextWander, inkCooldown, darkTicks, layTicks, homeX, homeY, homeZ, treasureX, treasureY, treasureZ, loveTicks int
	swim                                                                                                                                      mgl64.Vec3
	mate                                                                                                                                      *world.EntityHandle
	hasEgg, gotFish, hasTreasure, homeSet, dead                                                                                               bool
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
	b.ticks++
	_, inWater := tx.Liquid(cube.PosFromVec3(e.Position()))
	vel := e.data.Vel
	if b.id == "minecraft:turtle" && b.age == -1 {
		dropTurtleGrowth(e, tx)
	}
	if b.id == "minecraft:turtle" && b.loveTicks > 0 && !b.hasEgg {
		b.loveTicks--
		var partner *Turtle
		best := 16.0
		for other := range tx.Entities() {
			candidate, ok := other.(*Turtle)
			if !ok || candidate.H() == e.handle || candidate.Baby() || candidate.b().hasEgg || candidate.b().loveTicks <= 0 {
				continue
			}
			distance := candidate.Position().Sub(e.Position()).LenSqr()
			if distance < best {
				partner, best = candidate, distance
			}
		}
		if partner != nil {
			b.mate = partner.H()
			partner.b().mate = e.handle
			if best <= 4 {
				b.loveTicks = 0
				partner.b().loveTicks = 0
				b.mate = nil
				partner.b().mate = nil
				if !b.hasEgg {
					b.hasEgg = true
				}
				b.age = 6000
				partner.b().age = 6000
			}
		}
	}
	switch b.id {
	case "minecraft:squid", "minecraft:glow_squid":
		if inWater {
			if b.nextWander <= 0 {
				b.chooseSwim(e, tx)
			}
			if b.nextWander > 0 {
				b.nextWander--
			}
			if b.swim.Len() > 0 {
				vel = vel.Add(b.swim.Mul(.02))
			}
			if vel.Len() > .2 {
				vel = vel.Normalize().Mul(.2)
			}
		} else {
			vel[0] *= .9
			vel[2] *= .9
			if vel[1] > 0 {
				vel[1] -= .08
			}
		}
		if b.inkCooldown > 0 {
			b.inkCooldown--
		}
	case "minecraft:turtle":
		if !b.homeSet {
			p := cube.PosFromVec3(e.Position())
			b.homeX, b.homeY, b.homeZ = p.X(), p.Y(), p.Z()
			b.homeSet = true
		}
		if b.age < 0 {
			b.age++
		} else if b.age > 0 {
			b.age--
		}
		home := cube.Pos{b.homeX, b.homeY, b.homeZ}
		target := home.Vec3().Add(mgl64.Vec3{.5, 0, .5})
		if b.mate != nil && !b.hasEgg {
			if partner, ok := b.mate.Entity(tx); ok {
				target = partner.Position()
			} else {
				b.mate = nil
			}
		}
		if inWater {
			vel = wildlifeSteer(e, vel, target, .025*(b.speed/.25), .12*(b.speed/.25))
		} else if b.hasEgg {
			if home.Vec3().Sub(cube.PosFromVec3(e.Position()).Vec3()).Len() <= 1 {
				if _, ok := tx.Block(home.Side(cube.FaceDown)).(block.Sand); ok {
					b.layTicks++
					if b.layTicks >= 200 {
						wildlifeLayEgg(tx, home)
						b.hasEgg = false
						b.layTicks = 0
						b.loveTicks = 600
					}
				}
			} else {
				vel = wildlifeSteer(e, vel, target, .02*(b.speed/.25), .1*(b.speed/.25))
			}
		} else {
			vel[0] *= .8
			vel[2] *= .8
		}
	case "minecraft:dolphin":
		if b.nextWander > 0 {
			b.nextWander--
		}
		if b.gotFish && !b.hasTreasure {
			if target, found, _ := tx.World().FindNearestMapFeature("#minecraft:dolphin_located", cube.PosFromVec3(e.Position()), 50, false); found {
				b.treasureX, b.treasureY, b.treasureZ = target.Position.X(), target.Position.Y(), target.Position.Z()
				b.hasTreasure = true
			} else {
				b.gotFish = false
			}
		}
		if inWater {
			b.air = 300
			var playItem *Ent
			if !b.hasTreasure {
				for handle := range tx.Entities() {
					ent, ok := handle.(*Ent)
					if !ok || ent.Position().Sub(e.Position()).LenSqr() >= 64 {
						continue
					}
					if _, ok := tx.Liquid(cube.PosFromVec3(ent.Position())); !ok {
						continue
					}
					ib, ok := ent.Behaviour().(*ItemBehaviour)
					if !ok || ib.i.Empty() || ib.pickupDelay > 0 {
						continue
					}
					playItem = ent
					break
				}
			}
			if playItem != nil {
				vel = wildlifeSteer(e, vel, playItem.Position(), .07*(b.speed/.25), .32*(b.speed/.25))
				if playItem.Position().Sub(e.Position()).LenSqr() < 1 {
					d := playItem.Position().Sub(e.Position())
					if d.Len() > 0 {
						playItem.SetVelocity(d.Normalize().Mul(.3))
					}
				}
			} else if b.hasTreasure {
				goal := mgl64.Vec3{float64(b.treasureX) + .5, float64(b.treasureY), float64(b.treasureZ) + .5}
				if goal.Sub(e.Position()).LenSqr() < 16 {
					b.hasTreasure = false
					b.gotFish = false
				}
				vel = wildlifeSteer(e, vel, goal, .045*(b.speed/.25), .32*(b.speed/.25))
			} else {
				var player world.Entity
				best := 100.0
				for candidate := range tx.Players() {
					swimmer, ok := candidate.(interface{ Swimming() bool })
					if !ok || !swimmer.Swimming() {
						continue
					}
					distance := candidate.Position().Sub(e.Position()).LenSqr()
					if distance < best {
						player, best = candidate, distance
					}
				}
				if player != nil {
					vel = wildlifeSteer(e, vel, player.Position(), .025*(b.speed/.25), .3*(b.speed/.25))
				} else if b.nextWander <= 0 {
					b.chooseSwim(e, tx)
					b.nextWander = 80
				}
			}
		} else {
			b.moistness--
			if b.moistness <= 0 && b.ticks%20 == 0 {
				(&oceanWildlife{Ent: e}).Hurt(1, DrowningDamageSource{})
			}
		}
		b.air = 4800
	}
	if b.id == "minecraft:glow_squid" && b.darkTicks > 0 {
		b.darkTicks--
	}
	e.data.Vel = vel
	rot := e.data.Rot
	if vel[0]*vel[0]+vel[2]*vel[2] > .0001 {
		rot[0] = math.Atan2(-vel[0], vel[2]) * 180 / math.Pi
	}
	return b.movement.TickMovement(e, e.Position(), vel, rot, rot, tx)
}
func (b *oceanWildlifeBehaviour) chooseSwim(e *Ent, tx *world.Tx) {
	seed := int64(e.Position()[0]*341873128712+e.Position()[1]*132897987541+e.Position()[2]*42317861) + int64(b.ticks)
	r := java26.NewXoroshiroRandom(seed)
	b.nextWander = 40 + int(r.NextInt(81))
	b.swim = mgl64.Vec3{r.NextDouble()*2 - 1, r.NextDouble() - .5, r.NextDouble()*2 - 1}
	if _, ok := tx.Liquid(cube.PosFromVec3(e.Position())); !ok {
		b.swim[1] = 0
	}
	if b.swim.Len() > 0 {
		b.swim = b.swim.Normalize()
	}
}
func wildlifeSteer(e *Ent, vel, target mgl64.Vec3, accel, limit float64) mgl64.Vec3 {
	d := target.Sub(e.Position())
	if d.Len() == 0 {
		return vel
	}
	vel = vel.Add(d.Normalize().Mul(accel))
	if vel.Len() > limit {
		vel = vel.Normalize().Mul(limit)
	}
	return vel
}
func wildlifeLayEgg(tx *world.Tx, pos cube.Pos) {
	if _, ok := tx.Block(pos.Side(cube.FaceDown)).(block.Sand); !ok {
		return
	}
	egg := block.TurtleEgg{Eggs: 1}
	if current, ok := tx.Block(pos).(block.TurtleEgg); ok {
		egg = current
		if egg.Eggs < 4 {
			egg.Eggs++
		}
	} else {
		r := java26.NewXoroshiroRandom(int64(pos.X()*341873128712 + pos.Z()*132897987541))
		egg.Eggs = uint8(r.NextInt(4) + 1)
	}
	tx.SetBlock(pos, egg, nil)
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
	b.health.AddHealth(-d)
	m.viewAction(HurtAction{})
	if b.id == "minecraft:glow_squid" {
		b.darkTicks = 100
	}
	if b.id == "minecraft:squid" && b.inkCooldown == 0 {
		b.inkCooldown = 100
		v := m.Velocity()
		if v.Len() > 0 {
			m.SetVelocity(v.Add(v.Normalize().Mul(-.4)))
		}
		m.tx.PlaySound(m.Position(), sound.Custom{Name: "entity.squid.squirt", Volume: .4, Pitch: 1})
	}
	if m.Dead() {
		b.dead = true
		m.viewAction(DeathAction{})
		switch b.id {
		case "minecraft:squid":
			m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, item.NewStack(item.InkSac{}, 1)))
		case "minecraft:glow_squid":
			m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, item.NewStack(item.InkSac{Glowing: true}, 1)))
		}
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
		if _, ok := held.Item().(block.SeaGrass); !ok || held.Empty() || b.age < 0 || b.hasEgg {
			return false
		}
		ctx.SubtractFromCount(1)
		b.loveTicks = 600
		return true
	case "minecraft:dolphin":
		if held.Empty() {
			return false
		}
		food := false
		switch fish := held.Item().(type) {
		case item.Cod:
			food = !fish.Cooked
		case item.Salmon:
			food = !fish.Cooked
		case item.TropicalFish, item.Pufferfish:
			food = true
		}
		if !food {
			return false
		}
		ctx.SubtractFromCount(1)
		tx.PlaySound(m.Position(), sound.Custom{Name: "entity.dolphin.eat", Volume: 1, Pitch: 1})
		if b.age < 0 {
			b.age += max(1, -b.age/10)
			if b.age > 0 {
				b.age = 0
			}
		} else {
			b.gotFish = true
			b.hasTreasure = false
			if target, found, _ := tx.World().FindNearestMapFeature("#minecraft:dolphin_located", cube.PosFromVec3(m.Position()), 50, false); found {
				b.treasureX, b.treasureY, b.treasureZ = target.Position.X(), target.Position.Y(), target.Position.Z()
				b.hasTreasure = true
			}
		}
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
func (m *oceanWildlife) FinalizeSpawn(_ string, _ *java26.Random) {
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

var _ world.EntityType = oceanWildlifeType{}
var _ Living = (*oceanWildlife)(nil)
var _ Interactable = (*oceanWildlife)(nil)
