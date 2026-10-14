package entity

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"math/rand/v2"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewWolf creates a worldless wolf handle.
func NewWolf(opts world.EntitySpawnOpts, conf WolfBehaviourConfig) *world.EntityHandle {
	return opts.New(WolfType, conf)
}

var WolfType wolfType

type wolfType struct{}

func (wolfType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		WolfBehaviourConfig{}.Apply(data)
	}
	return &Wolf{Ent: &Ent{tx: tx, handle: handle, data: data}}
}
func (wolfType) EncodeEntity() string        { return "minecraft:wolf" }
func (wolfType) BBox(world.Entity) cube.BBox { return cube.Box(-0.4, 0, -0.4, 0.4, 0.85, 0.4) }
func (wolfType) DecodeNBT(m map[string]any, data *world.EntityData) {
	c := WolfBehaviourConfig{}
	javaState := ReadNativeMobJavaState(m)
	source := m
	if javaState != nil {
		source = javaState
	}
	if _, ok := source["Health"]; ok {
		c.Health, c.healthSet = float64(nbtconv.Float32(source, "Health")), true
	} else if health, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.healthSet = health.Current, true
	}
	if _, ok := source["MaxHealth"]; ok {
		c.MaxHealth = float64(nbtconv.Float32(source, "MaxHealth"))
	} else if health, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.MaxHealth = health.Base
	}
	c.Tamed = nbtconv.Bool(source, "Tame")
	if !c.Tamed {
		c.Tamed = nbtconv.Bool(m, "IsTamed")
	}
	c.Sitting = nbtconv.Bool(source, "Sitting") || nbtconv.Bool(m, "Sitting")
	c.Angry = nbtconv.Bool(source, "Angry") || NativeMobAngry(m)
	c.AgeTicks = int(nbtconv.Int32(source, "Age"))
	if owner := javaUUIDString(source["Owner"]); owner != "" {
		c.OwnerUUID, c.Tamed = owner, true
	} else if most, ok := source["OwnerMost"].(int64); ok {
		if least, ok := source["OwnerLeast"].(int64); ok {
			c.OwnerUUID, c.Tamed = javaUUIDFromHalves(most, least), true
		}
	}
	if _, ok := source["CollarColor"]; ok {
		c.CollarColor = uint8(nbtconv.Int32(source, "CollarColor"))
		c.collarSet = true
	}
	if variant, ok := source["variant"].(string); ok {
		c.Variant = wolfVariant(variant)
	} else if variant, ok := NativeMobVariant(m); ok && variant >= 0 && variant < 8 {
		c.Variant = int(variant)
	}
	if variant, ok := source["sound_variant"].(string); ok {
		c.SoundVariant = strings.TrimPrefix(variant, "minecraft:")
	}
	if armor, ok := source["Armor"].(map[string]any); ok && armor["id"] == "minecraft:wolf_armor" {
		if tag, ok := armor["tag"].(map[string]any); ok {
			c.BodyArmor = item.WolfArmor{}.DecodeNBT(tag).(item.WolfArmor)
		} else {
			c.BodyArmor = item.WolfArmor{}
		}
		if _, ok := armor["Damage"]; ok {
			c.ArmorDamage = int(nbtconv.Int32(armor, "Damage"))
		}
	}
	if equipment, ok := source["equipment"].(map[string]any); ok {
		body, ok := equipment["body"].(map[string]any)
		if !ok {
			body, ok = equipment["minecraft:body"].(map[string]any)
		}
		if ok {
			stack, err := item.StackFromJavaNBT(body)
			if err == nil {
				if armor, ok := stack.Item().(item.WolfArmor); ok {
					c.BodyArmor, c.bodyArmorStack = armor, stack
					c.ArmorDamage = max(0, stack.MaxDurability()-stack.Durability())
				}
			}
		}
	} else if equipment := ReadNativeMobEquipment(m); len(equipment.Armor) > 4 && equipment.Armor[4].Name != "" {
		armor := equipment.Armor[4]
		if stack, ok := nativeWolfArmorStack(armor); ok {
			if value, ok := stack.Item().(item.WolfArmor); ok {
				c.BodyArmor, c.bodyArmorStack = value, stack
				c.ArmorDamage = int(armor.Damage)
			}
		}
	}
	data.Data = c.New()
	data.Data.(*WolfBehaviour).breeding.decodeNBT(source)
}
func (wolfType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*WolfBehaviour)
	maxHealth := b.health.MaxHealth()
	attributes := EncodeNativeMobAttributes(ReadNativeMobAttributes(data.NativeNBT), []NativeMobAttribute{{
		Name: "minecraft:health", Base: maxHealth, Current: b.health.Health(),
		DefaultMin: 0, DefaultMax: maxHealth, Min: 0, Max: maxHealth,
	}})
	native := map[string]any{
		"Attributes": attributes,
		"IsTamed":    boolByte(b.tamed),
		"Sitting":    boolByte(b.sitting),
	}
	variant := int32(8)
	if old, ok := NativeMobVariant(data.NativeNBT); ok {
		variant = old
	}
	native = SetNativeMobVariant(native, variant)
	native = SetNativeMobAngry(native, b.angry)
	ownerID, ok := NativeMobOwnerID(data.NativeNBT)
	if !ok {
		ownerID = -1
	}
	native = SetNativeMobOwnerID(native, ownerID)

	equipment := ReadNativeMobEquipment(data.NativeNBT)
	if equipment.Armor == nil {
		equipment.Armor = make([]NativeMobItem, 5)
	}
	for len(equipment.Armor) < 5 {
		equipment.Armor = append(equipment.Armor, NativeMobItem{})
	}
	equipment.Armor[4] = NativeMobItem{Raw: equipment.Armor[4].Raw}
	if !b.bodyArmor.Empty() {
		raw := item.WriteNBT(b.bodyArmor, true)
		name, _ := raw["Name"].(string)
		count, _ := raw["Count"].(byte)
		damage, _ := nativeNBTInt(raw["Damage"])
		equipment.Armor[4] = NativeMobItem{
			Name: name, Count: count, Damage: int32(damage), Raw: raw,
		}
	}
	if equipment.Mainhand == nil {
		equipment.Mainhand = []NativeMobItem{{}}
	}
	if equipment.Offhand == nil {
		equipment.Offhand = []NativeMobItem{{}}
	}
	native = MergeNativeMobNBT(native, EncodeNativeMobEquipment(equipment))

	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(data.NativeNBT), b.breeding.encodeNBT(map[string]any{
		"Tame": boolByte(b.tamed), "Sitting": boolByte(b.sitting), "Age": int32(b.ageTicks),
		"CollarColor": int32(b.collarColor), "Angry": boolByte(b.angry),
		"variant":       "minecraft:" + []string{"pale", "spotted", "snowy", "black", "ash", "rusty", "woods", "chestnut"}[min(b.variant, 7)],
		"sound_variant": "minecraft:" + b.soundVariant,
	}))
	if b.ownerUUID != "" {
		if owner := javaUUIDIntArray(b.ownerUUID); owner != nil {
			javaState["Owner"] = owner
		}
	} else {
		delete(javaState, "Owner")
	}
	if !b.bodyArmor.Empty() {
		if armor, err := item.StackToJavaNBT(b.bodyArmor); err == nil {
			javaState["equipment"] = map[string]any{"body": armor}
		}
	} else {
		delete(javaState, "equipment")
	}
	return MergeNativeMobJavaState(native, javaState)
}

