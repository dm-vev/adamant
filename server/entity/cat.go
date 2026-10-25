package entity

import (
	"encoding/binary"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// CatType is the Java minecraft:cat entity type.
var CatType catType

type catType struct{}

func (catType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		CatBehaviourConfig{}.Apply(d)
	}
	return &Cat{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (catType) EncodeEntity() string        { return "minecraft:cat" }
func (catType) BBox(world.Entity) cube.BBox { return cube.Box(-.3, 0, -.3, .3, .7, .3) }
func (catType) DecodeNBT(m map[string]any, d *world.EntityData) {
	c := CatBehaviourConfig{}
	javaState := ReadNativeMobJavaState(m)
	source := m
	if javaState != nil {
		source = javaState
	}
	if _, ok := source["Health"]; ok {
		c.Health = float64(nbtconv.Float32(source, "Health"))
		c.healthSet = true
	} else if h, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.healthSet = h.Current, true
	}
	if _, ok := source["MaxHealth"]; ok {
		c.MaxHealth = float64(nbtconv.Float32(source, "MaxHealth"))
	} else if h, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.MaxHealth = h.Base
	}
	c.OwnerUUID = catReadOwner(source["Owner"])
	if c.OwnerUUID == "" {
		c.OwnerUUID = nbtconv.String(source, "OwnerUUID")
	}
	c.Tamed = nbtconv.Bool(source, "Tame") || c.OwnerUUID != ""
	c.Sitting = nbtconv.Bool(source, "Sitting")
	if v, ok := source["variant"].(string); ok {
		c.Variant = catVariantFromName(v)
		c.variantSet = true
	} else if v, ok := NativeMobVariant(m); ok && v >= 0 && int(v) < len(bedrockCatVariants) {
		c.Variant = int(bedrockCatVariants[v])
		c.variantSet = true
	}
	if _, ok := source["CatType"]; ok {
		c.Variant = int(nbtconv.Int32(source, "CatType"))
		c.variantSet = true
	}
	// Java saves a byte; earlier Adamant versions an int.
	if collar, ok := nbtconv.Integer(source, "CollarColor"); ok {
		c.CollarColor = uint8(collar)
		c.collarSet = true
	}
	if value, ok := source["Age"]; ok {
		c.AgeTicks = int(nbtconv.Int32(map[string]any{"Age": value}, "Age"))
	}
	if value, ok := source["sound_variant"].(string); ok {
		c.SoundVariant = strings.TrimPrefix(value, "minecraft:")
	}
	d.Data = c.New()
	d.Data.(*CatBehaviour).breeding.decodeNBT(source)
}
func (catType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*CatBehaviour)
	health := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: health.MaxHealth(), Current: health.Health(), DefaultMin: 0, DefaultMax: health.MaxHealth(), Min: 0, Max: health.MaxHealth()}, {Name: "minecraft:movement", Base: .3, Current: .3, DefaultMin: 0, DefaultMax: .3, Min: 0, Max: .3}, {Name: "minecraft:attack_damage", Base: 3, Current: 3, DefaultMin: 0, DefaultMax: 3, Min: 0, Max: 3}})
	native := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attrs, "Variant": bedrockCatVariants[b.variant]})
	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), b.breeding.encodeNBT(map[string]any{"Age": int32(b.ageTicks), "Health": float32(health.Health()), "MaxHealth": float32(health.MaxHealth()), "Tame": byte(catBool(b.tamed)), "Sitting": byte(catBool(b.orderedToSit)), "variant": "minecraft:" + catVariantNames[b.variant], "sound_variant": "minecraft:" + b.soundVariant, "CollarColor": b.collarColor}))
	if owner := catOwnerNBT(b.ownerUUID); owner != nil {
		javaState["Owner"] = owner
	} else {
		delete(javaState, "Owner")
	}
	return MergeNativeMobJavaState(native, javaState)
}
func catOwnerNBT(owner string) []int32 {
	id, err := uuid.Parse(owner)
	if err != nil {
		return nil
	}
	b := id[:]
	return []int32{int32(binary.BigEndian.Uint32(b[:4])), int32(binary.BigEndian.Uint32(b[4:8])), int32(binary.BigEndian.Uint32(b[8:12])), int32(binary.BigEndian.Uint32(b[12:16]))}
}
func catReadOwner(raw any) string {
	if s, ok := raw.(string); ok {
		id, err := uuid.Parse(s)
		if err == nil {
			return id.String()
		}
		return ""
	}
	parts, ok := raw.([]int32)
	if !ok || len(parts) != 4 {
		return ""
	}
	var b [16]byte
	for i, p := range parts {
		binary.BigEndian.PutUint32(b[i*4:], uint32(p))
	}
	id, err := uuid.FromBytes(b[:])
	if err != nil {
		return ""
	}
	return id.String()
}

