package session

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// bedrockShape is the collision box and scale BDS gives a vanilla actor.
// Clients draw the model at the scale and pick the actor with the crosshair
// in a box of the width and height times the scale.
type bedrockShape struct{ width, height, scale float32 }

// bedrockShapes holds the shape of an adult actor and, for actors that grow
// up, of a baby.
type bedrockShapes [2]bedrockShape

// writeActorShape writes the shape a Bedrock client draws a vanilla actor
// with and the flags BDS sets on every actor of its type, in place of the
// Java collision box and the generic flags.
func writeActorShape(e world.Entity, id string, m mc.Metadata) {
	if flags, ok := bedrockActorFlags[id]; ok {
		m[mc.EntityDataKeyFlags], m[mc.EntityDataKeyFlagsTwo] = int64(0), int64(0)
		for _, f := range flags {
			setActorFlag(m, f)
		}
	}
	if shape, ok := actorShape(e, id); ok {
		m[mc.EntityDataKeyWidth], m[mc.EntityDataKeyHeight] = shape.width, shape.height
		m[mc.EntityDataKeyScale] = shape.scale
	}
}

// setActorFlag sets a flag in whichever of the two flag fields holds it.
func setActorFlag(m mc.Metadata, flag uint8) {
	if flag < 64 {
		m.SetFlag(mc.EntityDataKeyFlags, flag)
		return
	}
	m.SetFlag(mc.EntityDataKeyFlagsTwo, flag-64)
}

func actorShape(e world.Entity, id string) (bedrockShape, bool) {
	switch e := e.(type) {
	case *entity.Slime:
		return cubeShape(e.Size(), 0.52*float32(e.Size())), true
	case *entity.MagmaCube:
		height := 0.52 * float32(e.Size())
		if e.Size() == 2 {
			height = 1.02
		}
		return cubeShape(e.Size(), height), true
	}
	shapes, ok := bedrockActorShapes[id]
	if !ok {
		return bedrockShape{}, false
	}
	shape := shapes[0]
	if actorIsBaby(e) && shapes[1] != (bedrockShape{}) {
		shape = shapes[1]
	}
	switch e := e.(type) {
	case *entity.Salmon:
		shape.scale *= [...]float32{0.5, 1, 1.5}[min(max(e.Variant(), 0), 2)]
	case *entity.Camel:
		if e.Sitting() {
			shape.height = sittingCamelHeight(shape)
		}
	case *entity.CamelHusk:
		if e.Sitting() {
			shape.height = sittingCamelHeight(shape)
		}
	}
	return shape, true
}

func cubeShape(size int, height float32) bedrockShape {
	return bedrockShape{width: 0.52 * float32(size), height: height, scale: 1}
}

func sittingCamelHeight(shape bedrockShape) float32 {
	if shape.scale < 1 {
		return 0.944
	}
	return 0.945
}

func actorIsBaby(e world.Entity) bool {
	if b, ok := e.(baby); ok {
		return b.Baby()
	}
	if beh, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
		if b, ok := beh.Behaviour().(baby); ok {
			return b.Baby()
		}
	}
	return false
}

var bedrockProfessions = map[string]int32{
	"none": 0, "farmer": 1, "fisherman": 2, "shepherd": 3, "fletcher": 4, "librarian": 5, "cartographer": 6, "cleric": 7,
	"armorer": 8, "weaponsmith": 9, "toolsmith": 10, "butcher": 11, "leatherworker": 12, "mason": 13, "nitwit": 14,
}

var bedrockVillagerTypes = map[string]int32{"plains": 0, "desert": 1, "jungle": 2, "savanna": 3, "snow": 4, "swamp": 5, "taiga": 6}