func nativeWolfArmorStack(armor NativeMobItem) (item.Stack, bool) {
	if armor.Name != "minecraft:wolf_armor" || armor.Count == 0 {
		return item.Stack{}, false
	}
	if armor.Raw != nil {
		stack := item.ReadNBT(armor.Raw, nil)
		if !stack.Empty() {
			return stack, true
		}
	}
	itemType, ok := world.ItemByName(armor.Name, 0)
	if !ok {
		return item.Stack{}, false
	}
	stack := item.NewStack(itemType, int(armor.Count))
	if maximum := stack.MaxDurability(); maximum > 0 {
		stack = stack.WithDurability(maximum - int(armor.Damage))
	}
	return stack, true
}
func wolfVariant(s string) int {
	switch strings.TrimPrefix(s, "minecraft:") {
	case "spotted":
		return 1
	case "snowy":
		return 2
	case "black":
		return 3
	case "ash":
		return 4
	case "rusty":
		return 5
	case "woods":
		return 6
	case "chestnut":
		return 7
	}
	return 0
}
func javaUUIDString(value any) string {
	var parts []int32
	switch uuid := value.(type) {
	case []int32:
		parts = uuid
	case [4]int32:
		parts = uuid[:]
	case []any:
		if len(uuid) != 4 {
			return ""
		}
		parts = make([]int32, 4)
		for i, part := range uuid {
			switch n := part.(type) {
			case int32:
				parts[i] = n
			case int:
				parts[i] = int32(n)
			case int64:
				parts[i] = int32(n)
			default:
				return ""
			}
		}
	default:
		return ""
	}
	if len(parts) != 4 {
		return ""
	}
	raw := make([]byte, 16)
	for i, part := range parts {
		binary.BigEndian.PutUint32(raw[i*4:], uint32(part))
	}
	encoded := hex.EncodeToString(raw)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

func javaUUIDFromHalves(most, least int64) string {
	return javaUUIDString([]int32{int32(uint64(most) >> 32), int32(most), int32(uint64(least) >> 32), int32(least)})
}

func javaUUIDIntArray(uuid string) []int32 {
	raw, err := hex.DecodeString(strings.ReplaceAll(uuid, "-", ""))
	if err != nil || len(raw) != 16 {
		return nil
	}
	parts := make([]int32, 4)
	for i := range parts {
		parts[i] = int32(binary.BigEndian.Uint32(raw[i*4:]))
	}
	return parts
}

type WolfBehaviourConfig struct {
	Health, MaxHealth              float64
	Tamed, Angry, Sitting          bool
	OwnerUUID, SoundVariant        string
	CollarColor                    uint8
	Variant, AgeTicks, ArmorDamage int
	BodyArmor                      item.WolfArmor
	bodyArmorStack                 item.Stack
	healthSet, collarSet           bool
}

func (c WolfBehaviourConfig) Apply(data *world.EntityData) { data.Data = c.New() }
func (c WolfBehaviourConfig) New() *WolfBehaviour {
	maxHealth := c.MaxHealth
	if maxHealth <= 0 {
		if c.Tamed {
			maxHealth = 40
		} else {
			maxHealth = 8
		}
	}
	health := c.Health
	if !c.healthSet && health <= 0 {
		health = maxHealth
	}
	collar := c.CollarColor
	if !c.collarSet && collar == 0 {
		collar = 14
	}
	armor := c.bodyArmorStack
	if armor.Empty() && c.BodyArmor != (item.WolfArmor{}) {
		armor = item.NewStack(c.BodyArmor, 1).WithDurability(c.BodyArmor.DurabilityInfo().MaxDurability - c.ArmorDamage)
	}
	if !armor.Empty() && armor.MaxDurability() > 0 {
		armor = armor.WithDurability(armor.MaxDurability() - c.ArmorDamage)
	}
	soundVariant := strings.TrimPrefix(c.SoundVariant, "minecraft:")
	switch soundVariant {
	case "puglin", "sad", "angry", "grumpy", "big", "cute":
	default:
		soundVariant = "classic"
	}
	return &WolfBehaviour{
		health: NewHealthManager(health, maxHealth), effects: NewEffectManager(),
		mc:    &MovementComputer{Gravity: 0.08, Drag: 0.02, DragBeforeGravity: true},
		speed: 0.3, tamed: c.Tamed, angry: c.Angry, sitting: c.Sitting, ownerUUID: c.OwnerUUID,
		collarColor: collar, variant: uint8(max(0, min(c.Variant, 7))), ageTicks: c.AgeTicks, nextWander: 1,
		soundVariant: soundVariant, bodyArmor: armor,
		// Wolves only panic from fire, lava, freezing, cactus and lightning.
		panic: panicState{environmental: true},
	}
}

type WolfBehaviour struct {
	health                               *HealthManager
	effects                              *EffectManager
	mc                                   *MovementComputer
	speed                                float64
	tamed, angry, sitting, dead          bool
	owner                                *world.EntityHandle
	ownerUUID                            string
	collarColor, variant                 uint8
	soundVariant                         string
	ageTicks, nextWander, followCooldown int
	navigator                            Navigator
	bodyArmor                            item.Stack
	breeding                             breeding
	panic                                panicState
}

// Wolf is a tameable passive wolf with owner and body armour state.
type Wolf struct{ *Ent }

func (w *Wolf) behaviour() *WolfBehaviour { return w.Behaviour().(*WolfBehaviour) }
func (w *Wolf) Tick(tx *world.Tx, current int64) {
	w.behaviour().effects.Tick(w, tx)
	w.Ent.Tick(tx, current)
}
func (b *WolfBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.tamed && b.owner == nil && b.ownerUUID != "" {
		for candidate := range tx.Entities() {
			if candidate.H().UUID().String() == b.ownerUUID {
				b.owner = candidate.H()
				break
			}
		}
	}
	panicking := b.panic.run(e, tx, &b.navigator)
	partner, inLove := tickBreeding(&Wolf{Ent: e}, tx)
	if b.sitting {
		b.navigator.Path, b.navigator.Index = nil, 0
		e.data.Vel = mgl64.Vec3{}
		return nil
	}
	if b.followCooldown > 0 {
		b.followCooldown--
	}
	if b.tamed && b.owner != nil && b.followCooldown == 0 && !panicking {
		if owner, ok := b.owner.Entity(tx); ok && e.Position().Sub(owner.Position()).LenSqr() > 100 {
			start, goal := cube.PosFromVec3(e.Position()), cube.PosFromVec3(owner.Position())
			b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 256}, navigatorTick(e))
			b.followCooldown = 20
		}
	}
	if b.nextWander > 0 {
		b.nextWander--
	}
	speed := b.speed
	if panicking {
		// TamableAnimalPanicGoal at 1.5 times the movement speed.
		speed = groundSpeed(.3, 1.5)
	} else if inLove {
		navigateToPartner(&b.navigator, e, tx, partner)
	} else if b.navigator.Index >= len(b.navigator.Path) && b.nextWander == 0 && !(b.tamed && b.owner != nil) {
		b.chooseWanderPath(e, tx)
	}
	velocity, rot := e.data.Vel, e.data.Rot
	if target, ok := b.navigator.Tick(e.data.Pos, navigatorTick(e), .9); ok {
		delta := target.Sub(e.data.Pos)
		delta[1] = 0
		if delta.Len() < .3 {
			b.navigator.Index++
		} else {
			dir := delta.Normalize()
			velocity[0], velocity[2] = dir[0]*speed, dir[2]*speed
			rot[0] = math.Atan2(-dir[0], dir[2]) * 180 / math.Pi
		}
	} else {
		velocity[0], velocity[2] = 0, 0
	}
	m := b.mc.TickMovement(e, e.data.Pos, velocity, e.data.Rot, rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (b *WolfBehaviour) chooseWanderPath(e *Ent, tx *world.Tx) {
	b.nextWander = 40 + rand.IntN(81)
	start := cube.PosFromVec3(e.Position())
	for range 4 {
		goal := start.Add(cube.Pos{rand.IntN(13) - 6, 0, rand.IntN(13) - 6})
		if b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e)) {
			return
		}
	}
	b.navigator.Path, b.navigator.Index = nil, 0
}
func (w *Wolf) Health() float64        { return w.behaviour().health.Health() }
func (w *Wolf) MaxHealth() float64     { return w.behaviour().health.MaxHealth() }
func (w *Wolf) SetMaxHealth(v float64) { w.behaviour().health.SetMaxHealth(v) }
func (w *Wolf) Dead() bool             { return w.behaviour().dead || w.Health() <= 0 }

