package entity

import (
	"encoding/binary"
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

const (
	ironGolemHealth = 100.0
	snowGolemHealth = 4.0
)

type golemKind uint8

const (
	ironKind golemKind = iota
	snowKind
)

type golemState struct {
	kind                         golemKind
	health                       *HealthManager
	effects                      *EffectManager
	mc                           *MovementComputer
	speed                        float64
	dead, playerCreated, pumpkin bool
	target                       *world.EntityHandle
	cooldown, wander, flower     int
	angerTarget                  string
	angerTicks                   int
	offerTarget                  string
	path                         []cube.Pos
	pathIndex                    int
}
type golemConfig struct {
	kind                              golemKind
	health                            float64
	healthSet, playerCreated, pumpkin bool
	angerTarget                       string
	angerTicks                        int
}

func (c golemConfig) Apply(d *world.EntityData) {
	h := c.health
	maxH := ironGolemHealth
	speed := .25
	if c.kind == snowKind {
		maxH = snowGolemHealth
		speed = .2
	}
	if !c.healthSet {
		h = maxH
	}
	d.Data = &golemState{kind: c.kind, health: NewHealthManager(h, maxH), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, speed: speed, playerCreated: c.playerCreated, pumpkin: c.pumpkin, angerTarget: c.angerTarget, angerTicks: c.angerTicks}
}

func NewIronGolem(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(IronGolemType, golemConfig{kind: ironKind})
}
func NewSnowGolem(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(SnowGolemType, golemConfig{kind: snowKind, pumpkin: true})
}

var IronGolemType ironGolemType
var SnowGolemType snowGolemType

type ironGolemType struct{}
type snowGolemType struct{}

func (ironGolemType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		golemConfig{kind: ironKind}.Apply(d)
	}
	return &IronGolem{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (snowGolemType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		golemConfig{kind: snowKind, pumpkin: true}.Apply(d)
	}
	return &SnowGolem{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (ironGolemType) EncodeEntity() string        { return "minecraft:iron_golem" }
func (snowGolemType) EncodeEntity() string        { return "minecraft:snow_golem" }
func (ironGolemType) BBox(world.Entity) cube.BBox { return cube.Box(-.7, 0, -.7, .7, 2.7, .7) }
func (snowGolemType) BBox(world.Entity) cube.BBox { return cube.Box(-.35, 0, -.35, .35, 1.9, .35) }
func (ironGolemType) DecodeNBT(n map[string]any, d *world.EntityData) {
	source := ReadNativeMobJavaState(n)
	if source == nil {
		source = n
	}
	c := golemConfig{kind: ironKind}
	if _, ok := source["Health"]; ok {
		c.health = float64(nbtconv.Float32(source, "Health"))
		c.healthSet = true
	}
	if health, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health, c.healthSet = health.Current, true
	}
	c.playerCreated = nbtconv.Bool(source, "PlayerCreated")
	c.angerTarget = golemUUIDString(source["AngryAt"])
	c.angerTicks = int(nbtconv.Int32(source, "AngerTime"))
	c.Apply(d)
}
func (snowGolemType) DecodeNBT(n map[string]any, d *world.EntityData) {
	source := ReadNativeMobJavaState(n)
	if source == nil {
		source = n
	}
	c := golemConfig{kind: snowKind, pumpkin: true}
	if _, ok := source["Health"]; ok {
		c.health = float64(nbtconv.Float32(source, "Health"))
		c.healthSet = true
	}
	if health, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health, c.healthSet = health.Current, true
	}
	if _, ok := source["Pumpkin"]; ok {
		c.pumpkin = nbtconv.Bool(source, "Pumpkin")
	}
	c.Apply(d)
}
func (ironGolemType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*golemState)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{
		{Name: "minecraft:health", Base: ironGolemHealth, Current: h.Health(), DefaultMin: 0, DefaultMax: ironGolemHealth, Min: 0, Max: ironGolemHealth},
	})
	javaState := map[string]any{"Health": float32(h.Health()), "PlayerCreated": byte(boolByte(b.playerCreated)),
		"AngerTime": int32(b.angerTicks)}
	target := golemUUIDNBT(b.angerTarget)
	if target != nil && b.angerTicks > 0 {
		javaState["AngryAt"] = target
	}
	native := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Health": float32(h.Health()), "Attributes": attrs,
		"PlayerCreated": byte(boolByte(b.playerCreated)), "IsAngry": byte(boolByte(b.angerTicks > 0))})
	state := MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), javaState)
	if target == nil || b.angerTicks <= 0 {
		delete(state, "AngryAt")
	}
	return MergeNativeMobJavaState(native, state)
}
func (snowGolemType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*golemState)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{
		{Name: "minecraft:health", Base: snowGolemHealth, Current: h.Health(), DefaultMin: 0, DefaultMax: snowGolemHealth, Min: 0, Max: snowGolemHealth},
	})
	native := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Health": float32(h.Health()), "Attributes": attrs,
		"Pumpkin": byte(boolByte(b.pumpkin))})
	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), map[string]any{"Health": float32(h.Health()), "Pumpkin": byte(boolByte(b.pumpkin))})
	return MergeNativeMobJavaState(native, javaState)
}