// writeActorAppearance writes the variants, colours and states Bedrock
// clients draw vanilla mobs by, converted from the Java state Adamant keeps.
func (s *Session) writeActorAppearance(e world.Entity, m mc.Metadata) {
	switch e := e.(type) {
	case *entity.Pig, *entity.Cow, *entity.Chicken:
		// The climate variant property colours these.
		m[mc.EntityDataKeyVariant] = int32(0)
	case *entity.Cat:
		m[mc.EntityDataKeyVariant] = e.BedrockVariant()
		writeTameMetadata(m, e.Tamed(), e.Sitting(), e.CollarColor())
		if e.Lying() {
			setActorFlag(m, mc.EntityDataFlagLayingDown)
		}
		if e.Crouching() {
			setActorFlag(m, mc.EntityDataFlagSneaking)
		}
	case *entity.PolarBear:
		if e.Standing() {
			setActorFlag(m, mc.EntityDataFlagStanding)
		}
	case *entity.Turtle:
		if e.HasEgg() {
			setActorFlag(m, mc.EntityDataFlagPregnant)
		}
		if e.LayingEgg() {
			setActorFlag(m, mc.EntityDataFlagLayingEgg)
		}
	case *entity.Ocelot:
		if e.Trusting() {
			setActorFlag(m, mc.EntityDataFlagTrusting)
		}
		if e.Crouching() {
			setActorFlag(m, mc.EntityDataFlagSneaking)
		}
	case *entity.Wolf:
		m[mc.EntityDataKeyVariant] = e.BedrockVariant()
		writeTameMetadata(m, e.Tamed(), e.Sitting(), e.CollarColor())
		if e.Angry() {
			setActorFlag(m, mc.EntityDataFlagAngry)
		}
		if e.Interested() {
			setActorFlag(m, mc.EntityDataFlagInterested)
		}
	case *entity.Rabbit:
		// Bedrock has no killer bunny.
		if v := e.Variant(); v <= 5 {
			m[mc.EntityDataKeyVariant] = int32(v)
		} else {
			m[mc.EntityDataKeyVariant] = int32(0)
		}
	case *entity.Fox:
		m[mc.EntityDataKeyVariant] = int32(e.Variant())
		if e.Sitting() {
			setActorFlag(m, mc.EntityDataFlagSitting)
		}
		if e.Sleeping() {
			setActorFlag(m, mc.EntityDataFlagSleeping)
		}
		if e.Crouching() {
			setActorFlag(m, mc.EntityDataFlagStalking)
		}
		if e.Interested() {
			setActorFlag(m, mc.EntityDataFlagInterested)
		}
	case *entity.Mooshroom:
		m[mc.EntityDataKeyVariant] = int32(e.Variant())
		m[mc.EntityDataKeyMarkVariant] = int32(-1)
	case *entity.Panda:
		m[mc.EntityDataKeyVariant] = int32(e.Variant())
		for flag, set := range map[uint8]bool{
			mc.EntityDataFlagSitting: e.Sitting(), mc.EntityDataFlagRolling: e.Rolling(),
			mc.EntityDataFlagLayingDown: e.LyingOnBack(), mc.EntityDataFlagSneezing: e.Sneezing(),
			mc.EntityDataFlagEating: e.Eating(), mc.EntityDataFlagScared: e.Scared(),
		} {
			if set {
				setActorFlag(m, flag)
			}
		}
	case *entity.Axolotl:
		m[mc.EntityDataKeyVariant], _, _, _ = e.BedrockVariant()
	case *entity.TropicalFish:
		variant, mark, color, color2 := e.BedrockVariant()
		m[mc.EntityDataKeyVariant], m[mc.EntityDataKeyMarkVariant] = variant, mark
		m[mc.EntityDataKeyColorIndex], m[mc.EntityDataKeyColorTwoIndex] = color, color2
	case *entity.Salmon:
		// The scale sets the size of a salmon.
		m[mc.EntityDataKeyVariant] = int32(0)
	case *entity.Slime:
		m[mc.EntityDataKeyVariant] = int32(e.Size())
	case *entity.MagmaCube:
		m[mc.EntityDataKeyVariant] = int32(e.Size())
	case *entity.SulfurCube:
		m[mc.EntityDataKeyVariant] = int32(e.Size())
	case *entity.Goat:
		left, right := e.Horns()
		m[mc.EntityDataKeyGoatHornCount] = int32(boolByte(left) + boolByte(right))
		if e.Screaming() {
			m[mc.EntityDataKeyVariant] = int32(1)
		}
	case *entity.TraderLlama:
		m[mc.EntityDataKeyMarkVariant] = int32(1)
	case *entity.Piglin:
		// Piglins with a crossbow are ranged units, the others melee units.
		variant := int32(1)
		if main, _ := e.HeldItems(); !main.Empty() {
			if _, ok := main.Item().(item.Crossbow); ok {
				variant = 0
			}
		}
		m[mc.EntityDataKeyVariant] = variant
	case *entity.PiglinBrute:
		m[mc.EntityDataKeyVariant] = int32(1)
	case *entity.WitherSkeleton:
		m[mc.EntityDataKeyVariant] = int32(1)
	case *entity.Stray, *entity.Husk:
		m[mc.EntityDataKeyVariant] = int32(2)
	case *entity.Bogged:
		m[mc.EntityDataKeyVariant] = int32(3)
	case *entity.Parched:
		m[mc.EntityDataKeyVariant] = int32(4)
	case *entity.Pillager:
		m[mc.EntityDataKeyVariant] = int32(boolByte(e.IsCaptain()))
	case *entity.Vindicator:
		m[mc.EntityDataKeyVariant] = int32(boolByte(e.IsCaptain()))
		// Bedrock vindicators are angry while they have a target, and only
		// then raise their axe.
		if e.Aggressive() {
			setActorFlag(m, mc.EntityDataFlagAngry)
		}
	case *entity.Villager:
		writeVillagerMetadata(m, e.Profession(), e.Variant())
		m[mc.EntityDataKeyTradeTier] = int32(max(e.MerchantLevel()-1, 0))
		m[mc.EntityDataKeySkinID] = int32(e.H().UUID()[15] % 6)
	case *entity.ZombieVillager:
		profession, villagerType := e.VillagerData()
		writeVillagerMetadata(m, profession, villagerType)
	case *entity.ZombifiedPiglin:
		if e.Angry() {
			setActorFlag(m, mc.EntityDataFlagAngry)
		}
	case *entity.Bee:
		if e.Angry() {
			setActorFlag(m, mc.EntityDataFlagAngry)
		}
	case *entity.Enderman:
		carried, ok := e.CarriedBlock()
		if !ok {
			carried = block.Air{}
		}
		m[mc.EntityDataKeyCarryBlockRuntimeID] = int32(s.blockHash(carried))
		if e.Angry() {
			setActorFlag(m, mc.EntityDataFlagAngry)
		}
	}
}