// Panicking reports whether the wolf runs from fire or other hazards.
func (w *Wolf) Panicking() bool { return w.behaviour().panic.active }
func (w *Wolf) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	b := w.behaviour()
	if w.Dead() || damage <= 0 || (src.Fire() && w.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if res, ok := b.effects.Effect(effect.Resistance); ok {
		damage *= effect.Resistance.Multiplier(src, res.Level())
	}
	if !b.bodyArmor.Empty() {
		b.bodyArmor = b.bodyArmor.Damage(max(1, int(math.Ceil(damage))))
		damage = wolfArmourDamage(damage)
	}
	b.health.AddHealth(-damage)
	b.panic.hurt(src, w.tx.CurrentTick())
	resetLove(w)
	w.viewAction(HurtAction{})
	onMobHurt(w.tx, w)
	if w.Dead() {
		w.die(src)
	}
	return damage, true
}
func wolfArmourDamage(d float64) float64 {
	reduction := min(20.0, max(11.0/5, 11-d/2))
	return d * (1 - reduction/25)
}
func (w *Wolf) Heal(h float64, _ world.HealingSource) float64 {
	if w.Dead() || h <= 0 {
		return 0
	}
	old := w.Health()
	w.behaviour().health.AddHealth(h)
	return w.Health() - old
}
func (w *Wolf) KnockBack(src mgl64.Vec3, force, height float64) {
	d := w.Position().Sub(src)
	d[1] = 0
	if d.Len() != 0 {
		d = d.Normalize().Mul(force)
	}
	d[1] = height
	w.SetVelocity(d)
}
func (w *Wolf) AddEffect(e effect.Effect)    { w.behaviour().effects.Add(e, w) }
func (w *Wolf) RemoveEffect(t effect.Type)   { w.behaviour().effects.Remove(t, w) }
func (w *Wolf) Effects() []effect.Effect     { return w.behaviour().effects.Effects() }
func (w *Wolf) Speed() float64               { return w.behaviour().speed }
func (w *Wolf) SetSpeed(v float64)           { w.behaviour().speed = v }
func (w *Wolf) hasEffect(t effect.Type) bool { _, ok := w.behaviour().effects.Effect(t); return ok }
func (w *Wolf) Tamed() bool                  { return w.behaviour().tamed }
func (w *Wolf) Owner() *world.EntityHandle   { return w.behaviour().owner }
func (w *Wolf) Angry() bool                  { return w.behaviour().angry }
func (w *Wolf) SetSitting(sit bool)          { w.behaviour().sitting = sit }
func (w *Wolf) Variant() uint8               { return w.behaviour().variant }
func (w *Wolf) CollarColor() uint8           { return w.behaviour().collarColor }
func (w *Wolf) BodyArmor() item.Stack        { return w.behaviour().bodyArmor }
func (w *Wolf) CanShearBodyArmor(user world.Entity) bool {
	b := w.behaviour()
	return user != nil && user.H() != nil && b.tamed && b.ownerUUID != "" &&
		b.ownerUUID == user.H().UUID().String() && !b.bodyArmor.Empty()
}

