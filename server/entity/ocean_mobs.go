package entity

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// The ocean hostile and mount types share only their low-level living and swim
// machinery. Their AI state remains keyed by the concrete Java entity id.
type oceanKind uint8

const (
	oceanDrowned oceanKind = iota
	oceanGuardian
	oceanElderGuardian
	oceanNautilus
	oceanZombieNautilus
)

var (
	DrownedType        drownedType
	GuardianType       guardianType
	ElderGuardianType  elderGuardianType
	NautilusType       nautilusType
	ZombieNautilusType zombieNautilusType
)

type drownedType struct{}
type guardianType struct{}
type elderGuardianType struct{}
type nautilusType struct{}
type zombieNautilusType struct{}

func NewDrowned(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(DrownedType, oceanConfig{kind: oceanDrowned})
}
func NewGuardian(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(GuardianType, oceanConfig{kind: oceanGuardian})
}
func NewElderGuardian(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(ElderGuardianType, oceanConfig{kind: oceanElderGuardian})
}
func NewNautilus(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(NautilusType, oceanConfig{kind: oceanNautilus})
}
func NewZombieNautilus(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(ZombieNautilusType, oceanConfig{kind: oceanZombieNautilus})
}

type oceanEntityType struct {
	kind          oceanKind
	id            string
	width, height float64
}

