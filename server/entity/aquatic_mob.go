package entity

import (
	"hash/fnv"
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

var (
	AxolotlType      = aquaticMobType{id: "minecraft:axolotl", width: .75, height: .42, health: 14}
	CodType          = aquaticMobType{id: "minecraft:cod", width: .5, height: .3, health: 3}
	PufferfishType   = aquaticMobType{id: "minecraft:pufferfish", width: .7, height: .7, health: 3}
	SalmonType       = aquaticMobType{id: "minecraft:salmon", width: .7, height: .4, health: 3}
	TadpoleType      = aquaticMobType{id: "minecraft:tadpole", width: .4, height: .3, health: 6}
	TropicalFishType = aquaticMobType{id: "minecraft:tropical_fish", width: .5, height: .4, health: 3}
)

type aquaticMobType struct {
	id                    string
	width, height, health float64
}

func (t aquaticMobType) EncodeEntity() string { return t.id }
func (t aquaticMobType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	conf := AquaticMobConfig{ID: t.id, MaxHealth: t.health}
	if t.id == "minecraft:salmon" {
		conf.Variant = 1
	}
	if data.Data == nil {
		conf.Apply(data)
	}
	if h != nil {
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(h.UUID().String()))
		behaviour := data.Data.(*AquaticMobBehaviour)
		behaviour.random = java26.NewLegacyRandom(int64(hash.Sum64()))
		behaviour.nextSchoolTick = 200 + int(behaviour.random.NextInt(200)%20)
	}
	base := &AquaticMob{Ent: &Ent{tx: tx, handle: h, data: data}}
	switch t.id {
	case "minecraft:axolotl":
		return &Axolotl{AquaticMob: base}
	case "minecraft:cod":
		return &Cod{AquaticMob: base}
	case "minecraft:pufferfish":
		return &Pufferfish{AquaticMob: base}
	case "minecraft:salmon":
		return &Salmon{AquaticMob: base}
	case "minecraft:tadpole":
		return &Tadpole{AquaticMob: base}
	default:
		return &TropicalFish{AquaticMob: base}
	}
}
func (t aquaticMobType) BBox(e world.Entity) cube.BBox {
	w, h := t.width, t.height
	if t.id == "minecraft:pufferfish" && e != nil {
		if mob, ok := e.(interface{ behaviour() *AquaticMobBehaviour }); ok {
			scale := .5
			if state := mob.behaviour().puffState; state == 0 {
				scale = .5
			} else if state == 1 {
				scale = .7
			} else {
				scale = 1
			}
			w, h = .7*scale, .7*scale
		}
	}
	if t.id == "minecraft:salmon" && e != nil {
		if mob, ok := e.(interface{ behaviour() *AquaticMobBehaviour }); ok {
			scale := 1.0
			switch mob.behaviour().variant {
			case 0:
				scale = .5
			case 2:
				scale = 1.5
			}
			w, h = w*scale, h*scale
		}
	}
	if t.id == "minecraft:axolotl" && e != nil {
		if mob, ok := e.(interface{ behaviour() *AquaticMobBehaviour }); ok && mob.behaviour().age < 0 {
			w, h = .375, .21
		}
	}
	return cube.Box(-w/2, 0, -w/2, w/2, h, w/2)
}
func aquaticWorldEntity(e *Ent) world.Entity {
	base := &AquaticMob{Ent: e}
	switch base.behaviour().id {
	case "minecraft:axolotl":
		return &Axolotl{AquaticMob: base}
	case "minecraft:cod":
		return &Cod{AquaticMob: base}
	case "minecraft:pufferfish":
		return &Pufferfish{AquaticMob: base}
	case "minecraft:salmon":
		return &Salmon{AquaticMob: base}
	case "minecraft:tadpole":
		return &Tadpole{AquaticMob: base}
	default:
		return &TropicalFish{AquaticMob: base}
	}
}
func (t aquaticMobType) DecodeNBT(nbt map[string]any, data *world.EntityData) {
	c := AquaticMobConfig{ID: t.id, MaxHealth: t.health}
	if t.id == "minecraft:salmon" {
		c.Variant = 1
	}
	if _, ok := nbt["Health"]; ok {
		c.Health = float64(nbtconv.Float32(nbt, "Health"))
		c.healthSet = true
	}
	if _, ok := nbt["Variant"]; ok {
		c.Variant = int(nbtconv.Int32(nbt, "Variant"))
	}
	if _, ok := nbt["BucketVariantTag"]; ok {
		c.BucketVariant = int(nbtconv.Int32(nbt, "BucketVariantTag"))
	}
	if _, ok := nbt["Age"]; ok {
		c.Age = int(nbtconv.Int32(nbt, "Age"))
	}
	if _, ok := nbt["PuffState"]; ok {
		c.PuffState = int(nbtconv.Int32(nbt, "PuffState"))
	}
	if _, ok := nbt["AgeLocked"]; ok {
		c.AgeLocked = nbtconv.Bool(nbt, "AgeLocked")
	}
	if _, ok := nbt["FromBucket"]; ok {
		c.fromBucket = nbtconv.Bool(nbt, "FromBucket")
	}
	c.Apply(data)
	if t.id == "minecraft:axolotl" {
		data.Data.(*AquaticMobBehaviour).breeding.decodeNBT(nbt)
	}
}
func (aquaticMobType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*AquaticMobBehaviour)
	m := map[string]any{"Health": float32(b.health.Health()), "Variant": int32(b.variant), "BucketVariantTag": int32(b.bucketVariant), "Age": int32(b.age), "PuffState": int32(b.puffState), "FromBucket": boolByte(b.id == "minecraft:tadpole" || b.fromBucket)}
	switch b.id {
	case "minecraft:axolotl":
		b.breeding.encodeNBT(m)
	case "minecraft:tadpole":
		m["AgeLocked"] = boolByte(b.breeding.ageLocked)
	}
	return m
}