func (w *Wolf) RemoveBodyArmor(_ *world.Tx) item.Stack {
	b := w.behaviour()
	armor := b.bodyArmor
	b.bodyArmor = item.Stack{}
	return armor
}

func (w *Wolf) ArmorShearingSound() world.Sound {
	return sound.Custom{Name: "item.armor.unequip_wolf", Volume: 1, Pitch: 1}
}

// Interact ports Wolf.mobInteract: a hurt tamed wolf heals twice the
// nutrition of food it is fed, food otherwise breeds it or lets a pup grow
// up faster, and its owner makes it sit or stand.
func (w *Wolf) Interact(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if user == nil || user.H() == nil {
		return false
	}
	b := w.behaviour()
	isOwner := b.tamed && b.ownerUUID != "" && b.ownerUUID == user.H().UUID().String()
	main, _ := user.HeldItems()
	if nutrition, food := wolfFoodNutrition(main.Item()); b.tamed && food && ctx != nil && w.Health() < w.MaxHealth() {
		w.Heal(2*float64(nutrition), nil)
		ctx.SubtractFromCount(1)
		return true
	}
	if feedBreeder(w, user, ctx) {
		return true
	}
	if !isOwner {
		return false
	}
	b.sitting = !b.sitting
	b.navigator.Path, b.navigator.Index = nil, 0
	if b.sitting {
		w.SetVelocity(mgl64.Vec3{})
	}
	return true
}

