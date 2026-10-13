package entity

import (
	"hash/fnv"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// BeeType is the source minecraft:bee species.
var BeeType beeType

type beeType struct{}

func (beeType) EncodeEntity() string { return "minecraft:bee" }
func (beeType) BBox(world.Entity) cube.BBox {
	const halfWidth = float64(float32(.55)) / 2
	return cube.Box(-halfWidth, 0, -halfWidth, halfWidth, float64(float32(.5)), halfWidth)
}
func (beeType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		beeConfig{}.Apply(d)
	}
	b := &Bee{Ent: &Ent{tx: tx, handle: h, data: d}}
	if h != nil {
		u := h.UUID()
		sum := fnv.New64a()
		_, _ = sum.Write(u[:])
		b.state().random = java26.NewLegacyRandom(int64(sum.Sum64()))
	}
	return b
}
func (beeType) DecodeNBT(n map[string]any, d *world.EntityData) {
	c := beeConfig{health: 10}
	for _, a := range ReadNativeMobAttributes(n) {
		if a.Name == "minecraft:health" {
			c.health = a.Current
		}
	}
	j := ReadNativeMobJavaState(n)
	if j != nil {
		c.hasNectar = nbtconv.Bool(j, "HasNectar")
		c.hasStung = nbtconv.Bool(j, "HasStung")
		c.crops = int(nbtconv.Int32(j, "CropsGrownSincePollination"))
		c.noNectar = int(nbtconv.Int32(j, "TicksSincePollination"))
		c.stayOut = int(nbtconv.Int32(j, "CannotEnterHiveTicks"))
		c.anger = int(nbtconv.Int32(j, "AngerTime"))
		c.hive = decodeBeePos(j, "hive_pos")
		c.flower = decodeBeePos(j, "flower_pos")
	}
	c.Apply(d)
}
func (beeType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*beeBehaviour)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: 10, Current: h.Health(), DefaultMin: 0, DefaultMax: 10, Min: 0, Max: 10}, {Name: "minecraft:movement", Base: .3, Current: .3, DefaultMin: 0, DefaultMax: .3, Min: 0, Max: .3}, {Name: "minecraft:attack_damage", Base: 2, Current: 2, DefaultMin: 0, DefaultMax: 2, Min: 0, Max: 2}})
	m := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attrs})
	j := map[string]any{"HasNectar": byte(boolByte(b.hasNectar)), "HasStung": byte(boolByte(b.hasStung)), "CropsGrownSincePollination": int32(b.crops), "TicksSincePollination": int32(b.noNectar), "CannotEnterHiveTicks": int32(b.stayOut), "AngerTime": int32(b.anger)}
	if b.hasHive {
		j["hive_pos"] = encodeBeePos(b.hive)
	}
	if b.hasFlower {
		j["flower_pos"] = encodeBeePos(b.flower)
	}
	return MergeNativeMobJavaState(m, j)
}
func decodeBeePos(m map[string]any, key string) cube.Pos {
	p, ok := m[key].(map[string]any)
	if !ok {
		return cube.Pos{}
	}
	return cube.Pos{int(nbtconv.Int32(p, "X")), int(nbtconv.Int32(p, "Y")), int(nbtconv.Int32(p, "Z"))}
}
func encodeBeePos(p cube.Pos) map[string]any {
	return map[string]any{"X": int32(p.X()), "Y": int32(p.Y()), "Z": int32(p.Z())}
}

type beeConfig struct {
	health                                      float64
	hasNectar, hasStung                         bool
	crops, noNectar, stayOut, stingTicks, anger int
	hive, flower                                cube.Pos
}

func (c beeConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c beeConfig) New() *beeBehaviour {
	if c.health <= 0 {
		c.health = 10
	}
	return &beeBehaviour{health: NewHealthManager(c.health, 10), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .01, Drag: .08, DragBeforeGravity: true}, hasNectar: c.hasNectar, hasStung: c.hasStung, crops: c.crops, noNectar: c.noNectar, stayOut: c.stayOut, stingTicks: c.stingTicks, anger: c.anger, hive: c.hive, flower: c.flower, hasHive: c.hive != (cube.Pos{}), hasFlower: c.flower != (cube.Pos{}), random: java26.NewLegacyRandom(0), speed: .12}
}

