package entity

import (
	"hash/fnv"
	"maps"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
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
		behaviour.random = vanilla.NewLegacyRandom(int64(hash.Sum64()))
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
	if java, ok := aquaticJavaState(nbt); ok {
		if _, ok := java["Variant"]; ok {
			c.Variant = int(nbtconv.Int32(java, "Variant"))
		}
		if _, ok := java["BucketVariantTag"]; ok {
			c.BucketVariant = int(nbtconv.Int32(java, "BucketVariantTag"))
		}
		if _, ok := java["PuffState"]; ok {
			c.PuffState = int(nbtconv.Int32(java, "PuffState"))
		}
	} else {
		c.Variant, c.PuffState = javaAquaticVariant(t.id, nbt, c.Variant)
	}
	if _, ok := nbt["Age"]; ok {
		c.Age = int(nbtconv.Int32(nbt, "Age"))
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
	m := map[string]any{"Health": float32(b.health.Health()), "Age": int32(b.age), "FromBucket": boolByte(b.id == "minecraft:tadpole" || b.fromBucket)}
	maps.Copy(m, b.bedrockVariantNBT())
	m["adamant:java"] = map[string]any{"Variant": int32(b.variant), "BucketVariantTag": int32(b.bucketVariant), "PuffState": int32(b.puffState)}
	switch b.id {
	case "minecraft:axolotl":
		b.breeding.encodeNBT(m)
	case "minecraft:tadpole":
		m["AgeLocked"] = boolByte(b.breeding.ageLocked)
	}
	return m
}

// bedrockAxolotlVariants holds the Bedrock variant of each Java axolotl
// variant. The mapping is its own inverse.
var bedrockAxolotlVariants = [...]int32{0, 3, 2, 1, 4}

// bedrockVariantNBT returns the variant fields BDS saves for the mob,
// converted from the Java variant kept.
func (b *AquaticMobBehaviour) bedrockVariantNBT() map[string]any {
	m := map[string]any{"Variant": int32(0), "MarkVariant": int32(0), "Color": byte(0), "Color2": byte(0)}
	switch b.id {
	case "minecraft:axolotl":
		m["Variant"] = bedrockAxolotlVariants[max(0, min(b.variant, len(bedrockAxolotlVariants)-1))]
	case "minecraft:tropical_fish":
		// Java packs the size, pattern and both colours in one variant.
		m["Variant"], m["MarkVariant"] = int32(b.variant&0xff), int32(b.variant>>8&0xff)
		m["Color"], m["Color2"] = byte(b.variant>>16), byte(b.variant>>24)
	case "minecraft:pufferfish":
		m["Variant"] = int32(b.puffState)
	}
	return m
}

// BedrockVariant returns the variant, mark variant and colours that Bedrock
// draws the mob by.
func (a *AquaticMob) BedrockVariant() (variant, mark int32, color, color2 byte) {
	m := a.behaviour().bedrockVariantNBT()
	return m["Variant"].(int32), m["MarkVariant"].(int32), m["Color"].(byte), m["Color2"].(byte)
}

// aquaticJavaState returns the map that holds the Java variant fields: the
// Java compound of a native save, or the top level of Java NBT and of saves
// from before Adamant wrote the Bedrock fields, which alone have PuffState
// at the top level.
func aquaticJavaState(nbt map[string]any) (map[string]any, bool) {
	if java := ReadNativeMobJavaState(nbt); java != nil {
		if _, ok := java["Variant"]; ok {
			return java, true
		}
	}
	_, javaNBT := nbt["id"].(string)
	_, legacy := nbt["PuffState"]
	return nbt, javaNBT || legacy
}

// javaAquaticVariant converts the variant fields BDS saves to the Java
// variant and puff state, keeping def for mobs BDS saves no variant of.
func javaAquaticVariant(id string, nbt map[string]any, def int) (variant, puffState int) {
	v, _ := NativeMobVariant(nbt)
	switch id {
	case "minecraft:axolotl":
		if v >= 0 && int(v) < len(bedrockAxolotlVariants) {
			return int(bedrockAxolotlVariants[v]), 0
		}
	case "minecraft:tropical_fish":
		mark, _ := nbtconv.Integer(nbt, "MarkVariant")
		color, _ := nbtconv.Integer(nbt, "Color")
		color2, _ := nbtconv.Integer(nbt, "Color2")
		return int(v&0xff) | int(mark&0xff)<<8 | int(color&0xff)<<16 | int(color2&0xff)<<24, 0
	case "minecraft:pufferfish":
		return def, int(max(0, min(v, 2)))
	case "minecraft:salmon":
		// BDS sizes salmon by component group.
		definitions, _ := nbt["definitions"].([]any)
		for _, d := range definitions {
			switch d {
			case "+scale_small":
				return 0, 0
			case "+scale_large":
				return 2, 0
			}
		}
	}
	return def, 0
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
	// Fish keep the movement speed of 0.7 every Mob has, tadpoles 1.
	speed := .7
	if c.ID == "minecraft:axolotl" || c.ID == "minecraft:tadpole" {
		speed = 1
	}
	b := &AquaticMobBehaviour{health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), id: c.ID, variant: c.Variant, bucketVariant: c.BucketVariant, age: c.Age, puffState: c.PuffState, breeding: breeding{ageLocked: c.AgeLocked}, fromBucket: c.fromBucket, movement: &MovementComputer{Gravity: .08, Drag: .1, DragBeforeGravity: true}, speed: speed, schoolSize: 1, random: vanilla.NewLegacyRandom(int64(c.Variant))}
	if c.ID == "minecraft:tadpole" {
		b.br = newTadpoleBrain()
	}
	return b
}