func (t oceanEntityType) BBox(world.Entity) cube.BBox {
	return cube.Box(-t.width/2, 0, -t.width/2, t.width/2, t.height, t.width/2)
}
func (t oceanEntityType) open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) *oceanMob {
	if d.Data == nil {
		(oceanConfig{kind: t.kind}).Apply(d)
	}
	return &oceanMob{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (t oceanEntityType) DecodeNBT(m map[string]any, d *world.EntityData) {
	c := oceanConfig{kind: t.kind}
	if _, ok := m["Health"]; ok {
		c.Health = float64(nbtconv.Float32(m, "Health"))
		c.healthSet = true
	}
	if _, ok := m["MaxHealth"]; ok {
		c.MaxHealth = float64(nbtconv.Float32(m, "MaxHealth"))
	}
	c.fromBucket = nbtconv.Bool(m, "FromBucket")
	{
		c.tamed = nbtconv.Bool(m, "Tame")
		c.canBreakDoors = nbtconv.Bool(m, "CanBreakDoors")
		{
			c.canPickupLoot = nbtconv.Bool(m, "CanPickUpLoot")
			c.baby = nbtconv.Bool(m, "IsBaby")
		}
	}
	c.age = int(nbtconv.Int32(m, "Age"))
	c.air = int(nbtconv.Int16(m, "Air"))
	if v, ok := m["variant"].(string); ok && (v == "warm" || v == "temperate") {
		c.variant = v
	}
	if slots, ok := m["HandItems"].([]any); ok {
		if len(slots) > 0 {
			c.mainHand = decodeOceanHand(slots[0])
		}
		if len(slots) > 1 {
			c.offHand = decodeOceanHand(slots[1])
		}
	}
	if saddle, ok := m["Saddle"].(map[string]any); ok {
		c.saddle = !decodeOceanHand(saddle).Empty()
	}
	if armor, ok := m["BodyArmor"].(map[string]any); ok {
		if stack := decodeOceanHand(armor); !stack.Empty() {
			if a, ok := stack.Item().(item.NautilusArmor); ok {
				c.bodyArmorMaterial = int(a.Material)
				c.armorSet = true
			}
		}
	}
	c.Apply(d)
}
func (t oceanEntityType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*oceanBehaviour)
	m := map[string]any{
		"Health": float32(b.health.Health()), "MaxHealth": float32(b.health.MaxHealth()),
		"Air": int16(b.air),
	}
	if b.kind == oceanNautilus || b.kind == oceanZombieNautilus {
		m["Age"] = int32(b.age)
	}
	if b.fromBucket {
		m["FromBucket"] = byte(1)
	}
	if b.kind == oceanZombieNautilus {
		m["variant"] = b.variant
	}
	if b.kind == oceanNautilus || b.kind == oceanZombieNautilus {
		m["Tame"] = boolByte(b.tamed)
		if b.saddle {
			m["Saddle"] = encodeOceanHand(item.NewStack(item.Saddle{}, 1))
		}
		if b.bodyArmorMaterial >= 0 {
			m["BodyArmor"] = encodeOceanHand(item.NewStack(item.NautilusArmor{Material: item.NautilusArmorMaterial(b.bodyArmorMaterial)}, 1))
		}
	}
	if !b.mainHand.Empty() || !b.offHand.Empty() {
		m["HandItems"] = []any{encodeOceanHand(b.mainHand), encodeOceanHand(b.offHand)}
	}
	if b.kind == oceanDrowned {
		m["CanBreakDoors"] = boolByte(b.canBreakDoors)
		m["CanPickUpLoot"] = boolByte(b.canPickupLoot)
		m["IsBaby"] = boolByte(b.baby)
	}
	return m
}
func decodeOceanHand(raw any) item.Stack {
	m, ok := raw.(map[string]any)
	if !ok {
		return item.Stack{}
	}
	id, _ := m["id"].(string)
	if id == "" {
		id, _ = m["Name"].(string)
	}
	if id == "" {
		return item.Stack{}
	}
	it, ok := world.ItemByName(id, 0)
	if !ok {
		return item.Stack{}
	}
	count := 1
	if v, ok := m["Count"].(byte); ok {
		count = int(v)
	} else if v, ok := m["count"].(int32); ok {
		count = int(v)
	}
	return item.NewStack(it, count)
}
func encodeOceanHand(stack item.Stack) map[string]any {
	if stack.Empty() {
		return map[string]any{}
	}
	id, _ := stack.Item().EncodeItem()
	return map[string]any{"id": id, "Count": byte(stack.Count())}
}
func (drownedType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return &Drowned{oceanEntityType{kind: oceanDrowned}.open(tx, h, d)}
}
func (guardianType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return &Guardian{oceanEntityType{kind: oceanGuardian}.open(tx, h, d)}
}
func (elderGuardianType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return &ElderGuardian{oceanEntityType{kind: oceanElderGuardian}.open(tx, h, d)}
}
func (nautilusType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return &Nautilus{oceanEntityType{kind: oceanNautilus}.open(tx, h, d)}
}
func (zombieNautilusType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return &ZombieNautilus{oceanEntityType{kind: oceanZombieNautilus}.open(tx, h, d)}
}
func (drownedType) EncodeEntity() string        { return "minecraft:drowned" }
func (guardianType) EncodeEntity() string       { return "minecraft:guardian" }
func (elderGuardianType) EncodeEntity() string  { return "minecraft:elder_guardian" }
func (nautilusType) EncodeEntity() string       { return "minecraft:nautilus" }
func (zombieNautilusType) EncodeEntity() string { return "minecraft:zombie_nautilus" }
func (drownedType) BBox(e world.Entity) cube.BBox {
	if m, ok := e.(*Drowned); ok && m.b().baby {
		return oceanEntityType{width: .49, height: .98}.BBox(e)
	}
	return oceanEntityType{width: .6, height: 1.95}.BBox(e)
}
func (guardianType) BBox(e world.Entity) cube.BBox {
	return oceanEntityType{width: .85, height: .85}.BBox(e)
}
func (elderGuardianType) BBox(e world.Entity) cube.BBox {
	return oceanEntityType{width: 1.9975, height: 1.9975}.BBox(e)
}
func (nautilusType) BBox(e world.Entity) cube.BBox {
	if m, ok := e.(*Nautilus); ok && m.Age() < 0 {
		return oceanEntityType{width: .7, height: .5}.BBox(e)
	}
	return oceanEntityType{width: 1.4, height: 1.0}.BBox(e)
}
func (zombieNautilusType) BBox(e world.Entity) cube.BBox {
	return oceanEntityType{width: 1.4, height: 1.0}.BBox(e)
}
func (t drownedType) DecodeNBT(m map[string]any, d *world.EntityData) {
	(oceanEntityType{kind: oceanDrowned}).DecodeNBT(m, d)
}
func (t drownedType) EncodeNBT(d *world.EntityData) map[string]any {
	return (oceanEntityType{}).EncodeNBT(d)
}
func (t guardianType) DecodeNBT(m map[string]any, d *world.EntityData) {
	(oceanEntityType{kind: oceanGuardian}).DecodeNBT(m, d)
}
func (t guardianType) EncodeNBT(d *world.EntityData) map[string]any {
	return (oceanEntityType{}).EncodeNBT(d)
}
func (t elderGuardianType) DecodeNBT(m map[string]any, d *world.EntityData) {
	(oceanEntityType{kind: oceanElderGuardian}).DecodeNBT(m, d)
}
func (t elderGuardianType) EncodeNBT(d *world.EntityData) map[string]any {
	return (oceanEntityType{}).EncodeNBT(d)
}
func (t nautilusType) DecodeNBT(m map[string]any, d *world.EntityData) {
	(oceanEntityType{kind: oceanNautilus}).DecodeNBT(m, d)
}
func (t nautilusType) EncodeNBT(d *world.EntityData) map[string]any {
	return (oceanEntityType{}).EncodeNBT(d)
}
func (t zombieNautilusType) DecodeNBT(m map[string]any, d *world.EntityData) {
	(oceanEntityType{kind: oceanZombieNautilus}).DecodeNBT(m, d)
}
func (t zombieNautilusType) EncodeNBT(d *world.EntityData) map[string]any {
	return (oceanEntityType{}).EncodeNBT(d)
}

type oceanConfig struct {
	kind                                                                               oceanKind
	Health, MaxHealth                                                                  float64
	variant                                                                            string
	healthSet, fromBucket, tamed, saddle, armorSet, canBreakDoors, canPickupLoot, baby bool
	age, air, dashCooldown, beamTicks, bodyArmorMaterial                               int
	mainHand, offHand                                                                  item.Stack
}

func (c oceanConfig) Apply(d *world.EntityData) {
	b := c.New()
	b.variant = oceanVariant(c.variant)
	b.canBreakDoors, b.canPickupLoot, b.baby = c.canBreakDoors, c.canPickupLoot, c.baby
	d.Data = b
}
func (c oceanConfig) New() *oceanBehaviour {
	maxHealth := c.MaxHealth
	if maxHealth <= 0 {
		switch c.kind {
		case oceanDrowned:
			maxHealth = 20
		case oceanGuardian:
			maxHealth = 30
		case oceanElderGuardian:
			maxHealth = 80
		case oceanNautilus:
			maxHealth = 30
		default:
			maxHealth = 30
		}
	}
	health := c.Health
	if !c.healthSet && health <= 0 {
		health = maxHealth
	}
	armor := c.bodyArmorMaterial
	if !c.armorSet {
		armor = -1
	}
	air := c.air
	if air == 0 {
		air = 300
	}
	return &oceanBehaviour{kind: c.kind, health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .02, Drag: .04, DragBeforeGravity: true, NoBlockCollisions: false}, speed: oceanBaseSpeed(c.kind), fromBucket: c.fromBucket, tamed: c.tamed, age: c.age, air: air, dashCooldown: c.dashCooldown, beamTicks: c.beamTicks, saddle: c.saddle, bodyArmorMaterial: armor, mainHand: c.mainHand, offHand: c.offHand, nextWander: 1}
}
func oceanBaseSpeed(k oceanKind) float64 {
	switch k {
	case oceanDrowned:
		return .23
	case oceanGuardian:
		return .5
	case oceanElderGuardian:
		return .3
	default:
		return .25
	}
}