type beeBehaviour struct {
	health                                                                                 *HealthManager
	effects                                                                                *EffectManager
	mc                                                                                     *MovementComputer
	random                                                                                 java26.Random
	hasNectar, hasStung, dead, hasHive, hasFlower                                          bool
	hive, flower                                                                           cube.Pos
	crops, noNectar, stayOut, stingTicks, anger, pollination, locateCooldown, cropCooldown int
	target                                                                                 world.Entity
	speed                                                                                  float64
}
type Bee struct{ *Ent }

func NewBee(opts world.EntitySpawnOpts) *world.EntityHandle { return opts.New(BeeType, beeConfig{}) }
func (b *Bee) SpeciesType() world.EntityType                { return BeeType }
func (b *Bee) state() *beeBehaviour                         { return b.Behaviour().(*beeBehaviour) }
func (b *Bee) Tick(tx *world.Tx, n int64)                   { b.state().effects.Tick(b, tx); b.Ent.Tick(tx, n) }
func (b *beeBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	mob := &Bee{Ent: e}
	pos := cube.PosFromVec3(e.Position())
	if b.stayOut > 0 {
		b.stayOut--
	}
	if b.locateCooldown > 0 {
		b.locateCooldown--
	}
	if b.anger > 0 {
		b.anger--
	}
	if b.hasStung {
		b.stingTicks++
		if b.stingTicks >= 1200 {
			mob.die(nil)
			return nil
		}
	}
	timeOfDay := tx.World().Time() % 24000
	night := timeOfDay >= 13000 && timeOfDay < 23000
	if b.hasNectar {
		if movement, handled := beeHiveStep(b, mob, tx, pos); handled {
			return movement
		}
	} else {
		if b.hasHive && (night || tx.RainingAt(pos) || b.noNectar > 3600) {
			if movement, handled := beeHiveStep(b, mob, tx, pos); handled {
				return movement
			}
		}
		b.noNectar++
		if !night && !tx.RainingAt(pos) {
			if (!b.hasFlower || b.noNectar > 600) && b.locateCooldown == 0 {
				if flower, ok := beeFindFlower(tx, pos, b.random); ok {
					b.flower, b.hasFlower, b.pollination = flower, true, 0
					b.locateCooldown = 200
				}
			}
			if b.hasFlower {
				if !beeFlower(tx.Block(b.flower)) {
					b.hasFlower = false
					b.pollination = 0
				} else if pos.Vec3().Sub(b.flower.Vec3()).Len() < 1.4 {
					b.pollination++
					if b.pollination >= 400 {
						b.hasNectar, b.noNectar, b.pollination = true, 0, 0
						tx.PlaySound(e.Position(), sound.Custom{Name: "entity.bee.pollinate", Volume: .4, Pitch: 1})
					}
				} else {
					return beeFly(b, e, tx, b.flower.Vec3().Add(mgl64.Vec3{.5, .5, .5}))
				}
			}
		}
	}
	if b.hasNectar && b.crops < 10 && b.cropCooldown == 0 && b.random.NextInt(30) == 0 {
		b.cropCooldown = 30
		beeGrowCrop(tx, pos, &b.crops)
	}
	if b.cropCooldown > 0 {
		b.cropCooldown--
	}
	if b.target == nil && b.anger > 0 {
		b.target = beeFindTarget(tx, e)
	}
	if b.target != nil {
		if b.anger <= 0 || b.hasStung {
			b.target = nil
		} else if delta := b.target.Position().Sub(e.Position()); delta.Len() < 1.7 {
			if victim, ok := b.target.(Living); ok {
				if _, vulnerable := victim.Hurt(2, AttackDamageSource{Attacker: mob}); vulnerable {
					b.hasStung = true
					b.stingTicks = 0
					seconds := time.Duration(10)
					if tx.LocalDifficulty(pos).Base == world.DifficultyHard {
						seconds = 18
					}
					victim.AddEffect(effect.New(effect.Poison, 0, seconds*time.Second))
				}
			}
			b.target = nil
			return nil
		} else {
			return beeFly(b, e, tx, b.target.Position())
		}
	}
	if b.hasFlower {
		return beeFly(b, e, tx, b.flower.Vec3().Add(mgl64.Vec3{.5, .5, .5}))
	}
	return beeWander(b, e, tx)
}
func beeHiveStep(b *beeBehaviour, bee *Bee, tx *world.Tx, pos cube.Pos) (*Movement, bool) {
	if b.stayOut > 0 {
		return nil, false
	}
	if b.hasHive {
		switch hive := tx.Block(b.hive).(type) {
		case interface {
			IsFull() bool
			AddBeeOccupant(map[string]any) (world.Block, bool)
		}:
			if hive.IsFull() {
				b.hasHive = false
			}
		default:
			b.hasHive = false
		}
	}
	if !b.hasHive && b.locateCooldown == 0 {
		if hive, ok := beeFindHive(tx, pos, b.random); ok {
			b.hive, b.hasHive = hive, true
			b.locateCooldown = 200
		}
	}
	if !b.hasHive {
		return nil, false
	}
	if pos.Vec3().Sub(b.hive.Vec3()).Len() <= 2 {
		if hive, ok := tx.Block(b.hive).(interface {
			IsFull() bool
			AddBeeOccupant(map[string]any) (world.Block, bool)
		}); ok && !hive.IsFull() {
			next, added := hive.AddBeeOccupant(BeeType.EncodeNBT(bee.data))
			if added {
				tx.SetBlock(b.hive, next, nil)
				tx.ScheduleBlockUpdate(b.hive, next, time.Second/20)
				tx.PlaySound(b.hive.Vec3Centre(), sound.Custom{Name: "block.beehive.enter", Volume: 1, Pitch: 1})
				_ = bee.CloseIn(tx)
				return nil, true
			}
		}
		b.hasHive = false
		return nil, false
	}
	return beeFly(b, bee.Ent, tx, b.hive.Vec3().Add(mgl64.Vec3{.5, .5, .5})), true
}
func beeFly(b *beeBehaviour, e *Ent, tx *world.Tx, target mgl64.Vec3) *Movement {
	d := target.Sub(e.Position())
	if d.Len() > 0 {
		d = d.Normalize()
	}
	v := e.Velocity()
	v[0] += d[0] * .06
	v[1] += d[1] * .035
	v[2] += d[2] * .06
	if v.Len() > .35 {
		v = v.Normalize().Mul(.35)
	}
	r := e.Rotation()
	if d.Len() > 0 {
		r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	}
	m := b.mc.TickMovement(e, e.Position(), v, e.Rotation(), r, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func beeWander(b *beeBehaviour, e *Ent, tx *world.Tx) *Movement {
	if b.pollination == 0 || b.random.NextInt(50) == 0 {
		b.pollination = 1
		b.flower = cube.PosFromVec3(e.Position()).Add(cube.Pos{int(b.random.NextInt(17)) - 8, int(b.random.NextInt(9)) - 4, int(b.random.NextInt(17)) - 8})
		b.hasFlower = false
	}
	return beeFly(b, e, tx, b.flower.Vec3().Add(mgl64.Vec3{.5, .5, .5}))
}
func beeFlower(v world.Block) bool {
	switch f := v.(type) {
	case block.Flower, block.Torchflower, block.PinkPetals, block.Wildflowers, block.FloweringAzalea:
		return true
	case block.DoubleFlower:
		return f.UpperPart
	}
	return false
}
func beeFindFlower(tx *world.Tx, p cube.Pos, r java26.Random) (cube.Pos, bool) {
	for range 48 {
		q := p.Add(cube.Pos{int(r.NextInt(41)) - 20, int(r.NextInt(17)) - 8, int(r.NextInt(41)) - 20})
		if beeFlower(tx.Block(q)) {
			return q, true
		}
	}
	return cube.Pos{}, false
}
func beeFindHive(tx *world.Tx, p cube.Pos, r java26.Random) (cube.Pos, bool) {
	for range 64 {
		q := p.Add(cube.Pos{int(r.NextInt(41)) - 20, int(r.NextInt(17)) - 8, int(r.NextInt(41)) - 20})
		switch h := tx.Block(q).(type) {
		case interface{ IsFull() bool }:
			if !h.IsFull() {
				return q, true
			}
		}
	}
	return cube.Pos{}, false
}
func beeGrowCrop(tx *world.Tx, p cube.Pos, count *int) {
	for i := 1; i <= 2; i++ {
		q := p.Add(cube.Pos{0, -i, 0})
		switch c := tx.Block(q).(type) {
		case block.WheatSeeds:
			if c.Growth < 7 {
				c.Growth++
				tx.SetBlock(q, c, nil)
				*count++
				return
			}
		case block.Potato:
			if c.Growth < 7 {
				c.Growth++
				tx.SetBlock(q, c, nil)
				*count++
				return
			}
		case block.Carrot:
			if c.Growth < 7 {
				c.Growth++
				tx.SetBlock(q, c, nil)
				*count++
				return
			}
		case block.BeetrootSeeds:
			if c.Growth < 3 {
				c.Growth++
				tx.SetBlock(q, c, nil)
				*count++
				return
			}
		}
	}
}
func beeFindTarget(tx *world.Tx, e *Ent) world.Entity {
	area := cube.Box(-16, -8, -16, 16, 8, 16).Translate(e.Position())
	var target world.Entity
	best := 256.0
	for other := range tx.EntitiesWithin(area) {
		if other == e || other.H().Type().EncodeEntity() != "minecraft:player" {
			continue
		}
		d := other.Position().Sub(e.Position()).LenSqr()
		if d < best {
			target, best = other, d
		}
	}
	return target
}
func (b *Bee) Attack(attacker world.Entity, tx *world.Tx) {
	if attacker == nil {
		return
	}
	area := cube.Box(-16, -8, -16, 16, 8, 16).Translate(b.Position())
	for other := range tx.EntitiesWithin(area) {
		if ally, ok := other.(*Bee); ok && ally != b && !ally.state().hasStung {
			ally.state().anger = 400 + int(ally.state().random.NextInt(380))
			ally.state().target = attacker
		}
	}
	b.state().anger = 400 + int(b.state().random.NextInt(380))
	b.state().target = attacker
	b.Ent.Attack(attacker, tx)
}
func (b *Bee) Health() float64        { return b.state().health.Health() }
func (b *Bee) MaxHealth() float64     { return b.state().health.MaxHealth() }
func (b *Bee) SetMaxHealth(v float64) { b.state().health.SetMaxHealth(v) }
func (b *Bee) Dead() bool             { return b.state().dead || b.Health() <= 0 }
func (b *Bee) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if b.Dead() || d <= 0 {
		return 0, false
	}
	b.state().health.AddHealth(-d)
	if b.Dead() {
		b.die(src)
	} else {
		b.viewAction(HurtAction{})
	}
	return d, true
}
func (b *Bee) Heal(v float64, _ world.HealingSource) float64 {
	if b.Dead() || v <= 0 {
		return 0
	}
	old := b.Health()
	b.state().health.AddHealth(v)
	return b.Health() - old
}
func (b *Bee) KnockBack(src mgl64.Vec3, f, h float64) {
	d := b.Position().Sub(src)
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	b.SetVelocity(d)
}
func (b *Bee) AddEffect(e effect.Effect)  { b.state().effects.Add(e, b) }
func (b *Bee) RemoveEffect(t effect.Type) { b.state().effects.Remove(t, b) }
func (b *Bee) Effects() []effect.Effect   { return b.state().effects.Effects() }
func (b *Bee) Speed() float64             { return b.state().speed }
func (b *Bee) SetSpeed(v float64)         { b.state().speed = v }
func (b *Bee) viewAction(a world.EntityAction) {
	vs := b.tx.Viewers(b.Position())
	for _, v := range vs {
		v.ViewEntityAction(b, a)
	}
	b.tx.ReleaseViewers(vs)
}
func (b *Bee) die(src world.DamageSource) {
	s := b.state()
	if s.dead {
		return
	}
	s.dead = true
	b.viewAction(DeathAction{})
	dropAllDeathLoot(b.tx, b, src)
	_ = b.CloseIn(b.tx)
}

var _ world.EntityType = beeType{}
var _ Living = (*Bee)(nil)