// AquaticMobConfig configures a fish-like mob decoded from Java entity NBT.
type AquaticMobConfig struct {
	ID                                     string
	Health, MaxHealth                      float64
	Variant, BucketVariant, Age, PuffState int
	AgeLocked, fromBucket, healthSet       bool
}

func (c AquaticMobConfig) Apply(data *world.EntityData) { data.Data = c.New() }
func (c AquaticMobConfig) New() *AquaticMobBehaviour {
	maxHealth := c.MaxHealth
	if maxHealth <= 0 {
		maxHealth = 3
	}
	health := c.Health
	if !c.healthSet && health <= 0 {
		health = maxHealth
	}
	speed := .1
	if c.ID == "minecraft:axolotl" {
		speed = 1
	}
	return &AquaticMobBehaviour{health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), id: c.ID, variant: c.Variant, bucketVariant: c.BucketVariant, age: c.Age, puffState: c.PuffState, breeding: breeding{ageLocked: c.AgeLocked}, fromBucket: c.fromBucket, movement: &MovementComputer{Gravity: .08, Drag: .1, DragBeforeGravity: true}, speed: speed, schoolGroup: &aquaticSchoolGroupData{size: 1}, random: java26.NewLegacyRandom(int64(c.Variant))}
}

type AquaticMobBehaviour struct {
	health                                                                                                                                                                       *HealthManager
	effects                                                                                                                                                                      *EffectManager
	id                                                                                                                                                                           string
	variant, bucketVariant, age, puffState, airTicks, inflateCounter, deflateTimer, playDeadTicks, attackCooldown, huntingCooldown, ticks, nextSchoolTick, panicTicks int
	fromBucket, dead, panicFromSet                                                                                                                                    bool
	// breeding is the love and growth state of axolotls; tadpoles only use
	// its age lock.
	breeding breeding
	panicFrom                                                                                                                                                                    mgl64.Vec3
	target, schoolLeader                                                                                                                                                         *world.EntityHandle
	schoolGroup                                                                                                                                                                  *aquaticSchoolGroupData
	movement                                                                                                                                                                     *MovementComputer
	speed                                                                                                                                                                        float64
	random                                                                                                                                                                       java26.Random
	navPath                                                                                                                                                                      []cube.Pos
	navIndex, navTicks                                                                                                                                                           int
	navGoal                                                                                                                                                                      cube.Pos
	navGoalSet                                                                                                                                                                   bool
	swimGoal                                                                                                                                                                     mgl64.Vec3
	swimGoalSet                                                                                                                                                                  bool
}
type AquaticMob struct{ *Ent }
type Axolotl struct{ *AquaticMob }