var catVariantNames = []string{"tabby", "black", "red", "siamese", "british_shorthair", "calico", "persian", "ragdoll", "white", "jellie", "all_black"}

// bedrockCatVariants holds the Bedrock variant of each Java cat variant. The
// mapping is its own inverse.
var bedrockCatVariants = [...]int32{8, 1, 2, 3, 4, 5, 6, 7, 0, 10, 9}

func catVariantFromName(s string) int {
	s = strings.TrimPrefix(s, "minecraft:")
	for i, n := range catVariantNames {
		if n == s {
			return i
		}
	}
	return 1
}
func catBool(v bool) byte {
	if v {
		return 1
	}
	return 0
}

type CatBehaviourConfig struct {
	Health, MaxHealth                float64
	Tamed, Sitting                   bool
	OwnerUUID                        string
	CollarColor                      uint8
	Variant, AgeTicks                int
	SoundVariant                     string
	healthSet, collarSet, variantSet bool
}

func (c CatBehaviourConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c CatBehaviourConfig) New() *CatBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 10
	}
	if !c.healthSet && c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	if !c.collarSet {
		c.CollarColor = 14
	}
	if !c.variantSet {
		c.Variant = 1
	}
	c.Variant = max(0, min(c.Variant, len(catVariantNames)-1))
	soundVariant := strings.TrimPrefix(c.SoundVariant, "minecraft:")
	if soundVariant != "royal" {
		soundVariant = "classic"
	}
	return &CatBehaviour{health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager(), speed: .3,
		tamable: tamable{tamed: c.Tamed, ownerUUID: c.OwnerUUID, orderedToSit: c.Sitting, inSittingPose: c.Sitting},
		variant: c.Variant, collarColor: c.CollarColor, ageTicks: c.AgeTicks, soundVariant: soundVariant}
}

type CatBehaviour struct {
	health       *HealthManager
	effects      *EffectManager
	speed        float64
	dead         bool
	ageTicks     int
	soundVariant string
	variant      int
	collarColor  uint8
	breeding     breeding
	animalAI
	tamable
	tempt *catTemptGoal
	// lying and relaxStateOne are the IS_LYING and RELAX_STATE_ONE of Cat,
	// crouching and sprinting the pose its speed gives it.
	lying, relaxStateOne, crouching, sprinting bool
	shown                                      catShownState
}

// catShownState are the flags of a cat viewers were last shown.
type catShownState struct{ sitting, lying, crouching, sprinting bool }

func (c *CatBehaviour) Variant() int32 { return int32(c.variant) }
func (c *CatBehaviour) Baby() bool     { return c.ageTicks < 0 }

// SoundVariant returns the name of the sounds the cat makes.
func (c *CatBehaviour) SoundVariant() string { return c.soundVariant }

type Cat struct{ *Ent }

func NewCat(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(CatType, CatBehaviourConfig{})
}

// Hiss makes the cat hiss, as cats hiss at phantoms that come near.
func (c *Cat) Hiss(tx *world.Tx) {
	tx.PlaySound(c.Position(), sound.Custom{Name: "entity.cat.hiss", Volume: 1, Pitch: 1})
}

