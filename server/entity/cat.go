package entity

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
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
	b := d.Data.(*CatBehaviour)
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(h.UUID().String()))
	b.random = vanilla.NewLegacyRandom(int64(hash.Sum64()))
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
	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), b.breeding.encodeNBT(map[string]any{"Age": int32(b.ageTicks), "Health": float32(health.Health()), "MaxHealth": float32(health.MaxHealth()), "Tame": byte(catBool(b.tamed)), "Sitting": byte(catBool(b.sitting)), "variant": "minecraft:" + catVariantNames[b.variant], "sound_variant": "minecraft:" + b.soundVariant, "CollarColor": b.collarColor}))
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
	return &CatBehaviour{health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, random: vanilla.NewLegacyRandom(0), speed: .3, tamed: c.Tamed, sitting: c.Sitting, ownerUUID: c.OwnerUUID, variant: c.Variant, collarColor: c.CollarColor, ageTicks: c.AgeTicks, soundVariant: soundVariant, nextWander: 1}
}

type CatBehaviour struct {
	health               *HealthManager
	effects              *EffectManager
	mc                   *MovementComputer
	random               vanilla.Random
	speed                float64
	tamed, sitting, dead bool
	ownerUUID            string
	ageTicks             int
	soundVariant         string
	variant              int
	collarColor          uint8
	nextWander           int
	navigator            Navigator
	breeding             breeding
	panic                panicState
}

func (c *CatBehaviour) Variant() int32 { return int32(c.variant) }
func (c *CatBehaviour) Baby() bool     { return c.ageTicks < 0 }

// SoundVariant returns the name of the sounds the cat makes.
func (c *CatBehaviour) SoundVariant() string { return c.soundVariant }

type Cat struct{ *Ent }

func NewCat(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(CatType, CatBehaviourConfig{})
}
func (c *Cat) SpeciesType() world.EntityType { return CatType }
func (c *Cat) b() *CatBehaviour              { return c.Behaviour().(*CatBehaviour) }
func (c *Cat) Tick(tx *world.Tx, n int64)    { c.b().effects.Tick(c, tx); c.Ent.Tick(tx, n) }
func (b *CatBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	panicking := b.panic.run(e, tx, &b.navigator)
	if partner, ok := tickBreeding(&Cat{Ent: e}, tx); panicking {
		b.nextWander = 20
	} else if ok {
		navigateToPartner(&b.navigator, e, tx, partner)
		b.nextWander = 20
	} else if b.tamed && !b.sitting {
		var owner world.Entity
		for p := range tx.Players() {
			if p.H().UUID().String() == b.ownerUUID {
				owner = p
				break
			}
		}
		if owner != nil && owner.Position().Sub(e.Position()).Len() > 5 {
			b.navigator.MoveTo(tx, cube.PosFromVec3(e.Position()), cube.PosFromVec3(owner.Position()), GroundProfile{MaxVisitedNodes: 256}, navigatorTick(e))
			b.nextWander = 20
		}
	} else if !b.tamed {
		var nearest world.Entity
		distance := 256.0
		tempting := false
		for p := range tx.Players() {
			d := p.Position().Sub(e.Position()).Len()
			if d < distance {
				nearest, distance = p, d
				held, ok := p.(item.User)
				if ok {
					main, off := held.HeldItems()
					tempting = catFish(main.Item()) || catFish(off.Item())
				}
			}
		}
		if nearest != nil && tempting && distance < 10 {
			b.navigator.MoveTo(tx, cube.PosFromVec3(e.Position()), cube.PosFromVec3(nearest.Position()), GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e))
			b.nextWander = 20
		} else if nearest != nil && distance < 16 {
			away := e.Position().Sub(nearest.Position())
			away[1] = 0
			if away.Len() > 0 {
				goal := cube.PosFromVec3(e.Position().Add(away.Normalize().Mul(8)))
				b.navigator.MoveTo(tx, cube.PosFromVec3(e.Position()), goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e))
				b.nextWander = 20
			}
		}
	}
	if b.sitting {
		b.navigator.Path, b.navigator.Index = nil, 0
	} else {
		if b.nextWander > 0 {
			b.nextWander--
		}
		if b.navigator.Index >= len(b.navigator.Path) && b.nextWander == 0 {
			b.nextWander = 40 + int(b.random.NextInt(81))
			start := cube.PosFromVec3(e.Position())
			for range 4 {
				goal := start.Add(cube.Pos{int(b.random.NextInt(13)) - 6, 0, int(b.random.NextInt(13)) - 6})
				if b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e)) {
					break
				}
			}
		}
	}
	v := e.data.Vel
	r := e.data.Rot
	speed := b.speed
	if panicking {
		// TamableAnimalPanicGoal at 1.5 times the movement speed.
		speed = groundSpeed(.3, 1.5)
	}
	if p, ok := b.navigator.Tick(e.data.Pos, navigatorTick(e), .6); ok {
		d := p.Sub(e.data.Pos)
		d[1] = 0
		if d.Len() < .25 {
			b.navigator.Index++
		} else {
			d = d.Normalize()
			v[0], v[2] = d[0]*speed, d[2]*speed
			r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
		}
	} else {
		v[0], v[2] = 0, 0
	}
	return b.mc.TickMovement(e, e.data.Pos, v, r, e.data.Rot, tx).apply(e)
}
func (c *Cat) Health() float64        { return c.b().health.Health() }
func (c *Cat) MaxHealth() float64     { return c.b().health.MaxHealth() }
func (c *Cat) SetMaxHealth(v float64) { c.b().health.SetMaxHealth(v) }
func (c *Cat) Dead() bool             { return c.b().dead || c.Health() <= 0 }

// Panicking reports whether the cat runs from danger.
func (c *Cat) Panicking() bool { return c.b().panic.active }
func (c *Cat) Hurt(d float64, s world.DamageSource) (float64, bool) {
	b := c.b()
	if c.Dead() || d <= 0 || (s.Fire() && c.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	b.health.AddHealth(-d)
	b.panic.hurt(s, c.tx.CurrentTick())
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
	d := c.Position().Sub(s)
	d[1] = 0
	if d.Len() != 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	c.SetVelocity(d)
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
func (c *Cat) Sitting() bool          { return c.b().sitting }
func (c *Cat) moveFlagHeld() bool   { return c.b().sitting }
func (c *Cat) SetSitting(v bool)      { c.b().sitting = v; c.b().navigator.Path = nil; c.updateState() }

// TameWithFish makes the Java one-in-three fish-taming roll and records the owner.
func (c *Cat) TameWithFish(tx *world.Tx, user world.Entity) bool {
	b := c.b()
	if b.tamed {
		return false
	}
	if b.random.NextInt(3) == 0 {
		b.tamed = true
		b.ownerUUID = user.H().UUID().String()
		b.sitting = true
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
		c.SetSitting(!b.sitting)
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
