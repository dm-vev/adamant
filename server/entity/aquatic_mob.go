package entity

import (
	"maps"
	"time"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/entity/effect"
	"github.com/dm-vev/adamant/server/internal/nbtconv"
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

var (
	axolotlType      = aquaticMobType{id: "minecraft:axolotl", width: .75, height: .42, health: 14}
	codType          = aquaticMobType{id: "minecraft:cod", width: .5, height: .3, health: 3}
	pufferfishType   = aquaticMobType{id: "minecraft:pufferfish", width: .7, height: .7, health: 3}
	salmonType       = aquaticMobType{id: "minecraft:salmon", width: .7, height: .4, health: 3}
	tadpoleType      = aquaticMobType{id: "minecraft:tadpole", width: .4, height: .3, health: 6}
	tropicalFishType = aquaticMobType{id: "minecraft:tropical_fish", width: .5, height: .4, health: 3}
)

type aquaticMobType struct {
	id                    string
	width, height, health float64
}

func (t aquaticMobType) EncodeEntity() string { return t.id }
func (t aquaticMobType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	conf := aquaticMobConfig{ID: t.id, MaxHealth: t.health}
	if t.id == "minecraft:salmon" {
		conf.Variant = 1
	}
	if data.Data == nil {
		conf.Apply(data)
	}
	if behaviour := data.Data.(*aquaticMobBehaviour); h != nil && !behaviour.randomSeeded {
		behaviour.random, behaviour.randomSeeded = openRandom(tx, h), true
	}
	base := &aquaticMob{Ent: &Ent{tx: tx, handle: h, data: data}}
	switch t.id {
	case "minecraft:axolotl":
		return &Axolotl{aquaticMob: base}
	case "minecraft:cod":
		return &cod{aquaticMob: base}
	case "minecraft:pufferfish":
		return &pufferfish{aquaticMob: base}
	case "minecraft:salmon":
		return &Salmon{aquaticMob: base}
	case "minecraft:tadpole":
		return &tadpole{aquaticMob: base}
	default:
		return &TropicalFish{aquaticMob: base}
	}
}
func (t aquaticMobType) BBox(e world.Entity) cube.BBox {
	w, h := t.width, t.height
	if t.id == "minecraft:axolotl" && e != nil {
		if mob, ok := e.(interface{ behaviour() *aquaticMobBehaviour }); ok && mob.behaviour().age < 0 {
			// Axolotl.BABY_DIMENSIONS.
			w, h = .375, .21
		}
	}
	if t.id == "minecraft:pufferfish" && e != nil {
		if mob, ok := e.(interface{ behaviour() *aquaticMobBehaviour }); ok {
			var scale float64
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
		if mob, ok := e.(interface{ behaviour() *aquaticMobBehaviour }); ok {
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
		if mob, ok := e.(interface{ behaviour() *aquaticMobBehaviour }); ok && mob.behaviour().age < 0 {
			w, h = .375, .21
		}
	}
	return cube.Box(-w/2, 0, -w/2, w/2, h, w/2)
}
func aquaticWorldEntity(e *Ent) world.Entity {
	base := &aquaticMob{Ent: e}
	switch base.behaviour().id {
	case "minecraft:axolotl":
		return &Axolotl{aquaticMob: base}
	case "minecraft:cod":
		return &cod{aquaticMob: base}
	case "minecraft:pufferfish":
		return &pufferfish{aquaticMob: base}
	case "minecraft:salmon":
		return &Salmon{aquaticMob: base}
	case "minecraft:tadpole":
		return &tadpole{aquaticMob: base}
	default:
		return &TropicalFish{aquaticMob: base}
	}
}
func (t aquaticMobType) DecodeNBT(nbt map[string]any, data *world.EntityData) {
	c := aquaticMobConfig{ID: t.id, MaxHealth: t.health}
	if t.id == "minecraft:salmon" {
		c.Variant = 1
	}
	if _, ok := nbt["Health"]; ok {
		c.Health = float64(nbtconv.Float32(nbt, "Health"))
		c.healthSet = true
	}
	if extra, ok := aquaticExtraState(nbt); ok {
		if _, ok := extra["Variant"]; ok {
			c.Variant = int(nbtconv.Int32(extra, "Variant"))
		}
		if _, ok := extra["BucketVariantTag"]; ok {
			c.BucketVariant = int(nbtconv.Int32(extra, "BucketVariantTag"))
		}
		if _, ok := extra["PuffState"]; ok {
			c.PuffState = int(nbtconv.Int32(extra, "PuffState"))
		}
	} else {
		c.Variant, c.PuffState = aquaticVariantFromNative(t.id, nbt, c.Variant)
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
		data.Data.(*aquaticMobBehaviour).breeding.decodeNBT(nbt)
	}
}
func (aquaticMobType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*aquaticMobBehaviour)
	m := map[string]any{"Health": float32(b.health.Health()), "Age": int32(b.age), "FromBucket": boolByte(b.id == "minecraft:tadpole" || b.fromBucket)}
	maps.Copy(m, b.bedrockVariantNBT())
	m["adamant:state"] = map[string]any{"Variant": int32(b.variant), "BucketVariantTag": int32(b.bucketVariant), "PuffState": int32(b.puffState)}
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
func (b *aquaticMobBehaviour) bedrockVariantNBT() map[string]any {
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
// draws the mob by, which the world format saves and Bedrock clients are
// sent.
func (a *aquaticMob) BedrockVariant() (variant, mark int32, color, color2 byte) {
	m := a.behaviour().bedrockVariantNBT()
	return m["Variant"].(int32), m["MarkVariant"].(int32), m["Color"].(byte), m["Color2"].(byte)
}

// aquaticJavaState returns the map that holds the Java variant fields: the
// Java compound of a native save, or the top level of Java NBT and of saves
// from before Adamant wrote the Bedrock fields, which alone have PuffState
// at the top level.
func aquaticExtraState(nbt map[string]any) (map[string]any, bool) {
	if extra := readNativeMobExtraState(nbt); extra != nil {
		if _, ok := extra["Variant"]; ok {
			return extra, true
		}
	}
	_, hasCanonicalID := nbt["id"].(string)
	_, legacy := nbt["PuffState"]
	return nbt, hasCanonicalID || legacy
}

// javaAquaticVariant converts the variant fields BDS saves to the Java
// variant and puff state, keeping def for mobs BDS saves no variant of.
func aquaticVariantFromNative(id string, nbt map[string]any, def int) (variant, puffState int) {
	v, _ := nativeMobVariant(nbt)
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
type aquaticMobConfig struct {
	ID                                     string
	Health, MaxHealth                      float64
	Variant, BucketVariant, Age, PuffState int
	AgeLocked, fromBucket, healthSet       bool
}

func (c aquaticMobConfig) Apply(data *world.EntityData) { data.Data = c.New() }
func (c aquaticMobConfig) New() *aquaticMobBehaviour {
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
	b := &aquaticMobBehaviour{health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), id: c.ID, variant: c.Variant, bucketVariant: c.BucketVariant, age: c.Age, puffState: c.PuffState, breeding: breeding{ageLocked: c.AgeLocked}, fromBucket: c.fromBucket, speed: speed, schoolSize: 1, random: vanilla.NewLegacyRandom(int64(c.Variant))}
	switch c.ID {
	case "minecraft:tadpole":
		b.br = newTadpoleBrain()
	case "minecraft:axolotl":
		b.br = newAxolotlBrain()
	}
	return b
}

type aquaticMobBehaviour struct {
	health                                           *HealthManager
	effects                                          *EffectManager
	id                                               string
	variant, bucketVariant, age, puffState, airTicks int
	inflateCounter, deflateTimer                     int
	fromBucket, dead                                 bool
	// playingDead is Axolotl.DATA_PLAYING_DEAD.
	playingDead bool
	// lone is the inverse of TropicalFish.isSchool: a fish of a rare variant
	// that spawned outside a school.
	lone bool
	// breeding is the love and growth state of axolotls; tadpoles only use
	// its age lock.
	breeding breeding
	// ai is the MobAI the mob moves with.
	ai MobAI
	// br is the brain of an axolotl, or the one a tadpole runs besides its
	// goals.
	br     *brain
	panic  *panicGoal
	speed  float64
	random vanilla.Random
	// randomSeeded is set once random has been seeded.
	randomSeeded bool
	// leader, leaderState and schoolSize are those of Java's
	// AbstractSchoolingFish: the fish followed and the size of the school a
	// leader leads.
	leader      *world.EntityHandle
	leaderState *aquaticMobBehaviour
	schoolSize  int
}
type aquaticMob struct{ *Ent }
type Axolotl struct{ *aquaticMob }

// Rehydrate ports Axolotl.rehydrate: a splash of water gives an axolotl out
// of water 1800 more ticks of air, up to its maximum.
func (a *Axolotl) Rehydrate() {
	b := a.behaviour()
	b.airTicks = max(b.airTicks-1800, 0)
}

type cod struct{ *aquaticMob }
type pufferfish struct{ *aquaticMob }
type Salmon struct{ *aquaticMob }
type tadpole struct{ *aquaticMob }
type TropicalFish struct{ *aquaticMob }

func (a *aquaticMob) behaviour() *aquaticMobBehaviour { return a.Behaviour().(*aquaticMobBehaviour) }
func (a *aquaticMob) Tick(tx *world.Tx, current int64) {
	a.behaviour().effects.Tick(a, tx)
	a.Ent.Tick(tx, current)
}
func (b *aquaticMobBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.dead {
		return nil
	}
	if b.id == "minecraft:axolotl" {
		return b.tickAxolotl(e, tx)
	}
	return b.tickFish(e, tx)
}

func (b *aquaticMobBehaviour) viewPuffState(e *Ent) {
	world.BroadcastEntityState(e.tx, aquaticWorldEntity(e))
}
func (a *aquaticMob) Health() float64        { return a.behaviour().health.Health() }
func (a *aquaticMob) MaxHealth() float64     { return a.behaviour().health.MaxHealth() }
func (a *aquaticMob) SetMaxHealth(h float64) { a.behaviour().health.SetMaxHealth(h) }
func (a *aquaticMob) Dead() bool             { return a.behaviour().dead || a.Health() <= 0 }
func (a *aquaticMob) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if a.Dead() || d <= 0 {
		return 0, false
	}
	b := a.behaviour()
	old := a.Health()
	if b.id == "minecraft:axolotl" && !b.ai.NoAI && b.ai.started {
		// Axolotl.hurtServer: a hurt axolotl in water may play dead.
		m := b.ai.Mob(a.Ent, aquaticWorldEntity(a.Ent))
		r := m.Random()
		if r.NextInt(3) == 0 && (float64(r.NextInt(3)) < d || old/a.MaxHealth() < .5) && d < old && m.InWater() && aquaticDamageHasAttacker(src) && !b.playingDead {
			b.br.set(memPlayDeadTicks, 200)
		}
	}
	var ok bool
	if d, ok = b.ai.DamageCooldown(a.tx, d); !ok {
		return 0, false
	}
	b.ai.Hurt(src)
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
func (a *aquaticMob) Heal(h float64, _ world.HealingSource) float64 {
	if a.Dead() || h <= 0 {
		return 0
	}
	old := a.Health()
	a.behaviour().health.AddHealth(h)
	return a.Health() - old
}
func (a *aquaticMob) KnockBack(src mgl64.Vec3, force, height float64) {
	knockBack(a, src, force, height)
}
func (a *aquaticMob) AddEffect(e effect.Effect)  { a.behaviour().effects.Add(e, a) }
func (a *aquaticMob) RemoveEffect(t effect.Type) { a.behaviour().effects.Remove(t, a) }
func (a *aquaticMob) Effects() []effect.Effect   { return a.behaviour().effects.Effects() }
func (a *aquaticMob) Variant() int32 {
	b := a.behaviour()
	if b.id == "minecraft:pufferfish" {
		return int32(b.puffState)
	}
	return int32(b.variant)
}
func (a *aquaticMob) Baby() bool      { b := a.behaviour(); return b.id == "minecraft:axolotl" && b.age < 0 }
func (a *aquaticMob) Breathing() bool { return a.AirSupply() > 0 }
func (a *aquaticMob) AirSupply() time.Duration {
	b := a.behaviour()
	ticks := 300
	if b.id == "minecraft:axolotl" {
		ticks = 6000
	}
	return time.Duration(max(ticks-b.airTicks, 0)) * time.Second / 20
}
func (a *aquaticMob) MaxAirSupply() time.Duration {
	if a.behaviour().id == "minecraft:axolotl" {
		return 6000 * time.Second / 20
	}
	return 300 * time.Second / 20
}
func (a *aquaticMob) Speed() float64     { return a.behaviour().speed }
func (a *aquaticMob) SetSpeed(v float64) { a.behaviour().speed = v }
func (a *aquaticMob) CanBePickedUpWithBucket(bucket item.Bucket) bool {
	liquid, ok := bucket.Content.Liquid()
	return ok && liquid.LiquidType() == "water" && !a.Dead()
}
func (a *aquaticMob) BucketItem() item.Stack {
	b := a.behaviour()
	m := item.MobBucket{Entity: b.id}
	switch b.id {
	case "minecraft:cod", "minecraft:pufferfish", "minecraft:salmon", "minecraft:tropical_fish":
		return item.NewStack(item.FishBucket{MobBucket: m}, 1)
	default:
		return item.NewStack(m, 1)
	}
}
func (a *aquaticMob) SaveToBucket(stack item.Stack) item.Stack {
	b := a.behaviour()
	data := map[string]any{"Health": float32(b.health.Health()), "Variant": int32(b.variant), "BucketVariantTag": int32(b.bucketVariant), "Age": int32(b.age), "PuffState": int32(b.puffState), "FromBucket": byte(1)}
	if b.id == "minecraft:tadpole" || b.id == "minecraft:axolotl" {
		data["AgeLocked"] = boolByte(b.breeding.ageLocked)
	}
	if b.id == "minecraft:axolotl" && b.br.has(memHasHuntingCooldown) {
		// Axolotl.saveToBucketTag.
		data["HuntingCooldown"] = b.br.timeUntilExpiry(memHasHuntingCooldown)
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
func (a *aquaticMob) LoadFromBucket(stack item.Stack) {
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
	if b.id == "minecraft:axolotl" {
		// Axolotl.loadFromBucketTag.
		if _, ok := data["HuntingCooldown"]; ok {
			b.br.setWithExpiry(memHasHuntingCooldown, true, nbtconv.Int64(data, "HuntingCooldown"))
		} else {
			b.br.erase(memHasHuntingCooldown)
		}
	}
	if _, ok := data["Health"]; ok {
		b.health.AddHealth(float64(nbtconv.Float32(data, "Health")) - b.health.Health())
	}
	b.fromBucket = true
}
func (a *aquaticMob) BucketPickupSound() world.Sound {
	switch a.behaviour().id {
	case "minecraft:axolotl":
		return sound.Custom{Name: "item.bucket.fill_axolotl", Volume: 1, Pitch: 1}
	case "minecraft:tadpole":
		return sound.Custom{Name: "item.bucket.fill_tadpole", Volume: 1, Pitch: 1}
	default:
		return sound.Custom{Name: "item.bucket.fill_fish", Volume: 1, Pitch: 1}
	}
}
func (a *aquaticMob) FromBucket() bool {
	return a.behaviour().id == "minecraft:tadpole" || a.behaviour().fromBucket
}
func (a *aquaticMob) SetFromBucket(v bool) {
	if a.behaviour().id != "minecraft:tadpole" {
		a.behaviour().fromBucket = v
	}
}
func (a *aquaticMob) viewAction(action world.EntityAction) {
	world.BroadcastEntityAction(a.tx, a, action)
}
func NewAxolotl(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(axolotlType, aquaticMobConfig{ID: "minecraft:axolotl", MaxHealth: 14})
}
func NewCod(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(codType, aquaticMobConfig{ID: "minecraft:cod", MaxHealth: 3})
}
func NewPufferfish(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(pufferfishType, aquaticMobConfig{ID: "minecraft:pufferfish", MaxHealth: 3})
}
func NewSalmon(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(salmonType, aquaticMobConfig{ID: "minecraft:salmon", MaxHealth: 3, Variant: 1})
}
func NewTadpole(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(tadpoleType, aquaticMobConfig{ID: "minecraft:tadpole", MaxHealth: 6})
}
func NewTropicalFish(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(tropicalFishType, aquaticMobConfig{ID: "minecraft:tropical_fish", MaxHealth: 3})
}

var _ world.EntityType = aquaticMobType{}
var _ Bucketable = (*aquaticMob)(nil)
var _ Living = (*aquaticMob)(nil)

func (b *aquaticMobBehaviour) effectManager() *EffectManager { return b.effects }

// pushedByFluid ports the AbstractFish and Axolotl overrides of
// Entity.isPushedByFluid.
func (*aquaticMobBehaviour) pushedByFluid(*Mob) bool { return false }

func (*aquaticMobBehaviour) keepsOwnAirSupply() {}