type Drowned struct{ *oceanMob }
type Guardian struct{ *oceanMob }
type ElderGuardian struct{ *oceanMob }
type Nautilus struct{ *oceanMob }
type ZombieNautilus struct{ *oceanMob }
type oceanMob struct{ *Ent }

func (m *oceanMob) b() *oceanBehaviour { return m.Behaviour().(*oceanBehaviour) }

type oceanBehaviour struct {
	kind                                                                oceanKind
	health                                                              *HealthManager
	effects                                                             *EffectManager
	mc                                                                  *MovementComputer
	speed                                                               float64
	dead, fromBucket, tamed, saddle, canBreakDoors, canPickupLoot, baby bool
	age, air, dashCooldown, beamTicks, nextWander, ticks, pathCooldown  int
	bodyArmorMaterial                                                   int
	variant                                                             string
	mainHand, offHand                                                   item.Stack
	beamTarget                                                          *world.EntityHandle
	vehicleStrafe, vehicleForward                                       float64
	path                                                                []cube.Pos
	pathTarget                                                          cube.Pos
	pathIndex                                                           int
	attackCooldown, thornsCooldown                                      int
	swim                                                                mgl64.Vec3
}

func (b *oceanBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	b.age++
	b.ticks++
	if inWater := func() bool { _, ok := tx.Liquid(cube.PosFromVec3(e.Position())); return ok }(); inWater {
		b.mc.Gravity = .005
		b.mc.Drag = .04
	} else {
		b.mc.Gravity = .08
		b.mc.Drag = .02
	}
	if b.attackCooldown > 0 {
		b.attackCooldown--
	}
	if b.thornsCooldown > 0 {
		b.thornsCooldown--
	}
	if b.dashCooldown > 0 {
		b.dashCooldown--
	}
	if b.kind == oceanDrowned {
		return b.tickDrowned(e, tx)
	}
	if b.kind == oceanGuardian || b.kind == oceanElderGuardian {
		return b.tickGuardian(e, tx)
	}
	return b.tickNautilus(e, tx)
}
func (b *oceanBehaviour) tickDrowned(e *Ent, tx *world.Tx) *Movement {
	target := oceanNearestTarget(tx, e.Position(), 16)
	if target == nil {
		b.path = nil
		b.wander(e, tx, .03)
		return b.move(e, tx)
	}
	delta := target.Position().Sub(e.Position())
	if _, inWater := tx.Liquid(cube.PosFromVec3(e.Position())); !inWater && b.pathToward(e, tx, target.Position()) {
		return b.move(e, tx)
	}
	if _, ok := b.mainHand.Item().(item.Trident); ok && delta.Len() <= 10 && b.attackCooldown == 0 {
		aim := delta
		magnitude := math.Hypot(delta[0], delta[2])
		aim[1] += .2 * magnitude
		if aim.Len() != 0 {
			velocity := aim.Normalize().Mul(1.6)
			tx.AddEntity(NewTrident(world.EntitySpawnOpts{Position: e.Position(), Velocity: velocity}, e, b.mainHand, 0, 0, false))
		}
		b.attackCooldown = 40
	} else if delta.Len() < 1.4 && b.attackCooldown == 0 {
		if l, ok := target.(Living); ok {
			l.Hurt(3, AttackDamageSource{Attacker: e})
			l.KnockBack(e.Position(), .3, .1)
		}
		b.attackCooldown = 20
	} else {
		b.swimToward(e, target.Position(), .09)
	}
	return b.move(e, tx)
}
func (b *oceanBehaviour) tickGuardian(e *Ent, tx *world.Tx) *Movement {
	if b.kind == oceanElderGuardian && b.ticks%1200 == 0 {
		for p := range tx.Players() {
			if l, ok := p.(Living); ok && p.Position().Sub(e.Position()).LenSqr() <= 2500 {
				l.AddEffect(effect.New(effect.MiningFatigue, 2, 300*time.Second))
			}
		}
	}
	target := oceanNearestTarget(tx, e.Position(), 10)
	if target != nil {
		b.beamTarget = target.H()
		if b.beamTicks == 0 {
			b.beamTicks = 1
		} else if b.beamTicks < 80 {
			b.beamTicks++
		} else if b.attackCooldown == 0 {
			if l, ok := target.(Living); ok {
				damage := 6.0
				if b.kind == oceanElderGuardian {
					damage = 8
				}
				l.Hurt(damage, AttackDamageSource{Attacker: e})
			}
			b.attackCooldown = 60
			b.beamTicks = 1
		}
		b.swimToward(e, target.Position(), .04)
		if delta := target.Position().Sub(e.Position()); delta.Len() < 2 && b.thornsCooldown == 0 {
			if l, ok := target.(Living); ok {
				l.Hurt(2, AttackDamageSource{Attacker: e})
			}
			b.thornsCooldown = 20
		}
	} else {
		b.beamTarget = nil
		b.beamTicks = 0
		b.wander(e, tx, .015)
	}
	return b.move(e, tx)
}
func (b *oceanBehaviour) tickNautilus(e *Ent, tx *world.Tx) *Movement {
	_, inWater := tx.Liquid(cube.PosFromVec3(e.Position()))
	if inWater {
		b.speed = .25
	} else {
		b.speed = .08
	}
	b.air = oceanAir(b.air, inWater, &oceanMob{Ent: e})
	if b.tamed && b.saddle {
		for passenger := range tx.Entities() {
			r, ok := passenger.(Rider)
			if !ok || r.Riding() != e.H() {
				continue
			}
			yaw := passenger.Rotation()[0] * math.Pi / 180
			velocity := e.data.Vel
			velocity[0] += (-math.Sin(yaw)*b.vehicleForward + math.Cos(yaw)*b.vehicleStrafe) * .008
			velocity[2] += (math.Cos(yaw)*b.vehicleForward + math.Sin(yaw)*b.vehicleStrafe) * .008
			e.data.Vel = velocity
			return b.move(e, tx)
		}
	}
	target := world.Entity(nil)
	if b.kind == oceanZombieNautilus && !b.tamed {
		target = oceanNearestTarget(tx, e.Position(), 12)
	}
	if target != nil {
		if target.Position().Sub(e.Position()).Len() < 1.5 && b.attackCooldown == 0 {
			if l, ok := target.(Living); ok {
				l.Hurt(4, AttackDamageSource{Attacker: e})
			}
			b.attackCooldown = 20
		} else {
			b.swimToward(e, target.Position(), .04)
		}
	} else {
		b.wander(e, tx, .02)
	}
	return b.move(e, tx)
}
func (b *oceanBehaviour) swimToward(e *Ent, p mgl64.Vec3, accel float64) {
	d := p.Sub(e.Position())
	_, inWater := e.tx.Liquid(cube.PosFromVec3(e.Position()))
	if !inWater {
		d[1] = 0
	}
	if d.Len() == 0 {
		return
	}
	d = d.Normalize()
	v := e.data.Vel
	v[0] += d[0] * accel
	v[1] += d[1] * accel
	v[2] += d[2] * accel
	limit := b.speed
	if v.Len() > limit {
		v = v.Normalize().Mul(limit)
	}
	e.data.Vel = v
	e.data.Rot[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	e.data.Rot[1] = -math.Atan2(d[1], math.Hypot(d[0], d[2])) * 180 / math.Pi
}
func oceanAir(air int, inWater bool, mob *oceanMob) int {
	if inWater {
		return 300
	}
	air--
	if air <= -20 {
		air = 0
		mob.Hurt(2, DrowningDamageSource{})
	}
	return air
}
func (b *oceanBehaviour) wander(e *Ent, tx *world.Tx, accel float64) {
	if b.nextWander > 0 {
		b.nextWander--
	}
	if b.nextWander == 0 {
		seed := int64(e.Position()[0]*341873128712+e.Position()[1]*132897987541+e.Position()[2]*42317861) + int64(b.age)
		r := java26.NewXoroshiroRandom(seed)
		b.nextWander = 40 + int(r.NextInt(81))
		b.swim = mgl64.Vec3{float64(r.NextDouble())*2 - 1, float64(r.NextDouble()) - .5, float64(r.NextDouble())*2 - 1}
		if _, inWater := tx.Liquid(cube.PosFromVec3(e.Position())); !inWater {
			b.swim[1] = 0
		}
		if b.swim.Len() != 0 {
			b.swim = b.swim.Normalize()
		}
	}
	v := e.data.Vel
	v[0] += b.swim[0] * accel
	v[1] += b.swim[1] * accel
	v[2] += b.swim[2] * accel
	e.data.Vel = v
}
func (b *oceanBehaviour) move(e *Ent, tx *world.Tx) *Movement {
	m := b.mc.TickMovement(e, e.data.Pos, e.data.Vel, e.data.Rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func oceanNearestTarget(tx *world.Tx, p mgl64.Vec3, r float64) world.Entity {
	var best world.Entity
	dist := r * r
	choose := func(e world.Entity) {
		if e == nil {
			return
		}
		d := e.Position().Sub(p).LenSqr()
		if d < dist {
			if _, ok := e.(Living); ok {
				dist = d
				best = e
			}
		}
	}
	for player := range tx.Players() {
		choose(player)
	}
	for candidate := range tx.Entities() {
		if _, ok := candidate.(*Axolotl); ok {
			choose(candidate)
		}
	}
	return best
}
func (b *oceanBehaviour) pathToward(e *Ent, tx *world.Tx, target mgl64.Vec3) bool {
	start := cube.PosFromVec3(e.Position())
	goal := cube.PosFromVec3(target)
	if b.pathCooldown > 0 {
		b.pathCooldown--
	}
	if len(b.path) == 0 || b.pathTarget != goal || b.pathCooldown == 0 {
		b.path = findPath(tx, start, goal, 128, true, false)
		b.pathTarget = goal
		b.pathIndex = 0
		b.pathCooldown = 10
	}
	for b.pathIndex < len(b.path) {
		waypoint := b.path[b.pathIndex].Vec3().Add(mgl64.Vec3{.5, 0, .5})
		delta := waypoint.Sub(e.Position())
		delta[1] = 0
		if delta.Len() < .35 {
			b.pathIndex++
			continue
		}
		direction := delta.Normalize()
		e.data.Vel[0], e.data.Vel[2] = direction[0]*b.speed, direction[2]*b.speed
		e.data.Rot[0] = math.Atan2(-direction[0], direction[2]) * 180 / math.Pi
		return true
	}
	b.path = nil
	return false
}
func (m *oceanMob) FinalizeSpawn(reason string, random *java26.Random) {
	b := m.b()
	if b.kind == oceanZombieNautilus {
		biome := m.tx.Biome(cube.PosFromVec3(m.Position()))
		b.variant = "temperate"
		for _, tag := range biome.Tags() {
			if tag == "spawns_coral_variant_zombie_nautilus" {
				b.variant = "warm"
				break
			}
		}
		if biome.String() == "warm_ocean" {
			b.variant = "warm"
		}
	}
	if b.kind != oceanDrowned || random == nil {
		return
	}
	baby, doors, pickup, _ := finalizeZombieSpawn(reason, m.tx.LocalDifficulty(cube.PosFromVec3(m.Position())), random, false, m.H().SpawnGroup())
	b.canBreakDoors, b.canPickupLoot = doors, pickup
	if baby {
		b.baby = true
		b.speed *= 1.5
	}
	if b.mainHand.Empty() && random.NextFloat() > .9 {
		if random.NextInt(16) < 10 {
			b.mainHand = item.NewStack(item.Trident{}, 1)
		} else {
			b.mainHand = item.NewStack(item.FishingRod{}, 1)
		}
	}
	if b.offHand.Empty() && random.NextFloat() < .03 {
		b.offHand = item.NewStack(item.NautilusShell{}, 1)
	}
}

func (m *oceanMob) Health() float64        { return m.b().health.Health() }
func (m *oceanMob) MaxHealth() float64     { return m.b().health.MaxHealth() }
func (m *oceanMob) SetMaxHealth(h float64) { m.b().health.SetMaxHealth(h) }
func (m *oceanMob) Dead() bool             { return m.b().dead || m.Health() <= 0 }
func (m *oceanMob) Hurt(d float64, src world.DamageSource) (float64, bool) {
	b := m.b()
	if m.Dead() || d <= 0 || (src.Fire() && m.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if armor := m.BodyArmor(); !armor.Empty() && src.ReducedByArmour() {
		if a, ok := armor.Item().(item.NautilusArmor); ok {
			points, toughness := a.DefencePoints(), a.Toughness()
			reduction := min(20.0, max(points/5, points-d/(2+toughness/4)))
			d *= 1 - reduction/25
		}
	}
	if res, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, res.Level())
	}
	b.health.AddHealth(-d)
	m.viewAction(HurtAction{})
	if (b.kind == oceanGuardian || b.kind == oceanElderGuardian) && b.thornsCooldown == 0 {
		if s, ok := src.(AttackDamageSource); ok {
			if l, ok := s.Attacker.(Living); ok {
				l.Hurt(2, AttackDamageSource{Attacker: m})
				b.thornsCooldown = 20
			}
		}
	}
	if m.Dead() {
		m.die()
	}
	return d, true
}
func (m *oceanMob) Heal(h float64, _ world.HealingSource) float64 {
	if m.Dead() || h <= 0 {
		return 0
	}
	old := m.Health()
	m.b().health.AddHealth(h)
	return m.Health() - old
}
func (m *oceanMob) KnockBack(src mgl64.Vec3, force, height float64) {
	d := m.Position().Sub(src)
	if d.Len() != 0 {
		d = d.Normalize().Mul(force)
	}
	d[1] = height
	m.SetVelocity(d)
}
func (m *oceanMob) AddEffect(e effect.Effect) {
	if (m.b().kind == oceanNautilus || m.b().kind == oceanZombieNautilus) && e.Type() == effect.Poison {
		return
	}
	m.b().effects.Add(e, m)
}
func (m *oceanMob) RemoveEffect(t effect.Type)   { m.b().effects.Remove(t, m) }
func (m *oceanMob) Effects() []effect.Effect     { return m.b().effects.Effects() }
func (m *oceanMob) Speed() float64               { return m.b().speed }
func (m *oceanMob) SetSpeed(v float64)           { m.b().speed = v }
func (m *oceanMob) hasEffect(t effect.Type) bool { _, ok := m.b().effects.Effect(t); return ok }
func (m *oceanMob) viewAction(a world.EntityAction) {
	vs := m.tx.Viewers(m.Position())
	for _, v := range vs {
		v.ViewEntityAction(m, a)
	}
	m.tx.ReleaseViewers(vs)
}
func (m *oceanMob) die() {
	b := m.b()
	if b.dead {
		return
	}
	b.dead = true
	m.viewAction(DeathAction{})
	if armor := m.BodyArmor(); !armor.Empty() {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, armor))
	}
	if b.saddle {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, item.NewStack(item.Saddle{}, 1)))
	}
	if !b.offHand.Empty() {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, b.offHand))
	}
	_ = m.CloseIn(m.tx)
}
func (m *oceanMob) CanEquipBodyArmor(user world.Entity) bool {
	b := m.b()
	return (b.kind == oceanNautilus || b.kind == oceanZombieNautilus) && b.tamed && b.age >= 0 && b.bodyArmorMaterial < 0 && user != nil
}
func (m *oceanMob) BodyArmor() item.Stack {
	b := m.b()
	if b.bodyArmorMaterial < 0 {
		return item.Stack{}
	}
	return item.NewStack(item.NautilusArmor{Material: item.NautilusArmorMaterial(b.bodyArmorMaterial)}, 1)
}
func (m *oceanMob) EquipBodyArmor(tx *world.Tx, user world.Entity, armor world.Item) bool {
	if !m.CanEquipBodyArmor(user) {
		return false
	}
	a, ok := armor.(item.NautilusArmor)
	if !ok {
		return false
	}
	m.b().bodyArmorMaterial = int(a.Material)
	tx.PlaySound(m.Position(), sound.Custom{Name: "entity.nautilus.armor_equip", Volume: 1, Pitch: 1})
	return true
}
func (m *oceanMob) RemoveBodyArmor(tx *world.Tx) item.Stack {
	b := m.b()
	if b.bodyArmorMaterial < 0 {
		return item.Stack{}
	}
	armor := m.BodyArmor()
	b.bodyArmorMaterial = -1
	return armor
}
func (m *oceanMob) ArmorShearingSound() world.Sound {
	return sound.Custom{Name: "entity.nautilus.armor_unequip", Volume: 1, Pitch: 1}
}
func (m *oceanMob) CanShearBodyArmor(world.Entity) bool {
	return !m.hasPassenger(m.tx) && !m.BodyArmor().Empty()
}
func (m *oceanMob) Dismount(tx *world.Tx, passenger world.Entity) {
	if passenger == nil {
		return
	}
	if r, ok := passenger.(Rider); ok && r.Riding() == m.H() {
		r.SetRiding(nil)
	}
}
func (m *oceanMob) hasPassenger(tx *world.Tx) bool {
	for passenger := range tx.Entities() {
		if r, ok := passenger.(Rider); ok && r.Riding() == m.H() {
			return true
		}
	}
	return false
}
func (m *oceanMob) Interact(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	b := m.b()
	if b.kind != oceanNautilus && b.kind != oceanZombieNautilus {
		return false
	}
	if b.age < 0 {
		return false
	}
	main, _ := user.HeldItems()
	if !main.Empty() {
		switch it := main.Item().(type) {
		case item.Saddle:
			if b.tamed && !b.saddle {
				b.saddle = true
				ctx.SubtractFromCount(1)
				tx.PlaySound(m.Position(), sound.Custom{Name: "entity.nautilus.saddle_equip", Volume: 1, Pitch: 1})
				return true
			}
		case item.Cod:
			if !b.tamed && !it.Cooked {
				ctx.SubtractFromCount(1)
				r := java26.NewXoroshiroRandom(int64(m.Position()[0]*341873128712+m.Position()[2]*132897987541) + int64(b.age))
				if r.NextInt(3) == 0 {
					b.tamed = true
					b.health.SetMaxHealth(30)
				}
				tx.PlaySound(m.Position(), sound.Custom{Name: "entity.nautilus.eat", Volume: 1, Pitch: 1})
				return true
			}
		case item.Salmon:
			if !b.tamed && !it.Cooked {
				ctx.SubtractFromCount(1)
				r := java26.NewXoroshiroRandom(int64(m.Position()[0]*341873128712+m.Position()[2]*132897987541) + int64(b.age))
				if r.NextInt(3) == 0 {
					b.tamed = true
					b.health.SetMaxHealth(30)
				}
				tx.PlaySound(m.Position(), sound.Custom{Name: "entity.nautilus.eat", Volume: 1, Pitch: 1})
				return true
			}
		case item.Pufferfish:
			if !b.tamed {
				ctx.SubtractFromCount(1)
				r := java26.NewXoroshiroRandom(int64(m.Position()[0]*341873128712+m.Position()[2]*132897987541) + int64(b.age))
				if r.NextInt(3) == 0 {
					b.tamed = true
					b.health.SetMaxHealth(30)
				}
				tx.PlaySound(m.Position(), sound.Custom{Name: "entity.nautilus.eat", Volume: 1, Pitch: 1})
				return true
			}
		}
	}
	if b.tamed && b.saddle && !m.hasPassenger(tx) {
		if rider, ok := user.(Rider); ok && rider.Riding() == nil {
			rider.SetRiding(m.H())
			return true
		}
	}
	return false
}
func (m *oceanMob) ControlsVehicle(passenger world.Entity) bool {
	r, ok := passenger.(Rider)
	return m.b().saddle && ok && r.Riding() == m.H()
}
func (m *oceanMob) Saddled() bool { return m.b().saddle }
func (m *oceanMob) SetVehicleInput(strafe, forward float64) {
	b := m.b()
	if !b.saddle || b.dashCooldown > 0 {
		return
	}
	b.vehicleStrafe = max(-1, min(1, strafe))
	b.vehicleForward = max(-1, min(1, forward))
}
func (m *oceanMob) BreathesUnderwater() bool { return true }
func (m *oceanMob) Age() int                 { return m.b().age }
func (m *oceanMob) SetAge(age int)           { m.b().age = age }
func (m *oceanMob) Baby() bool               { b := m.b(); return b.baby || b.age < 0 }
func (m *oceanMob) AirSupply() int           { return m.b().air }
func (m *oceanMob) SetAirSupply(v int)       { m.b().air = v }
func (m *oceanMob) Dash(tx *world.Tx, rider world.Entity) bool {
	b := m.b()
	r, ok := rider.(Rider)
	if !ok || r.Riding() != m.H() || !b.saddle || b.dashCooldown > 0 || b.kind != oceanNautilus {
		return false
	}
	look := rider.Rotation().Vec3()
	power := .3
	if _, inWater := tx.Liquid(cube.PosFromVec3(m.Position())); !inWater {
		power = .125
	}
	m.SetVelocity(m.Velocity().Add(look.Mul(power)))
	b.dashCooldown = 40
	return true
}
func (m *oceanMob) Tame(user world.Entity) {
	b := m.b()
	b.tamed = true
	b.health.SetMaxHealth(30)
	tx := m.tx
	tx.PlaySound(m.Position(), sound.Custom{Name: "entity.nautilus.eat", Volume: 1, Pitch: 1})
}
func (m *oceanMob) HeldItems() (item.Stack, item.Stack) { return m.b().mainHand, m.b().offHand }
func (m *oceanMob) SetHeldItems(main, off item.Stack)   { m.b().mainHand, m.b().offHand = main, off }
func (m *oceanMob) BeamTicks() int                      { return m.b().beamTicks }
func (m *oceanMob) Dashing() bool                       { return m.b().dashCooldown > 35 }