// Sitting reports whether the wolf sits as its owner ordered.
func (w *Wolf) Sitting() bool { return w.behaviour().sitting }

func (w *Wolf) moveFlagHeld() bool { return w.behaviour().sitting }

// InLove reports whether the wolf is in love.
func (w *Wolf) InLove() bool { return w.behaviour().breeding.InLove() }

func (w *Wolf) breedingState() *breeding { return &w.behaviour().breeding }
func (w *Wolf) ageTicks() *int           { return &w.behaviour().ageTicks }

// breedingFood reports whether the stack is in #wolf_food.
func (w *Wolf) breedingFood(stack item.Stack) bool {
	_, food := wolfFoodNutrition(stack.Item())
	return food
}

// canMateWith ports Wolf.canMate: only tamed wolves breed, and not with a
// sitting partner.
func (w *Wolf) canMateWith(partner breeder) bool {
	p := partner.(*Wolf).behaviour()
	return w.behaviour().tamed && p.tamed && !p.sitting
}

// breedOffspring ports Wolf.getBreedOffspring: the pup takes the variant of
// either parent and a random sound variant, and the pup of a tamed wolf
// belongs to its owner with the collar colours of the parents mixed.
func (w *Wolf) breedOffspring(_ *world.Tx, partner breeder) *world.EntityHandle {
	b, p := w.behaviour(), partner.(*Wolf).behaviour()
	sounds := [...]string{"classic", "puglin", "sad", "angry", "grumpy", "big", "cute"}
	conf := WolfBehaviourConfig{AgeTicks: babyStartAge, Variant: int(randomParent(b.variant, p.variant)), SoundVariant: sounds[rand.IntN(len(sounds))]}
	if b.tamed {
		collar := mixedDyeColour(item.Colours()[b.collarColor%16], item.Colours()[p.collarColor%16])
		conf.Tamed, conf.OwnerUUID, conf.CollarColor, conf.collarSet = true, b.ownerUUID, collar.Uint8(), true
	}
	return babyAt(w).New(WolfType, conf)
}