func golemUUIDNBT(raw string) []int32 {
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return []int32{int32(binary.BigEndian.Uint32(id[0:4])), int32(binary.BigEndian.Uint32(id[4:8])), int32(binary.BigEndian.Uint32(id[8:12])), int32(binary.BigEndian.Uint32(id[12:16]))}
}

func golemUUIDString(raw any) string {
	parts, ok := raw.([]int32)
	if !ok || len(parts) != 4 {
		if text, ok := raw.(string); ok {
			id, err := uuid.Parse(text)
			if err == nil {
				return id.String()
			}
		}
		return ""
	}
	var bytes [16]byte
	for i, part := range parts {
		binary.BigEndian.PutUint32(bytes[i*4:], uint32(part))
	}
	id, err := uuid.FromBytes(bytes[:])
	if err != nil {
		return ""
	}
	return id.String()
}

type IronGolem struct{ *Ent }
type SnowGolem struct{ *Ent }

func (m *IronGolem) b() *golemState             { return m.Behaviour().(*golemState) }
func (m *SnowGolem) b() *golemState             { return m.Behaviour().(*golemState) }
func (m *IronGolem) Tick(tx *world.Tx, n int64) { m.b().effects.Tick(m, tx); m.Ent.Tick(tx, n) }
func (m *SnowGolem) Tick(tx *world.Tx, n int64) { m.b().effects.Tick(m, tx); m.Ent.Tick(tx, n) }
func (b *golemState) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.cooldown > 0 {
		b.cooldown--
	}
	if b.angerTicks > 0 {
		b.angerTicks--
	}
	if b.flower > 0 {
		b.flower--
		if b.flower == 0 {
			b.finishFlowerOffer(tx, e)
		}
		return b.stop(e, tx)
	}
	if b.kind == snowKind {
		if tx.Temperature(cube.PosFromVec3(e.Position())) > 1.0 {
			hurtGolem(e, b, 1, golemFireDamage{})
		}
		if _, wet := tx.Liquid(cube.PosFromVec3(e.Position())); wet {
			hurtGolem(e, b, 1, golemWaterDamage{})
		}
	}
	target := golemTarget(tx, e, b)
	if target == nil {
		if b.kind == ironKind && b.offerFlowerToVillager(tx, e) {
			return b.stop(e, tx)
		}
		movement := b.wanderTick(e, tx)
		if b.kind == snowKind {
			b.snowTrail(e, tx)
		}
		return movement
	}

	d := target.Position().Sub(e.Position())
	dist := d.Len()
	var movement *Movement
	if b.kind == ironKind {
		if dist < 2.8 && b.cooldown == 0 {
			if l, ok := target.(Living); ok {
				dmg := 7.5 + float64(rand.IntN(15))
				l.Hurt(dmg, AttackDamageSource{Attacker: e})
				l.KnockBack(e.Position(), .4, .4)
			}
			b.cooldown = 20
			movement = b.stop(e, tx)
		} else if dist > 1.8 {
			movement = b.move(e, tx, target.Position())
		} else {
			movement = b.stop(e, tx)
		}
	} else {
		if dist < 12 && b.cooldown == 0 {
			b.snowball(e, tx, target)
			b.cooldown = 20
		}
		if dist < 5 {
			away := e.Position().Sub(target.Position())
			away[1] = 0
			if away.Len() > 0 {
				movement = b.move(e, tx, e.Position().Add(away.Normalize().Mul(5)))
			} else {
				movement = b.stop(e, tx)
			}
		} else {
			movement = b.move(e, tx, target.Position())
		}
	}
	if b.kind == snowKind {
		b.snowTrail(e, tx)
	}
	return movement
}
func golemTarget(tx *world.Tx, e *Ent, b *golemState) world.Entity {
	if b.kind == ironKind {
		if target := golemVillageThreat(tx, e); target != nil {
			return target
		}
	}
	var best world.Entity
	dist := math.MaxFloat64
	for other := range tx.Entities() {
		if other == e {
			continue
		}
		living, ok := other.(Living)
		if !ok || living.Dead() {
			continue
		}
		id := other.H().Type().EncodeEntity()
		limit := 10.0
		if b.kind == ironKind && hostileGolemTarget(other) {
			limit = 5
		}
		if other.Position().Sub(e.Position()).Len() > limit {
			continue
		}
		if b.kind == ironKind {
			if id == "minecraft:creeper" {
				continue
			}
			if _, isUser := other.(item.User); isUser {
				if b.playerCreated || b.angerTicks <= 0 || id != "minecraft:player" || other.H().UUID().String() != b.angerTarget {
					continue
				}
			} else if !hostileGolemTarget(other) {
				continue
			}
		} else if !hostileGolemTarget(other) {
			continue
		}
		d := other.Position().Sub(e.Position()).LenSqr()
		if d < dist {
			best, dist = other, d
		}
	}
	return best
}
func golemVillageThreat(tx *world.Tx, e *Ent) world.Entity {
	var best world.Entity
	distance := 64.0 * 64
	for villagerEntity := range tx.Entities() {
		villager, ok := villagerEntity.(*Villager)
		if !ok {
			continue
		}
		d := villager.Position().Sub(e.Position())
		if math.Abs(d[0]) > 10 || math.Abs(d[1]) > 8 || math.Abs(d[2]) > 10 {
			continue
		}
		for candidate := range tx.Entities() {
			gameMode, hasGameMode := candidate.(interface{ GameMode() world.GameMode })
			if candidate.H().Type().EncodeEntity() != "minecraft:player" || !hasGameMode || !gameMode.GameMode().AllowsTakingDamage() || villager.Reputation(candidate) > -100 {
				continue
			}
			distanceSquared := candidate.Position().Sub(e.Position()).LenSqr()
			if distanceSquared < distance {
				best, distance = candidate, distanceSquared
			}
		}
	}
	return best
}
func hostileGolemTarget(e world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:zombie", "minecraft:zombie_villager", "minecraft:skeleton", "minecraft:wither_skeleton", "minecraft:spider", "minecraft:witch", "minecraft:evoker", "minecraft:vindicator", "minecraft:pillager", "minecraft:vex", "minecraft:ravager", "minecraft:phantom", "minecraft:drowned", "minecraft:husk", "minecraft:stray", "minecraft:bogged", "minecraft:creeper", "minecraft:guardian", "minecraft:elder_guardian", "minecraft:blaze":
		return true
	}
	return false
}
func (b *golemState) move(e *Ent, tx *world.Tx, p mgl64.Vec3) *Movement {
	d := p.Sub(e.Position())
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize()
	}
	v := e.Velocity()
	v[0], v[2] = d[0]*b.speed, d[2]*b.speed
	r := e.Rotation()
	r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	m := b.mc.TickMovement(e, e.Position(), v, e.Rotation(), r, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (b *golemState) offerFlowerToVillager(tx *world.Tx, e *Ent) bool {
	if b.flower > 0 || rand.IntN(8000) != 0 {
		return false
	}
	dayTime := int(tx.World().Time()) % 24000
	if dayTime >= 12000 || tx.SkyLight(cube.PosFromVec3(e.Position())) < 12 {
		return false
	}
	var candidate world.Entity
	distance := 36.0
	for other := range tx.Entities() {
		eligible := false
		if villager, ok := other.(*Villager); ok {
			eligible = villager.IsBaby()
		}
		if copper, ok := other.(*CopperGolem); ok {
			eligible = copper.Antenna().Empty()
		}
		d := other.Position().Sub(e.Position()).LenSqr()
		if eligible && d < distance {
			candidate, distance = other, d
		}
	}
	if candidate == nil {
		return false
	}
	b.offerTarget = candidate.H().UUID().String()
	b.flower = 400
	tx.PlayEntityAnimation(e, world.NewEntityAnimation("animation.iron_golem.offer_flower"))
	return true
}

func (b *golemState) finishFlowerOffer(tx *world.Tx, e *Ent) {
	defer func() { b.offerTarget = "" }()
	for other := range tx.Entities() {
		if other.H().UUID().String() != b.offerTarget || other.Position().Sub(e.Position()).LenSqr() > 64 {
			continue
		}
		if copper, ok := other.(*CopperGolem); ok && copper.Antenna().Empty() {
			copper.SetAntenna(item.NewStack(block.Flower{Type: block.Poppy()}, 1))
		}
		return
	}
}

// OfferFlower reports whether the golem is currently offering a flower.
func (m *IronGolem) OfferFlower() bool { return m.b().flower > 0 }
func (b *golemState) stop(e *Ent, tx *world.Tx) *Movement {
	v := e.Velocity()
	v[0], v[2] = 0, 0
	m := b.mc.TickMovement(e, e.Position(), v, e.Rotation(), e.Rotation(), tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (b *golemState) wanderTick(e *Ent, tx *world.Tx) *Movement {
	if b.wander > 0 {
		b.wander--
	}
	start := cube.PosFromVec3(e.Position())
	if b.kind == ironKind {
		inVillage := tx.IsVillage(start)
		if !inVillage && (len(b.path) == 0 || b.pathIndex >= len(b.path)) {
			if b.wander == 0 {
				if target, ok := golemVillagePOI(tx, start, true); ok {
					b.path = findPath(tx, start, target, 100, false, false)
					b.pathIndex = 0
					b.wander = 10
				}
			}
		} else if inVillage && b.pathIndex >= len(b.path) && b.wander == 0 {
			var target cube.Pos
			found := false
			if rand.Float64() < .3 {
				target = start.Add(cube.Pos{rand.IntN(21) - 10, rand.IntN(15) - 7, rand.IntN(21) - 10})
				found = true
			} else {
				villagerFirst := rand.Float64() < .7
				if villagerFirst {
					target, found = golemVillagerTarget(tx, start)
				} else {
					target, found = golemVillagePOI(tx, start, false)
				}
				if !found {
					if villagerFirst {
						target, found = golemVillagePOI(tx, start, false)
					} else {
						target, found = golemVillagerTarget(tx, start)
					}
				}
			}
			if found {
				b.path = findPath(tx, start, target, 100, false, false)
				b.pathIndex = 0
				b.wander = 240
			}
		}
	}
	if b.pathIndex >= len(b.path) && b.wander == 0 {
		b.wander = 60
		g := start.Add(cube.Pos{rand.IntN(13) - 6, 0, rand.IntN(13) - 6})
		b.path = findPath(tx, start, g, 100, false, false)
		b.pathIndex = 0
	}
	if b.pathIndex < len(b.path) {
		p := b.path[b.pathIndex].Vec3().Add(mgl64.Vec3{.5, 0, .5})
		if p.Sub(e.Position()).Len() < .4 {
			b.pathIndex++
		} else {
			return b.move(e, tx, p)
		}
	}
	return b.stop(e, tx)
}
func golemVillagePOI(tx *world.Tx, start cube.Pos, returning bool) (cube.Pos, bool) {
	sections := tx.VillageSections(start, 2)
	if len(sections) == 0 {
		return cube.Pos{}, false
	}
	if returning {
		best := cube.Pos{}
		bestDistance := math.MaxFloat64
		for _, section := range sections {
			center := section.Add(cube.Pos{8, 8, 8})
			d := center.Vec3Middle().Sub(start.Vec3Middle()).LenSqr()
			if d < bestDistance {
				best, bestDistance = center, d
			}
		}
		return best.Add(cube.Pos{rand.IntN(21) - 10, rand.IntN(15) - 7, rand.IntN(21) - 10}), true
	}
	section := sections[rand.IntN(len(sections))]
	center := section.Add(cube.Pos{8, 8, 8})
	home, _, hasHome := tx.NearestPOI(center, world.Home, 8, nil)
	meeting, _, hasMeeting := tx.NearestPOI(center, world.Meeting, 8, nil)
	workstation, _, hasWorkstation := tx.NearestPOI(center, world.Workstation, 8, nil)
	count := 0
	if hasHome {
		count++
	}
	if hasMeeting {
		count++
	}
	if hasWorkstation {
		count++
	}
	if count == 0 {
		return cube.Pos{}, false
	}
	selected := rand.IntN(count)
	var target cube.Pos
	if hasHome {
		if selected == 0 {
			target = home
		}
		selected--
	}
	if selected >= 0 && hasMeeting {
		if selected == 0 {
			target = meeting
		}
		selected--
	}
	if selected >= 0 && hasWorkstation && selected == 0 {
		target = workstation
	}
	return target.Add(cube.Pos{rand.IntN(21) - 10, rand.IntN(15) - 7, rand.IntN(21) - 10}), true
}
func golemVillagerTarget(tx *world.Tx, start cube.Pos) (cube.Pos, bool) {
	var chosen world.Entity
	count := 0
	tick := int64(tx.World().Time())
	for candidate := range tx.Entities() {
		villager, ok := candidate.(*Villager)
		if !ok || !villager.WantsToSpawnGolem(tick) {
			continue
		}
		d := villager.Position().Sub(start.Vec3Middle())
		if math.Abs(d[0]) > 32 || math.Abs(d[1]) > 32 || math.Abs(d[2]) > 32 {
			continue
		}
		count++
		if rand.IntN(count) == 0 {
			chosen = candidate
		}
	}
	if chosen == nil {
		return cube.Pos{}, false
	}
	return cube.PosFromVec3(chosen.Position()).Add(cube.Pos{rand.IntN(21) - 10, rand.IntN(15) - 7, rand.IntN(21) - 10}), true
}
func (b *golemState) snowball(e *Ent, tx *world.Tx, target world.Entity) {
	d := target.Position().Add(mgl64.Vec3{0, .8, 0}).Sub(e.Position().Add(mgl64.Vec3{0, 1.5, 0}))
	if d.Len() == 0 {
		return
	}
	d = d.Normalize().Mul(1.6)
	tx.AddEntity(newGolemSnowball(world.EntitySpawnOpts{Position: e.Position().Add(mgl64.Vec3{0, 1.5, 0}), Velocity: d}, e))
	tx.PlaySound(e.Position(), sound.Custom{Name: "entity.snow_golem.shoot", Volume: 1, Pitch: 1})
}
func newGolemSnowball(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	conf := snowballConf
	conf.Owner = owner.H()
	conf.Hit = func(projectile *Ent, tx *world.Tx, result trace.Result) {
		hit, ok := result.(trace.EntityResult)
		if !ok || hit.Entity().H().Type().EncodeEntity() != "minecraft:blaze" {
			return
		}
		attacker, _ := owner.H().Entity(tx)
		if living, ok := hit.Entity().(Living); ok {
			living.Hurt(3, ProjectileDamageSource{Projectile: projectile, Owner: attacker})
		}
	}
	return opts.New(SnowballType, conf)
}
func (b *golemState) snowTrail(e *Ent, tx *world.Tx) {
	p := cube.PosFromVec3(e.Position())
	if _, ok := tx.Block(p).(block.Air); ok {
		tx.SetBlock(p, block.SnowLayer{Layers: 1}, nil)
	}
}
func hurtGolem(e *Ent, b *golemState, d float64, src world.DamageSource) {
	m := golemLiving{Ent: e, b: b}
	m.Hurt(d, src)
}

type golemWaterDamage struct{}
type golemFireDamage struct{}

func (golemFireDamage) ReducedByArmour() bool      { return false }
func (golemFireDamage) ReducedByResistance() bool  { return true }
func (golemFireDamage) Fire() bool                 { return true }
func (golemFireDamage) IgnoreTotem() bool          { return false }
func (golemWaterDamage) ReducedByArmour() bool     { return false }
func (golemWaterDamage) ReducedByResistance() bool { return false }
func (golemWaterDamage) Fire() bool                { return false }
func (golemWaterDamage) IgnoreTotem() bool         { return false }

type golemLiving struct {
	*Ent
	b *golemState
}

func (m golemLiving) Health() float64        { return m.b.health.Health() }
func (m golemLiving) MaxHealth() float64     { return m.b.health.MaxHealth() }
func (m golemLiving) SetMaxHealth(h float64) { m.b.health.SetMaxHealth(h) }
func (m golemLiving) Dead() bool             { return m.b.dead || m.Health() <= 0 }
func (m golemLiving) Hurt(d float64, s world.DamageSource) (float64, bool) {
	if m.Dead() || d <= 0 {
		return 0, false
	}
	if r, ok := m.b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	m.b.health.AddHealth(-d)
	m.viewAction(HurtAction{})
	if m.Dead() {
		m.b.dead = true
		m.dropLoot()
		m.viewAction(DeathAction{})
		_ = m.CloseIn(m.tx)
	}
	return d, true
}
func (m golemLiving) dropLoot() {
	tx := m.tx
	pos := m.Position()
	drop := func(it world.Item, n int) {
		if n > 0 {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(it, n)))
		}
	}
	if m.b.kind == ironKind {
		drop(item.IronIngot{}, 3+rand.IntN(3))
		drop(block.Flower{Type: block.Poppy()}, rand.IntN(3))
	} else {
		drop(item.Snowball{}, rand.IntN(16))
	}
}
func (m golemLiving) Heal(h float64, _ world.HealingSource) float64 {
	if h <= 0 || m.Dead() {
		return 0
	}
	old := m.Health()
	m.b.health.AddHealth(h)
	return m.Health() - old
}
func (m golemLiving) KnockBack(src mgl64.Vec3, f, h float64) {
	d := m.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	m.SetVelocity(d)
}
func (m golemLiving) AddEffect(e effect.Effect)  { m.b.effects.Add(e, &m) }
func (m golemLiving) RemoveEffect(t effect.Type) { m.b.effects.Remove(t, &m) }
func (m golemLiving) Effects() []effect.Effect   { return m.b.effects.Effects() }
func (m golemLiving) Speed() float64             { return m.b.speed }
func (m golemLiving) SetSpeed(s float64)         { m.b.speed = s }
func (m golemLiving) viewAction(a world.EntityAction) {
	v := m.tx.Viewers(m.Position())
	for _, x := range v {
		x.ViewEntityAction(m, a)
	}
	m.tx.ReleaseViewers(v)
}
func (m *IronGolem) Health() float64        { return m.b().health.Health() }
func (m *SnowGolem) Health() float64        { return m.b().health.Health() }
func (m *IronGolem) MaxHealth() float64     { return m.b().health.MaxHealth() }
func (m *SnowGolem) MaxHealth() float64     { return m.b().health.MaxHealth() }
func (m *IronGolem) SetMaxHealth(h float64) { m.b().health.SetMaxHealth(h) }
func (m *SnowGolem) SetMaxHealth(h float64) { m.b().health.SetMaxHealth(h) }
func (m *IronGolem) Dead() bool             { return m.b().dead || m.Health() <= 0 }
func (m *SnowGolem) Dead() bool             { return m.b().dead || m.Health() <= 0 }
func (m *IronGolem) Hurt(d float64, s world.DamageSource) (float64, bool) {
	n, ok := golemLiving{m.Ent, m.b()}.Hurt(d, s)
	if ok {
		var attacker world.Entity
		switch source := s.(type) {
		case AttackDamageSource:
			attacker = source.Attacker
		case ProjectileDamageSource:
			attacker = source.Owner
		}
		if attacker != nil {
			m.b().angerTarget = attacker.H().UUID().String()
			m.b().angerTicks = 400 + rand.IntN(20)*20
		}
	}
	return n, ok
}
func (m *SnowGolem) Hurt(d float64, s world.DamageSource) (float64, bool) {
	return golemLiving{m.Ent, m.b()}.Hurt(d, s)
}
func (m *IronGolem) Heal(h float64, s world.HealingSource) float64 {
	return golemLiving{m.Ent, m.b()}.Heal(h, s)
}
func (m *SnowGolem) Heal(h float64, s world.HealingSource) float64 {
	return golemLiving{m.Ent, m.b()}.Heal(h, s)
}
func (m *IronGolem) KnockBack(p mgl64.Vec3, f, h float64) {
	golemLiving{m.Ent, m.b()}.KnockBack(p, f, h)
}
func (m *SnowGolem) KnockBack(p mgl64.Vec3, f, h float64) {
	golemLiving{m.Ent, m.b()}.KnockBack(p, f, h)
}
func (m *IronGolem) AddEffect(e effect.Effect)  { m.b().effects.Add(e, m) }
func (m *SnowGolem) AddEffect(e effect.Effect)  { m.b().effects.Add(e, m) }
func (m *IronGolem) RemoveEffect(t effect.Type) { m.b().effects.Remove(t, m) }
func (m *SnowGolem) RemoveEffect(t effect.Type) { m.b().effects.Remove(t, m) }
func (m *IronGolem) Effects() []effect.Effect   { return m.b().effects.Effects() }
func (m *SnowGolem) Effects() []effect.Effect   { return m.b().effects.Effects() }
func (m *IronGolem) Speed() float64             { return m.b().speed }
func (m *SnowGolem) Speed() float64             { return m.b().speed }
func (m *IronGolem) SetSpeed(v float64)         { m.b().speed = v }
func (m *SnowGolem) SetSpeed(v float64)         { m.b().speed = v }
func (m *IronGolem) PlayerCreated() bool        { return m.b().playerCreated }
func (m *IronGolem) SetPlayerCreated(v bool)    { m.b().playerCreated = v }
func (m *SnowGolem) HasPumpkin() bool           { return m.b().pumpkin }
func (m *SnowGolem) SetPumpkin(v bool)          { m.b().pumpkin = v }
func (m *IronGolem) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil {
		return false
	}
	main, _ := u.HeldItems()
	if _, ok := main.Item().(item.IronIngot); !ok || main.Empty() {
		return false
	}
	if m.Health() >= m.MaxHealth() {
		return false
	}
	m.Heal(25, nil)
	ctx.SubtractFromCount(1)
	tx.PlaySound(m.Position(), sound.Custom{Name: "entity.iron_golem.repair", Volume: 1, Pitch: 1})
	return true
}
func (m *SnowGolem) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if !m.HasPumpkin() || u == nil {
		return false
	}
	main, _ := u.HeldItems()
	if _, ok := main.Item().(item.Shears); !ok || main.Empty() {
		return false
	}
	m.SetPumpkin(false)
	ctx.DamageItem(1)
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, item.NewStack(block.Pumpkin{Carved: true}, 1)))
	tx.PlaySound(m.Position(), sound.Custom{Name: "entity.snow_golem.shear", Volume: 1, Pitch: 1})
	return true
}
func (m *SnowGolem) RequiresCustomPersistence() bool            { return false }
func (m *SnowGolem) CanBeHitByPiercingWeapon(world.Entity) bool { return true }
func (m *IronGolem) CanBeHitByPiercingWeapon(world.Entity) bool { return true }
func (m *SnowGolem) IsSensitiveToWater() bool                   { return true }
func (m *IronGolem) IsSensitiveToWater() bool                   { return false }