// Rehydrate ports Axolotl.rehydrate: a splash of water gives an axolotl out
// of water 1800 more ticks of air, up to its maximum.
func (a *Axolotl) Rehydrate() {
	b := a.behaviour()
	b.airTicks = max(b.airTicks-1800, 0)
}
type Cod struct{ *AquaticMob }
type Pufferfish struct{ *AquaticMob }
type Salmon struct{ *AquaticMob }
type Tadpole struct{ *AquaticMob }
type TropicalFish struct{ *AquaticMob }

func (a *AquaticMob) behaviour() *AquaticMobBehaviour { return a.Behaviour().(*AquaticMobBehaviour) }
func (a *AquaticMob) Tick(tx *world.Tx, current int64) {
	a.behaviour().effects.Tick(a, tx)
	a.Ent.Tick(tx, current)
}
func (b *AquaticMobBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.dead {
		return nil
	}
	pos := cube.PosFromVec3(e.Position())
	_, inWater := tx.Liquid(pos)
	airLimit := 300
	if b.id == "minecraft:axolotl" {
		airLimit = 6000
	}
	if inWater {
		b.airTicks = 0
	} else {
		b.airTicks++
		if b.airTicks > airLimit && b.airTicks%20 == 0 {
			(&AquaticMob{Ent: e}).Hurt(2, DrowningDamageSource{})
			if b.dead {
				return nil
			}
		}
	}
	schoolLeader := b.schoolingLeader(e, tx)
	fleeTarget, fleeSpeed, fleeing := aquaticFleeTarget(b, e, tx)
	if inWater && !fleeing && b.id != "minecraft:axolotl" && schoolLeader == nil && !b.swimGoalSet && b.random.NextInt(40) == 0 {
		if goal, ok := randomAquaticGoal(b, e, tx); ok {
			b.swimGoal, b.swimGoalSet = goal, true
		}
	}
	vel := e.data.Vel
	if b.id == "minecraft:axolotl" {
		partner, inLove := tickBreeding(&Axolotl{&AquaticMob{Ent: e}}, tx)
		vel = b.tickAxolotl(e, tx, vel, partner, inLove)
	}
	if inWater {
		b.movement.Gravity = 0
		vel = vel.Mul(.9)
		if fleeing {
			vel = vel.Add(aquaticSteer(b, e, tx, fleeTarget, b.speed*fleeSpeed))
		} else if schoolLeader != nil {
			vel = vel.Add(aquaticSteer(b, e, tx, schoolLeader.Position(), b.speed))
		} else if b.swimGoalSet {
			if e.Position().Sub(b.swimGoal).LenSqr() < .5 {
				b.swimGoalSet = false
			} else {
				vel = vel.Add(aquaticSteer(b, e, tx, b.swimGoal, b.speed))
			}
		}
	} else {
		b.movement.Gravity = .08
		b.swimGoalSet = false
		if b.id != "minecraft:axolotl" && b.movement.OnGround() && b.movement.CollidedVertically() {
			vel = vel.Add(mgl64.Vec3{float64(b.random.NextFloat())*.1 - .05, .4, float64(b.random.NextFloat())*.1 - .05})
			tx.PlaySound(e.Position(), sound.Custom{Name: aquaticFlopSound(b.id), Volume: 1, Pitch: 1})
		}
	}
	if b.id == "minecraft:pufferfish" {
		b.tickPuffer(e, tx)
	}
	if b.id == "minecraft:axolotl" && b.playDeadTicks > 0 {
		b.playDeadTicks--
	}
	b.ticks++
	if b.id == "minecraft:tadpole" && !b.breeding.ageLocked {
		b.age++
	}
	if b.id == "minecraft:tadpole" && b.age >= 24000 {
		opts := world.EntitySpawnOpts{Position: e.Position(), Rotation: e.data.Rot, Velocity: e.data.Vel}
		tx.AddEntity(opts.New(FrogType, frogConfig{variant: frogVariantAt(tx, e.Position())}))
		_ = e.CloseIn(tx)
		return nil
	}
	return b.movement.TickMovement(e, e.Position(), vel, e.data.Rot, e.data.Rot, tx).apply(e)
}
func (b *AquaticMobBehaviour) tickPuffer(e *Ent, tx *world.Tx) {
	box := e.H().Type().BBox(aquaticWorldEntity(e)).Translate(e.Position()).Grow(2)
	threat := false
	for target := range tx.EntitiesWithin(box) {
		if target.H() != e.handle && pufferTargetAllowed(target) {
			if living, ok := target.(Living); ok && !living.Dead() {
				threat = true
				break
			}
		}
	}
	if threat {
		b.deflateTimer = 0
		if b.inflateCounter == 0 {
			b.inflateCounter = 1
		}
	} else if b.inflateCounter > 0 {
		b.inflateCounter, b.deflateTimer = 0, 0
	} else if b.puffState > 0 {
		b.deflateTimer++
	}
	if b.inflateCounter > 0 {
		if b.puffState == 0 {
			b.puffState = 1
			tx.PlaySound(e.Position(), sound.Custom{Name: "entity.puffer_fish.blow_up", Volume: 1, Pitch: 1})
			b.viewPuffState(e)
		} else if b.inflateCounter > 40 && b.puffState == 1 {
			b.puffState = 2
			tx.PlaySound(e.Position(), sound.Custom{Name: "entity.puffer_fish.blow_up", Volume: 1, Pitch: 1})
			b.viewPuffState(e)
		}
		b.inflateCounter++
	} else if b.puffState == 2 && b.deflateTimer > 60 {
		b.puffState = 1
		tx.PlaySound(e.Position(), sound.Custom{Name: "entity.puffer_fish.blow_out", Volume: 1, Pitch: 1})
		b.viewPuffState(e)
	} else if b.puffState == 1 && b.deflateTimer > 100 {
		b.puffState = 0
		tx.PlaySound(e.Position(), sound.Custom{Name: "entity.puffer_fish.blow_out", Volume: 1, Pitch: 1})
		b.viewPuffState(e)
	}
	if b.puffState == 0 {
		return
	}
	touchBox := e.H().Type().BBox(aquaticWorldEntity(e)).Translate(e.Position()).Grow(.3)
	for target := range tx.EntitiesWithin(touchBox) {
		if target.H() == e.handle {
			continue
		}
		living, ok := target.(Living)
		if !ok || living.Dead() {
			continue
		}
		if _, vulnerable := living.Hurt(float64(1+b.puffState), AttackDamageSource{Attacker: &AquaticMob{Ent: e}}); vulnerable {
			living.AddEffect(effect.New(effect.Poison, 1, time.Duration(60*b.puffState)*time.Second/20))
			tx.PlaySound(e.Position(), sound.Custom{Name: "entity.puffer_fish.sting", Volume: 1, Pitch: 1})
		}
	}
}