// writeTameMetadata writes the state of a cat or wolf that may be tamed. Only
// a tamed one shows its collar.
func writeTameMetadata(m mc.Metadata, tamed, sitting bool, collar uint8) {
	if tamed {
		setActorFlag(m, mc.EntityDataFlagTamed)
		m[mc.EntityDataKeyColorIndex] = collar
	}
	if sitting {
		setActorFlag(m, mc.EntityDataFlagSitting)
	}
}

// writeVillagerMetadata writes the profession and biome type of a villager or
// zombie villager.
func writeVillagerMetadata(m mc.Metadata, profession, villagerType string) {
	m[mc.EntityDataKeyVariant] = bedrockProfessions[strings.TrimPrefix(profession, "minecraft:")]
	m[mc.EntityDataKeyMarkVariant] = bedrockVillagerTypes[strings.TrimPrefix(villagerType, "minecraft:")]
}

var bedrockActorShapes = map[string]bedrockShapes{
	"minecraft:allay":              {{0.35, 0.6, 1}},
	"minecraft:armadillo":          {{0.7, 0.65, 1}, {0.7, 0.65, 0.6}},
	"minecraft:axolotl":            {{0.75, 0.42, 1}, {0.75, 0.42, 0.5}},
	"minecraft:bat":                {{0.5, 0.9, 1}},
	"minecraft:bee":                {{0.55, 0.5, 1}, {0.55, 0.5, 0.5}},
	"minecraft:blaze":              {{0.5, 1.8, 1}},
	"minecraft:boat":               {{1.4, 0.455, 1}},
	"minecraft:bogged":             {{0.6, 1.9, 1}},
	"minecraft:breeze":             {{0.6, 1.77, 1}},
	"minecraft:camel":              {{1.7, 2.375, 1}, {2.11, 3.11, 0.45}},
	"minecraft:camel_husk":         {{1.7, 2.375, 1}},
	"minecraft:cat":                {{0.6, 0.7, 0.8}, {0.6, 0.7, 0.4}},
	"minecraft:cave_spider":        {{0.7, 0.5, 1}},
	"minecraft:chest_boat":         {{1.4, 0.455, 1}},
	"minecraft:chest_minecart":     {{0.98, 0.7, 1}},
	"minecraft:chicken":            {{0.6, 0.8, 1}, {0.6, 0.8, 0.5}},
	"minecraft:cod":                {{0.6, 0.3, 1}},
	"minecraft:cow":                {{0.9, 1.3, 1}, {0.9, 1.3, 0.5}},
	"minecraft:creaking":           {{0.9, 2.7, 1}},
	"minecraft:creeper":            {{0.6, 1.8, 1}},
	"minecraft:dolphin":            {{0.9, 0.6, 1}, {0.9, 0.6, 0.65}},
	"minecraft:donkey":             {{1.4, 1.6, 1}, {1.4, 1.6, 0.5}},
	"minecraft:drowned":            {{0.6, 1.9, 1}, {0.98, 1.96, 0.5}},
	"minecraft:elder_guardian":     {{1.99, 1.99, 1}},
	"minecraft:enderman":           {{0.6, 2.9, 1}},
	"minecraft:endermite":          {{0.4, 0.3, 1}},
	"minecraft:evocation_illager":  {{0.6, 1.9, 1}},
	"minecraft:fox":                {{0.6, 0.7, 1}, {0.72, 0.84, 0.5}},
	"minecraft:frog":               {{0.5, 0.55, 1}},
	"minecraft:ghast":              {{4.02, 4, 1}},
	"minecraft:glow_squid":         {{0.8, 0.8, 1}, {1, 1, 0.5}},
	"minecraft:goat":               {{0.9, 1.3, 1}, {0.9, 1.3, 0.5}},
	"minecraft:guardian":           {{0.85, 0.85, 1}},
	"minecraft:happy_ghast":        {{4, 4, 1}, {4, 4, 0.2375}},
	"minecraft:hoglin":             {{1.4, 1.4, 1}, {1.5, 1.7, 0.5}},
	"minecraft:hopper_minecart":    {{0.98, 0.7, 1}},
	"minecraft:horse":              {{1.4, 1.6, 1}, {1.96, 2.24, 0.5}},
	"minecraft:husk":               {{0.6, 1.9, 1}, {0.98, 1.96, 0.5}},
	"minecraft:iron_golem":         {{1.4, 2.9, 1}},
	"minecraft:llama":              {{0.9, 1.87, 1}, {0.9, 1.87, 0.5}},
	"minecraft:minecart":           {{0.98, 0.7, 1}},
	"minecraft:mooshroom":          {{0.9, 1.3, 1}, {0.9, 1.3, 0.5}},
	"minecraft:mule":               {{1.4, 1.6, 1}, {1.4, 1.6, 0.5}},
	"minecraft:nautilus":           {{0.875, 0.95, 1}, {0.44, 0.5, 1}},
	"minecraft:ocelot":             {{0.6, 0.7, 1}, {0.6, 0.7, 0.5}},
	"minecraft:panda":              {{1.3, 1.25, 1}, {1.3, 1.25, 0.4}},
	"minecraft:parched":            {{0.6, 1.9, 1}},
	"minecraft:parrot":             {{0.5, 1, 1}},
	"minecraft:phantom":            {{0.9, 0.5, 1}},
	"minecraft:pig":                {{0.9, 0.9, 1}, {0.9, 0.9, 0.5}},
	"minecraft:piglin":             {{0.6, 1.9, 1}, {0.98, 1.96, 0.5}},
	"minecraft:piglin_brute":       {{0.6, 1.9, 1}},
	"minecraft:pillager":           {{0.6, 1.9, 1}},
	"minecraft:polar_bear":         {{1.4, 1.4, 1}, {1.4, 1.4, 0.5}},
	"minecraft:pufferfish":         {{0.8, 0.8, 1.2}},
	"minecraft:rabbit":             {{0.81666666, 1, 0.6}, {0.6, 1, 0.4}},
	"minecraft:ravager":            {{1.95, 2.2, 1}},
	"minecraft:salmon":             {{0.5, 0.5, 1}},
	"minecraft:sheep":              {{0.9, 1.3, 1}, {0.9, 1.3, 0.5}},
	"minecraft:shulker":            {{0.6, 1.8, 1}},
	"minecraft:silverfish":         {{0.4, 0.3, 1}},
	"minecraft:skeleton":           {{0.6, 1.9, 1}},
	"minecraft:skeleton_horse":     {{1.4, 1.6, 1}, {1.96, 2.24, 0.5}},
	"minecraft:sniffer":            {{1.9, 1.75, 1}, {1.9, 1.75, 0.45}},
	"minecraft:snow_golem":         {{0.4, 1.8, 1}},
	"minecraft:spider":             {{1.4, 0.9, 1}},
	"minecraft:squid":              {{0.8, 0.8, 1}, {1, 1, 0.5}},
	"minecraft:stray":              {{0.6, 1.9, 1}},
	"minecraft:strider":            {{0.9, 1.7, 1}, {0.9, 1.7, 0.5}},
	"minecraft:tadpole":            {{0.8, 0.6, 1}},
	"minecraft:tnt_minecart":       {{0.98, 0.7, 1}},
	"minecraft:trader_llama":       {{0.9, 1.87, 1}, {0.9, 1.87, 0.5}},
	"minecraft:tropicalfish":       {{0.4, 0.4, 1.3}},
	"minecraft:turtle":             {{1.2, 0.4, 1}, {4, 1.333333, 0.16}},
	"minecraft:vex":                {{0.4, 0.8, 1}},
	"minecraft:villager_v2":        {{0.6, 1.9, 1}, {0.98, 1.96, 0.5}},
	"minecraft:vindicator":         {{0.6, 1.9, 1}},
	"minecraft:wandering_trader":   {{0.6, 1.9, 1}},
	"minecraft:warden":             {{0.9, 2.9, 1}},
	"minecraft:witch":              {{0.6, 1.9, 1}},
	"minecraft:wither_skeleton":    {{0.72, 2.01, 1.2}},
	"minecraft:wolf":               {{0.6, 0.8, 1}, {0.6, 0.8, 0.5}},
	"minecraft:zoglin":             {{1.4, 1.4, 1}, {1.5, 1.7, 0.5}},
	"minecraft:zombie":             {{0.6, 1.9, 1}, {0.98, 1.96, 0.5}},
	"minecraft:zombie_horse":       {{1.4, 1.6, 1}, {1.96, 2.24, 0.5}},
	"minecraft:zombie_nautilus":    {{0.875, 0.95, 1}},
	"minecraft:zombie_pigman":      {{0.6, 1.9, 1}, {0.98, 1.96, 0.5}},
	"minecraft:zombie_villager_v2": {{0.6, 1.9, 1}, {0.98, 1.96, 0.5}},
	// Actors other than mobs whose shape differs from their Java box.
	"minecraft:fireworks_rocket":       {{0.25, 0.25, 1}},
	"minecraft:lightning_bolt":         {{0.6, 1.8, 1}},
	"minecraft:wind_charge_projectile": {{0.3125, 0.3125, 1}},
}