// TrySpawnGolemPattern checks the Java snow and iron golem patterns with pos as
// the newly placed carved pumpkin. It clears the pattern before adding the mob.
func TrySpawnGolemPattern(tx *world.Tx, pos cube.Pos) bool {
	switch head := tx.Block(pos).(type) {
	case block.Pumpkin:
		if !head.Carved {
			return false
		}
	case block.LitPumpkin:
	default:
		return false
	}
	return forEachGolemPatternOrientation(func(right, up, _ cube.Pos) bool {
		base := pos.Sub(golemScale(up, 2))
		if ironGolemPattern(tx, pos, right, up) {
			clearIronGolemPattern(tx, pos, right, up)
			tx.AddEntity(world.EntitySpawnOpts{Position: base.Vec3Middle().Add(mgl64.Vec3{0, .05, 0})}.New(IronGolemType, golemConfig{kind: ironKind, playerCreated: true}))
			return true
		}
		snowTop := pos.Sub(up)
		bottom := pos.Sub(golemScale(up, 2))
		if _, ok := tx.Block(snowTop).(block.Snow); !ok {
			return false
		}
		if _, ok := tx.Block(bottom).(block.Snow); !ok {
			return false
		}
		tx.SetBlock(pos, block.Air{}, nil)
		tx.SetBlock(snowTop, block.Air{}, nil)
		tx.SetBlock(bottom, block.Air{}, nil)
		tx.AddEntity(NewSnowGolem(world.EntitySpawnOpts{Position: bottom.Vec3Middle().Add(mgl64.Vec3{0, .05, 0})}))
		return true
	})
}