// wolfFoodNutrition returns the nutrition of items in #wolf_food: meat, fish
// and rabbit stew.
func wolfFoodNutrition(food any) (int, bool) {
	switch foodItem := food.(type) {
	case item.Beef:
		if foodItem.Cooked {
			return 8, true
		}
		return 3, true
	case item.Porkchop:
		if foodItem.Cooked {
			return 8, true
		}
		return 3, true
	case item.Mutton:
		if foodItem.Cooked {
			return 6, true
		}
		return 2, true
	case item.Chicken:
		if foodItem.Cooked {
			return 6, true
		}
		return 2, true
	case item.Rabbit:
		if foodItem.Cooked {
			return 5, true
		}
		return 3, true
	case item.RottenFlesh:
		return 4, true
	case item.Cod:
		if foodItem.Cooked {
			return 5, true
		}
		return 2, true
	case item.Salmon:
		if foodItem.Cooked {
			return 6, true
		}
		return 2, true
	case item.TropicalFish, item.Pufferfish:
		return 1, true
	case item.RabbitStew:
		return 10, true
	}
	return 0, false
}
func (w *Wolf) TameWithBone(tx *world.Tx, user world.Entity) bool {
	b := w.behaviour()
	if b.tamed {
		return false
	}
	if rand.IntN(3) == 0 {
		b.tamed = true
		b.owner = user.H()
		b.ownerUUID = user.H().UUID().String()
		b.health.SetMaxHealth(40)
		b.health.AddHealth(40)
		b.angry = false
		w.viewAction(TameAction{Succeeded: true})
	} else {
		w.viewAction(TameAction{})
	}
	return true
}
func (w *Wolf) CanEquipBodyArmor(user world.Entity) bool {
	b := w.behaviour()
	return b.tamed && user != nil && user.H() != nil && b.ownerUUID != "" && b.ownerUUID == user.H().UUID().String() && b.ageTicks >= 0 && b.bodyArmor.Empty()
}
func (w *Wolf) EquipBodyArmor(tx *world.Tx, user world.Entity, armor world.Item) bool {
	if !w.CanEquipBodyArmor(user) {
		return false
	}
	if _, ok := armor.(item.WolfArmor); !ok {
		return false
	}
	w.behaviour().bodyArmor = item.NewStack(armor, 1)
	tx.PlaySound(w.Position(), sound.Custom{Name: "item.armor.equip_wolf", Volume: 1, Pitch: 1})
	return true
}
func (w *Wolf) viewAction(a world.EntityAction) {
	vs := w.tx.Viewers(w.Position())
	for _, v := range vs {
		v.ViewEntityAction(w, a)
	}
	w.tx.ReleaseViewers(vs)
}
func (w *Wolf) die(src world.DamageSource) {
	b := w.behaviour()
	if b.dead {
		return
	}
	b.dead = true
	w.viewAction(DeathAction{})
	if !b.bodyArmor.Empty() {
		w.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: w.Position()}, b.bodyArmor))
	}
	dropAllDeathLoot(w.tx, w, src)
	_ = w.CloseIn(w.tx)
}

// FinalizeSpawn selects a shared sound variant for members of the same pack.
func (w *Wolf) FinalizeSpawn(_ string, random *java26.Random) {
	if random == nil {
		return
	}
	group := w.H().SpawnGroup()
	variants := [...]string{"classic", "puglin", "sad", "angry", "grumpy", "big", "cute"}
	if group != nil {
		if shared, ok := group.Data.(wolfPackSpawnGroup); ok {
			w.behaviour().soundVariant = shared.soundVariant
			return
		}
	}
	variant := variants[random.NextInt(int32(len(variants)))]
	w.behaviour().soundVariant = variant
	if group != nil {
		group.Data = wolfPackSpawnGroup{soundVariant: variant}
	}
}

type wolfPackSpawnGroup struct{ soundVariant string }