func (c *Cat) SpeciesType() world.EntityType { return CatType }
func (c *Cat) b() *CatBehaviour              { return c.Behaviour().(*CatBehaviour) }
func (c *Cat) Tick(tx *world.Tx, n int64)    { c.b().effects.Tick(c, tx); c.Ent.Tick(tx, n) }
func (b *CatBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	c := &Cat{Ent: e}
	b.ai.MovementSpeed, b.ai.EyeHeight = b.speed, .35
	if b.ageTicks < 0 {
		b.ai.EyeHeight = .2125
	}
	mv := animalTick(e, tx, c, &b.animalAI, b.registerGoals)
	if shown := (catShownState{b.inSittingPose, b.lying, b.crouching, b.sprinting}); shown != b.shown {
		b.shown = shown
		c.updateState()
	}
	return mv
}
func (c *Cat) Health() float64        { return c.b().health.Health() }
func (c *Cat) MaxHealth() float64     { return c.b().health.MaxHealth() }
func (c *Cat) SetMaxHealth(v float64) { c.b().health.SetMaxHealth(v) }
func (c *Cat) Dead() bool             { return c.b().dead || c.Health() <= 0 }

// Panicking reports whether the cat runs from danger.
func (c *Cat) Panicking() bool { return c.b().panicking() }
func (c *Cat) Hurt(d float64, s world.DamageSource) (float64, bool) {
	b := c.b()
	if c.Dead() || d <= 0 || (s.Fire() && c.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	d, ok := b.ai.DamageCooldown(c.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	b.health.AddHealth(-d)
	b.ai.Hurt(s)
	resetLove(c)
	c.viewAction(HurtAction{})
	onMobHurt(c.tx, c)
	if c.Dead() {
		c.die(s)
	}
	return d, true
}
func (c *Cat) Heal(h float64, _ world.HealingSource) float64 {
	if c.Dead() || h <= 0 {
		return 0
	}
	old := c.Health()
	c.b().health.AddHealth(h)
	return c.Health() - old
}
func (c *Cat) KnockBack(s mgl64.Vec3, f, h float64) {
	knockBack(c, s, f, h)
}
func (c *Cat) AddEffect(e effect.Effect)    { c.b().effects.Add(e, c) }
func (c *Cat) RemoveEffect(t effect.Type)   { c.b().effects.Remove(t, c) }
func (c *Cat) Effects() []effect.Effect     { return c.b().effects.Effects() }
func (c *Cat) Speed() float64               { return c.b().speed }
func (c *Cat) SetSpeed(v float64)           { c.b().speed = v }
func (c *Cat) hasEffect(t effect.Type) bool { _, ok := c.b().effects.Effect(t); return ok }
func (c *Cat) Tamed() bool                  { return c.b().tamed }
func (c *Cat) OwnerUUID() string            { return c.b().ownerUUID }
func (c *Cat) Variant() int                 { return c.b().variant }
func (c *Cat) BedrockVariant() int32        { return bedrockCatVariants[c.b().variant] }
func (c *Cat) SetVariant(v int) {
	c.b().variant = max(0, min(v, len(catVariantNames)-1))
	c.updateState()
}
func (c *Cat) CollarColor() uint8     { return c.b().collarColor }
func (c *Cat) SetCollarColor(v uint8) { c.b().collarColor = v % 16; c.updateState() }
func (c *Cat) Sitting() bool          { return c.b().inSittingPose }
func (c *Cat) SetSitting(v bool)      { c.b().orderedToSit = v }

// Lying reports if the cat lies down on a bed, a furnace or its sleeping
// owner's bed.
func (c *Cat) Lying() bool { return c.b().lying }

// Crouching reports if the cat creeps, as it does while tempted.
func (c *Cat) Crouching() bool { return c.b().crouching }

// TameWithFish makes the Java one-in-three fish-taming roll and records the owner.
func (c *Cat) TameWithFish(tx *world.Tx, user world.Entity) bool {
	b := c.b()
	if b.tamed {
		return false
	}
	if b.ai.Mob(c.Ent, c).Random().NextInt(3) == 0 {
		// Cat.tryToTame: a tamed cat sits down.
		b.tamed, b.orderedToSit = true, true
		b.owner, b.ownerUUID = user.H(), user.H().UUID().String()
		c.updateState()
		c.viewAction(TameAction{Succeeded: true})
	} else {
		c.viewAction(TameAction{})
	}
	return true
}

// AcceptItem ports Cat.mobInteract: its owner dyes its collar, heals it with
// fish while hurt or makes it sit or stand, fish tames a wild cat, and fish
// otherwise breeds a cat or lets a kitten grow up faster.
func (c *Cat) AcceptItem(from world.Entity, tx *world.Tx, ctx *item.UseContext) bool {
	u, ok := from.(item.User)
	if !ok {
		return false
	}
	main, _ := u.HeldItems()
	b := c.b()
	fish := !main.Empty() && catFish(main.Item())
	if b.tamed && b.ownerUUID == from.H().UUID().String() {
		if d, ok := main.Item().(item.Dye); ok && !main.Empty() {
			if d.Colour.Uint8() != b.collarColor {
				c.SetCollarColor(d.Colour.Uint8())
				ctx.SubtractFromCount(1)
				return true
			}
		} else if fish && c.Health() < c.MaxHealth() {
			c.Heal(2, nil)
			ctx.SubtractFromCount(1)
			return true
		}
		if feedBreeder(c, u, ctx) {
			return true
		}
		c.SetSitting(!b.orderedToSit)
		return true
	}
	if fish && !b.tamed {
		ctx.SubtractFromCount(1)
		c.TameWithFish(tx, from)
		return true
	}
	return feedBreeder(c, u, ctx)
}
func catFish(i world.Item) bool {
	switch v := i.(type) {
	case item.Cod:
		return !v.Cooked
	case item.Salmon:
		return !v.Cooked
	}
	return false
}
func (c *Cat) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return c.AcceptItem(u, tx, ctx)
}

// InLove reports whether the cat is in love.
func (c *Cat) InLove() bool { return c.b().breeding.InLove() }

func (c *Cat) breedingState() *breeding { return &c.b().breeding }
func (c *Cat) ageTicks() *int           { return &c.b().ageTicks }

// breedingFood reports whether the stack is in #cat_food.
func (c *Cat) breedingFood(stack item.Stack) bool { return !stack.Empty() && catFish(stack.Item()) }

// canMateWith ports Cat.canMate: only tamed cats breed.
func (c *Cat) canMateWith(partner breeder) bool {
	return c.b().tamed && partner.(*Cat).b().tamed
}

// breedOffspring ports Cat.getBreedOffspring: the kitten takes the variant
// of either parent, and the kitten of a tamed cat belongs to its owner with
// the collar colours of the parents mixed.
func (c *Cat) breedOffspring(_ *world.Tx, partner breeder) *world.EntityHandle {
	b, p := c.b(), partner.(*Cat).b()
	conf := CatBehaviourConfig{AgeTicks: babyStartAge, Variant: randomParent(b.variant, p.variant), variantSet: true}
	if b.tamed {
		collar := mixedDyeColour(item.Colours()[b.collarColor%16], item.Colours()[p.collarColor%16])
		conf.Tamed, conf.OwnerUUID, conf.CollarColor, conf.collarSet = true, b.ownerUUID, collar.Uint8(), true
	}
	return babyAt(c).New(CatType, conf)
}
func (c *Cat) viewAction(a world.EntityAction) {
	vs := c.tx.Viewers(c.Position())
	for _, v := range vs {
		v.ViewEntityAction(c, a)
	}
	c.tx.ReleaseViewers(vs)
}
func (c *Cat) die(src world.DamageSource) {
	b := c.b()
	if b.dead {
		return
	}
	b.dead = true
	c.viewAction(DeathAction{})
	dropAllDeathLoot(c.tx, c, src)
	_ = c.CloseIn(c.tx)
}

var _ Living = (*Cat)(nil)
var _ ItemAcceptor = (*Cat)(nil)