func forEachGolemPatternOrientation(visit func(right, up, forward cube.Pos) bool) bool {
	for _, forwardFace := range cube.Faces() {
		forward := golemFaceOffset(forwardFace)
		for _, upFace := range cube.Faces() {
			if upFace == forwardFace || upFace == forwardFace.Opposite() {
				continue
			}
			up := golemFaceOffset(upFace)
			if visit(golemCross(forward, up), up, forward) {
				return true
			}
		}
	}
	return false
}

func golemFaceOffset(face cube.Face) cube.Pos {
	switch face {
	case cube.FaceDown:
		return cube.Pos{0, -1, 0}
	case cube.FaceUp:
		return cube.Pos{0, 1, 0}
	case cube.FaceNorth:
		return cube.Pos{0, 0, -1}
	case cube.FaceSouth:
		return cube.Pos{0, 0, 1}
	case cube.FaceWest:
		return cube.Pos{-1, 0, 0}
	default:
		return cube.Pos{1, 0, 0}
	}
}

func golemCross(a, b cube.Pos) cube.Pos {
	return cube.Pos{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

func golemScale(pos cube.Pos, n int) cube.Pos { return cube.Pos{pos[0] * n, pos[1] * n, pos[2] * n} }

func ironGolemPattern(tx *world.Tx, head, right, up cube.Pos) bool {
	upper := head.Sub(up)
	base := head.Sub(golemScale(up, 2))
	_, center := tx.Block(upper).(block.Iron)
	_, bottom := tx.Block(base).(block.Iron)
	_, left := tx.Block(upper.Sub(right)).(block.Iron)
	_, rightIron := tx.Block(upper.Add(right)).(block.Iron)
	_, headLeftAir := tx.Block(head.Sub(right)).(block.Air)
	_, headRightAir := tx.Block(head.Add(right)).(block.Air)
	_, baseLeftAir := tx.Block(base.Sub(right)).(block.Air)
	_, baseRightAir := tx.Block(base.Add(right)).(block.Air)
	return center && bottom && left && rightIron && headLeftAir && headRightAir && baseLeftAir && baseRightAir
}

func clearIronGolemPattern(tx *world.Tx, head, right, up cube.Pos) {
	upper := head.Sub(up)
	base := head.Sub(golemScale(up, 2))
	for _, p := range [...]cube.Pos{head, upper, base, upper.Sub(right), upper.Add(right)} {
		tx.SetBlock(p, block.Air{}, nil)
	}
}

var _ Living = (*IronGolem)(nil)
var _ Living = (*SnowGolem)(nil)
var _ world.EntityType = ironGolemType{}
var _ world.EntityType = snowGolemType{}