func (b *AquaticMobBehaviour) viewPuffState(e *Ent) {
	viewers := e.tx.Viewers(e.Position())
	for _, v := range viewers {
		v.ViewEntityState(aquaticWorldEntity(e))
	}
	e.tx.ReleaseViewers(viewers)
}
func (a *AquaticMob) Health() float64        { return a.behaviour().health.Health() }
func (a *AquaticMob) MaxHealth() float64     { return a.behaviour().health.MaxHealth() }
func (a *AquaticMob) SetMaxHealth(h float64) { a.behaviour().health.SetMaxHealth(h) }
func (a *AquaticMob) Dead() bool             { return a.behaviour().dead || a.Health() <= 0 }
func (a *AquaticMob) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if a.Dead() || d <= 0 {
		return 0, false
	}
	b := a.behaviour()
	old := a.Health()
	if b.id != "minecraft:axolotl" {
		b.panicTicks = 100
		if attacker := aquaticDamageAttacker(src); attacker != nil {
			b.panicFrom, b.panicFromSet = attacker.Position(), true
		} else {
			b.panicFromSet = false
		}
	}
	if b.id == "minecraft:axolotl" && b.playDeadTicks == 0 {
		_, inWater := a.tx.Liquid(cube.PosFromVec3(a.Position()))
		if inWater && b.random.NextInt(3) == 0 && (float64(b.random.NextInt(3)) < d || old/a.MaxHealth() < .5) && d < old && aquaticDamageHasAttacker(src) {
			b.playDeadTicks = 200
			a.AddEffect(effect.New(effect.Regeneration, 1, 10*time.Second))
		}
	}
	b.health.AddHealth(-d)
	if b.id == "minecraft:axolotl" {
		resetLove(&Axolotl{a})
	}
	dealt := old - a.Health()
	a.viewAction(HurtAction{})
	onMobHurt(a.tx, a)
	if a.Dead() {
		b.dead = true
		a.viewAction(DeathAction{})
		dropAllDeathLoot(a.tx, a, src)
		_ = a.CloseIn(a.tx)
	}
	return dealt, true
}
func aquaticDamageHasAttacker(src world.DamageSource) bool {
	switch damage := src.(type) {
	case AttackDamageSource:
		return damage.Attacker != nil
	case ProjectileDamageSource:
		return damage.Owner != nil || damage.Projectile != nil
	case ExplosionDamageSource:
		return damage.Source != nil
	default:
		return false
	}
}
func (a *AquaticMob) Heal(h float64, _ world.HealingSource) float64 {
	if a.Dead() || h <= 0 {
		return 0
	}
	old := a.Health()
	a.behaviour().health.AddHealth(h)
	return a.Health() - old
}
func (a *AquaticMob) KnockBack(src mgl64.Vec3, force, height float64) {
	d := a.Position().Sub(src)
	if d.Len() != 0 {
		d = d.Normalize().Mul(force)
	}
	d[1] = height
	a.SetVelocity(d)
}
func (a *AquaticMob) AddEffect(e effect.Effect)  { a.behaviour().effects.Add(e, a) }
func (a *AquaticMob) RemoveEffect(t effect.Type) { a.behaviour().effects.Remove(t, a) }
func (a *AquaticMob) Effects() []effect.Effect   { return a.behaviour().effects.Effects() }
func (a *AquaticMob) Variant() int32 {
	b := a.behaviour()
	if b.id == "minecraft:pufferfish" {
		return int32(b.puffState)
	}
	return int32(b.variant)
}
func (a *AquaticMob) Baby() bool      { b := a.behaviour(); return b.id == "minecraft:axolotl" && b.age < 0 }
func (a *AquaticMob) Breathing() bool { return a.AirSupply() > 0 }
func (a *AquaticMob) AirSupply() time.Duration {
	b := a.behaviour()
	ticks := 300
	if b.id == "minecraft:axolotl" {
		ticks = 6000
	}
	return time.Duration(max(ticks-b.airTicks, 0)) * time.Second / 20
}
func (a *AquaticMob) MaxAirSupply() time.Duration {
	if a.behaviour().id == "minecraft:axolotl" {
		return 6000 * time.Second / 20
	}
	return 300 * time.Second / 20
}
func (a *AquaticMob) Speed() float64     { return a.behaviour().speed }
func (a *AquaticMob) SetSpeed(v float64) { a.behaviour().speed = v }
func (a *AquaticMob) CanBePickedUpWithBucket(bucket item.Bucket) bool {
	liquid, ok := bucket.Content.Liquid()
	return ok && liquid.LiquidType() == "water" && !a.Dead()
}
func (a *AquaticMob) BucketItem() item.Stack {
	b := a.behaviour()
	m := item.MobBucket{Entity: b.id}
	switch b.id {
	case "minecraft:cod", "minecraft:pufferfish", "minecraft:salmon", "minecraft:tropical_fish":
		return item.NewStack(item.FishBucket{MobBucket: m}, 1)
	default:
		return item.NewStack(m, 1)
	}
}
func (a *AquaticMob) SaveToBucket(stack item.Stack) item.Stack {
	b := a.behaviour()
	data := map[string]any{"Health": float32(b.health.Health()), "Variant": int32(b.variant), "BucketVariantTag": int32(b.bucketVariant), "Age": int32(b.age), "PuffState": int32(b.puffState), "FromBucket": byte(1)}
	if b.id == "minecraft:tadpole" || b.id == "minecraft:axolotl" {
		data["AgeLocked"] = boolByte(b.breeding.ageLocked)
	}
	switch bucket := stack.Item().(type) {
	case item.FishBucket:
		bucket.Data = data
		return item.NewStack(bucket, 1)
	case item.MobBucket:
		bucket.Data = data
		return item.NewStack(bucket, 1)
	default:
		return item.Stack{}
	}
}
func (a *AquaticMob) LoadFromBucket(stack item.Stack) {
	var data map[string]any
	switch bucket := stack.Item().(type) {
	case item.FishBucket:
		data = bucket.Data
	case item.MobBucket:
		data = bucket.Data
	}
	if data == nil {
		return
	}
	b := a.behaviour()
	b.variant = int(nbtconv.Int32(data, "Variant"))
	b.bucketVariant = int(nbtconv.Int32(data, "BucketVariantTag"))
	b.age = int(nbtconv.Int32(data, "Age"))
	b.puffState = int(nbtconv.Int32(data, "PuffState"))
	b.breeding.ageLocked = nbtconv.Bool(data, "AgeLocked")
	if _, ok := data["Health"]; ok {
		b.health.AddHealth(float64(nbtconv.Float32(data, "Health")) - b.health.Health())
	}
	b.fromBucket = true
}
func (a *AquaticMob) BucketPickupSound() world.Sound {
	switch a.behaviour().id {
	case "minecraft:axolotl":
		return sound.Custom{Name: "item.bucket.fill_axolotl", Volume: 1, Pitch: 1}
	case "minecraft:tadpole":
		return sound.Custom{Name: "item.bucket.fill_tadpole", Volume: 1, Pitch: 1}
	default:
		return sound.Custom{Name: "item.bucket.fill_fish", Volume: 1, Pitch: 1}
	}
}
func (a *AquaticMob) FromBucket() bool {
	return a.behaviour().id == "minecraft:tadpole" || a.behaviour().fromBucket
}
func (a *AquaticMob) SetFromBucket(v bool) {
	if a.behaviour().id != "minecraft:tadpole" {
		a.behaviour().fromBucket = v
	}
}
func (a *AquaticMob) viewAction(action world.EntityAction) {
	vs := a.tx.Viewers(a.Position())
	for _, v := range vs {
		v.ViewEntityAction(a, action)
	}
	a.tx.ReleaseViewers(vs)
}
func NewAxolotl(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(AxolotlType, AquaticMobConfig{ID: "minecraft:axolotl", MaxHealth: 14})
}
func NewCod(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(CodType, AquaticMobConfig{ID: "minecraft:cod", MaxHealth: 3})
}
func NewPufferfish(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(PufferfishType, AquaticMobConfig{ID: "minecraft:pufferfish", MaxHealth: 3})
}
func NewSalmon(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(SalmonType, AquaticMobConfig{ID: "minecraft:salmon", MaxHealth: 3, Variant: 1})
}
func NewTadpole(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(TadpoleType, AquaticMobConfig{ID: "minecraft:tadpole", MaxHealth: 6})
}
func NewTropicalFish(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(TropicalFishType, AquaticMobConfig{ID: "minecraft:tropical_fish", MaxHealth: 3})
}

var _ world.EntityType = aquaticMobType{}
var _ Bucketable = (*AquaticMob)(nil)
var _ Living = (*AquaticMob)(nil)