var _ world.EntityType = drownedType{}
var _ world.EntityType = guardianType{}
var _ world.EntityType = elderGuardianType{}
var _ world.EntityType = nautilusType{}
var _ world.EntityType = zombieNautilusType{}
var _ Living = (*oceanMob)(nil)
var _ BodyArmorable = (*oceanMob)(nil)
var _ PassengerDismounter = (*oceanMob)(nil)
var _ PiercingWeaponTarget = (*oceanMob)(nil)
var _ VehicleInput = (*oceanMob)(nil)
var _ VehicleController = (*oceanMob)(nil)
var _ Interactable = (*oceanMob)(nil)

func (m *oceanMob) BeamTarget() world.Entity {
	h := m.b().beamTarget
	if h == nil {
		return nil
	}
	target, ok := h.Entity(m.tx)
	if !ok {
		return nil
	}
	return target
}
func (m *oceanMob) CanBeHitByPiercingWeapon(attacker world.Entity) bool {
	return !m.Dead() && attacker != nil && attacker.H() != m.H()
}
func oceanVariant(variant string) string {
	if variant == "warm" {
		return "warm"
	}
	return "temperate"
}
func (m *oceanMob) Variant() string           { return oceanVariant(m.b().variant) }
func (m *oceanMob) SetVariant(variant string) { m.b().variant = oceanVariant(variant) }