type AquaticMobBehaviour struct {
	health                                                *HealthManager
	effects                                               *EffectManager
	id                                                    string
	variant, bucketVariant, age, puffState, airTicks      int
	inflateCounter, deflateTimer                          int
	playDeadTicks, attackCooldown, huntingCooldown, ticks int
	fromBucket, dead                                      bool
	// breeding is the love and growth state of axolotls; tadpoles only use
	// its age lock.
	breeding breeding
	target   *world.EntityHandle
	// ai is the MobAI fish swim with. Axolotls move by movement.
	ai MobAI
	// br is the brain a tadpole runs besides its goals.
	br       *Brain
	panic    *PanicGoal
	movement *MovementComputer
	speed    float64
	random   vanilla.Random
	// leader, leaderState and schoolSize are those of Java's
	// AbstractSchoolingFish: the fish followed and the size of the school a
	// leader leads.
	leader      *world.EntityHandle
	leaderState *AquaticMobBehaviour
	schoolSize  int
	// The path an axolotl swims along.
	navPath            []cube.Pos
	navIndex, navTicks int
	navGoal            cube.Pos
	navGoalSet         bool
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
	if b.id != "minecraft:axolotl" {
		return b.tickFish(e, tx)
	}
	pos := cube.PosFromVec3(e.Position())
	_, inWater := tx.Liquid(pos)
	if inWater {
		b.airTicks = 0
	} else {
		b.airTicks++
		if b.airTicks > 6000 && b.airTicks%20 == 0 {
			(&AquaticMob{Ent: e}).Hurt(2, DrowningDamageSource{})
			if b.dead {
				return nil
			}
		}
	}
	partner, inLove := tickBreeding(&Axolotl{&AquaticMob{Ent: e}}, tx)
	vel := b.tickAxolotl(e, tx, e.data.Vel, partner, inLove)
	if inWater {
		b.movement.Gravity = 0
		vel = vel.Mul(.9)
	} else {
		b.movement.Gravity = .08
	}
	if b.playDeadTicks > 0 {
		b.playDeadTicks--
	}
	b.ticks++
	return b.movement.TickMovement(e, e.Position(), vel, e.data.Rot, e.data.Rot, tx).apply(e)
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
	if b.id != "minecraft:axolotl" {
		var ok bool
		if d, ok = b.ai.DamageCooldown(a.tx, d); !ok {
			return 0, false
		}
		b.ai.Hurt(src)
	}
	old := a.Health()
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