var bedrockActorFlags = map[string][]uint8{
	"minecraft:allay":              {mc.EntityDataFlagFly, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:armadillo":          {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:axolotl":            {mc.EntityDataFlagSwim, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:bat":                {mc.EntityDataFlagFly, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:bee":                {mc.EntityDataFlagFly, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:blaze":              {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagFireImmune, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:boat":               {mc.EntityDataFlagStackable, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagCollidable},
	"minecraft:bogged":             {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:breeze":             {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:camel":              {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:camel_husk":         {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:cat":                {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:cave_spider":        {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagRenderWhenInvisible},
	"minecraft:chest_boat":         {mc.EntityDataFlagStackable, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagCollidable},
	"minecraft:chest_minecart":     {mc.EntityDataFlagStackable, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity},
	"minecraft:chicken":            {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:cod":                {mc.EntityDataFlagSwim, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:cow":                {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:creaking":           {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagRenderWhenInvisible},
	"minecraft:creeper":            {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:dolphin":            {mc.EntityDataFlagClimb, mc.EntityDataFlagSwim, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:donkey":             {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:drowned":            {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagRotationLockedToVehicle},
	"minecraft:elder_guardian":     {mc.EntityDataFlagSwim, mc.EntityDataFlagElder, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:enderman":           {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagRenderWhenInvisible},
	"minecraft:endermite":          {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:evocation_illager":  {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:fox":                {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:frog":               {mc.EntityDataFlagSwim, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:ghast":              {mc.EntityDataFlagFly, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagFireImmune, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:glow_squid":         {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:goat":               {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:guardian":           {mc.EntityDataFlagSwim, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:happy_ghast":        {mc.EntityDataFlagFly, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagRenderWhenInvisible, mc.EntityDataFlagBodyRotationAlwaysFollowsHead},
	"minecraft:hoglin":             {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:hopper_minecart":    {mc.EntityDataFlagStackable, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity},
	"minecraft:horse":              {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:husk":               {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagRotationLockedToVehicle},
	"minecraft:iron_golem":         {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:llama":              {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:magma_cube":         {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagFireImmune, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:minecart":           {mc.EntityDataFlagStackable, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity},
	"minecraft:mooshroom":          {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:mule":               {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:nautilus":           {mc.EntityDataFlagSwim, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:ocelot":             {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:panda":              {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:parched":            {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:parrot":             {mc.EntityDataFlagFly, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:phantom":            {mc.EntityDataFlagHasCollision, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagRenderWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:pig":                {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:piglin":             {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagDoorOpener, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:piglin_brute":       {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagDoorOpener, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:pillager":           {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:polar_bear":         {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:pufferfish":         {mc.EntityDataFlagSwim, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:rabbit":             {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:ravager":            {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:salmon":             {mc.EntityDataFlagSwim, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:sheep":              {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:shulker":            {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagFireImmune, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagRenderWhenInvisible, mc.EntityDataFlagCollidable, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:silverfish":         {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:skeleton":           {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagRotationLockedToVehicle},
	"minecraft:skeleton_horse":     {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:slime":              {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:sniffer":            {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:snow_golem":         {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:spider":             {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagRenderWhenInvisible},
	"minecraft:squid":              {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:stray":              {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:strider":            {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagFireImmune, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:sulfur_cube":        {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagRenderWhenInvisible},
	"minecraft:tadpole":            {mc.EntityDataFlagBaby, mc.EntityDataFlagSwim, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:tnt_minecart":       {mc.EntityDataFlagStackable, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity},
	"minecraft:trader_llama":       {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:tropicalfish":       {mc.EntityDataFlagSwim, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:turtle":             {mc.EntityDataFlagSwim, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:vex":                {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagFireImmune, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:villager_v2":        {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagDoorOpener, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:vindicator":         {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:wandering_trader":   {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:warden":             {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagFireImmune, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:witch":              {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:wither_skeleton":    {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagFireImmune, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:wolf":               {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:zoglin":             {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagFireImmune, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:zombie":             {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible, mc.EntityDataFlagRotationLockedToVehicle},
	"minecraft:zombie_horse":       {mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:zombie_nautilus":    {mc.EntityDataFlagSwim, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:zombie_pigman":      {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagFireImmune, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:zombie_villager_v2": {mc.EntityDataFlagClimb, mc.EntityDataFlagWalk, mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	// Actors other than mobs.
	"minecraft:armor_stand":            {mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagHiddenWhenInvisible},
	"minecraft:arrow":                  {mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity},
	"minecraft:egg":                    {mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity},
	"minecraft:ender_crystal":          {mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagFireImmune, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:evocation_fang":         {},
	"minecraft:falling_block":          {mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity},
	"minecraft:fireworks_rocket":       {mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity},
	"minecraft:lightning_bolt":         {mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:snowball":               {mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity},
	"minecraft:splash_potion":          {mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity},
	"minecraft:tnt":                    {mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagUsesLegacyFriction},
	"minecraft:wind_charge_projectile": {mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity},
	"minecraft:xp_orb":                 {mc.EntityDataFlagHasCollision, mc.EntityDataFlagHasGravity, mc.EntityDataFlagUsesLegacyFriction},
}
