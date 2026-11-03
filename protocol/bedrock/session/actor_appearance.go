package session

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
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
func writeActorShape(e world.Entity, id string, m v2193.Metadata) {
	if flags, ok := bedrockActorFlags[id]; ok {
		m[v2193.EntityDataKeyFlags], m[v2193.EntityDataKeyFlagsTwo] = int64(0), int64(0)
		for _, f := range flags {
			setActorFlag(m, f)
		}
	}
	if shape, ok := actorShape(e, id); ok {
		m[v2193.EntityDataKeyWidth], m[v2193.EntityDataKeyHeight] = shape.width, shape.height
		m[v2193.EntityDataKeyScale] = shape.scale
	}
}

// setActorFlag sets a flag in whichever of the two flag fields holds it.
func setActorFlag(m v2193.Metadata, flag uint8) {
	if flag < 64 {
		m.SetFlag(v2193.EntityDataKeyFlags, flag)
		return
	}
	m.SetFlag(v2193.EntityDataKeyFlagsTwo, flag-64)
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
func (s *Session) writeActorAppearance(e world.Entity, m v2193.Metadata) {
	switch e := e.(type) {
	case *entity.Pig, *entity.Cow, *entity.Chicken:
		// The climate variant property colours these.
		m[v2193.EntityDataKeyVariant] = int32(0)
	case *entity.Cat:
		m[v2193.EntityDataKeyVariant] = e.BedrockVariant()
		writeTameMetadata(m, e.Tamed(), e.Sitting(), e.CollarColor())
		if e.Lying() {
			setActorFlag(m, v2193.EntityDataFlagLayingDown)
		}
		if e.Crouching() {
			setActorFlag(m, v2193.EntityDataFlagSneaking)
		}
	case *entity.PolarBear:
		if e.Standing() {
			setActorFlag(m, v2193.EntityDataFlagStanding)
		}
	case *entity.Turtle:
		if e.HasEgg() {
			setActorFlag(m, v2193.EntityDataFlagPregnant)
		}
		if e.LayingEgg() {
			setActorFlag(m, v2193.EntityDataFlagLayingEgg)
		}
	case *entity.Ocelot:
		if e.Trusting() {
			setActorFlag(m, v2193.EntityDataFlagTrusting)
		}
		if e.Crouching() {
			setActorFlag(m, v2193.EntityDataFlagSneaking)
		}
	case *entity.Wolf:
		m[v2193.EntityDataKeyVariant] = e.BedrockVariant()
		writeTameMetadata(m, e.Tamed(), e.Sitting(), e.CollarColor())
		if e.Angry() {
			setActorFlag(m, v2193.EntityDataFlagAngry)
		}
		if e.Interested() {
			setActorFlag(m, v2193.EntityDataFlagInterested)
		}
	case *entity.Rabbit:
		// Bedrock has no killer bunny.
		if v := e.Variant(); v <= 5 {
			m[v2193.EntityDataKeyVariant] = int32(v)
		} else {
			m[v2193.EntityDataKeyVariant] = int32(0)
		}
	case *entity.Fox:
		m[v2193.EntityDataKeyVariant] = int32(e.Variant())
		if e.Sitting() {
			setActorFlag(m, v2193.EntityDataFlagSitting)
		}
		if e.Sleeping() {
			setActorFlag(m, v2193.EntityDataFlagSleeping)
		}
		if e.Crouching() {
			setActorFlag(m, v2193.EntityDataFlagStalking)
		}
		if e.Interested() {
			setActorFlag(m, v2193.EntityDataFlagInterested)
		}
	case *entity.Mooshroom:
		m[v2193.EntityDataKeyVariant] = int32(e.Variant())
		m[v2193.EntityDataKeyMarkVariant] = int32(-1)
	case *entity.Panda:
		m[v2193.EntityDataKeyVariant] = int32(e.Variant())
		for flag, set := range map[uint8]bool{
			v2193.EntityDataFlagSitting: e.Sitting(), v2193.EntityDataFlagRolling: e.Rolling(),
			v2193.EntityDataFlagLayingDown: e.LyingOnBack(), v2193.EntityDataFlagSneezing: e.Sneezing(),
			v2193.EntityDataFlagEating: e.Eating(), v2193.EntityDataFlagScared: e.Scared(),
		} {
			if set {
				setActorFlag(m, flag)
			}
		}
	case *entity.Axolotl:
		m[v2193.EntityDataKeyVariant], _, _, _ = e.BedrockVariant()
	case *entity.TropicalFish:
		variant, mark, color, color2 := e.BedrockVariant()
		m[v2193.EntityDataKeyVariant], m[v2193.EntityDataKeyMarkVariant] = variant, mark
		m[v2193.EntityDataKeyColorIndex], m[v2193.EntityDataKeyColorTwoIndex] = color, color2
	case *entity.Salmon:
		// The scale sets the size of a salmon.
		m[v2193.EntityDataKeyVariant] = int32(0)
	case *entity.Slime:
		m[v2193.EntityDataKeyVariant] = int32(e.Size())
	case *entity.MagmaCube:
		m[v2193.EntityDataKeyVariant] = int32(e.Size())
	case *entity.SulfurCube:
		m[v2193.EntityDataKeyVariant] = int32(e.Size())
	case *entity.Goat:
		left, right := e.Horns()
		m[v2193.EntityDataKeyGoatHornCount] = int32(boolByte(left) + boolByte(right))
		if e.Screaming() {
			m[v2193.EntityDataKeyVariant] = int32(1)
		}
	case *entity.TraderLlama:
		m[v2193.EntityDataKeyMarkVariant] = int32(1)
	case *entity.Piglin:
		// Piglins with a crossbow are ranged units, the others melee units.
		variant := int32(1)
		if main, _ := e.HeldItems(); !main.Empty() {
			if _, ok := main.Item().(item.Crossbow); ok {
				variant = 0
			}
		}
		m[v2193.EntityDataKeyVariant] = variant
	case *entity.PiglinBrute:
		m[v2193.EntityDataKeyVariant] = int32(1)
	case *entity.WitherSkeleton:
		m[v2193.EntityDataKeyVariant] = int32(1)
	case *entity.Stray, *entity.Husk:
		m[v2193.EntityDataKeyVariant] = int32(2)
	case *entity.Bogged:
		m[v2193.EntityDataKeyVariant] = int32(3)
	case *entity.Parched:
		m[v2193.EntityDataKeyVariant] = int32(4)
	case *entity.Pillager:
		m[v2193.EntityDataKeyVariant] = int32(boolByte(e.IsCaptain()))
	case *entity.Vindicator:
		m[v2193.EntityDataKeyVariant] = int32(boolByte(e.IsCaptain()))
		// Bedrock vindicators are angry while they have a target, and only
		// then raise their axe.
		if e.Aggressive() {
			setActorFlag(m, v2193.EntityDataFlagAngry)
		}
	case *entity.Villager:
		writeVillagerMetadata(m, e.Profession(), e.Variant())
		m[v2193.EntityDataKeyTradeTier] = int32(max(e.MerchantLevel()-1, 0))
		m[v2193.EntityDataKeySkinID] = int32(e.H().UUID()[15] % 6)
	case *entity.ZombieVillager:
		profession, villagerType := e.VillagerData()
		writeVillagerMetadata(m, profession, villagerType)
	case *entity.ZombifiedPiglin:
		if e.Angry() {
			setActorFlag(m, v2193.EntityDataFlagAngry)
		}
	case *entity.Bee:
		if e.Angry() {
			setActorFlag(m, v2193.EntityDataFlagAngry)
		}
	case *entity.Enderman:
		carried, ok := e.CarriedBlock()
		if !ok {
			carried = block.Air{}
		}
		m[v2193.EntityDataKeyCarryBlockRuntimeID] = int32(networkBlockID(s.br, carried))
		if e.Angry() {
			setActorFlag(m, v2193.EntityDataFlagAngry)
		}
	}
}

// writeTameMetadata writes the state of a cat or wolf that may be tamed. Only
// a tamed one shows its collar.
func writeTameMetadata(m v2193.Metadata, tamed, sitting bool, collar uint8) {
	if tamed {
		setActorFlag(m, v2193.EntityDataFlagTamed)
		m[v2193.EntityDataKeyColorIndex] = collar
	}
	if sitting {
		setActorFlag(m, v2193.EntityDataFlagSitting)
	}
}

// writeVillagerMetadata writes the profession and biome type of a villager or
// zombie villager.
func writeVillagerMetadata(m v2193.Metadata, profession, villagerType string) {
	m[v2193.EntityDataKeyVariant] = bedrockProfessions[strings.TrimPrefix(profession, "minecraft:")]
	m[v2193.EntityDataKeyMarkVariant] = bedrockVillagerTypes[strings.TrimPrefix(villagerType, "minecraft:")]
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
	"minecraft:allay":              {v2193.EntityDataFlagFly, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:armadillo":          {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:axolotl":            {v2193.EntityDataFlagSwim, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:bat":                {v2193.EntityDataFlagFly, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:bee":                {v2193.EntityDataFlagFly, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:blaze":              {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagFireImmune, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:boat":               {v2193.EntityDataFlagStackable, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagCollidable},
	"minecraft:bogged":             {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:breeze":             {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:camel":              {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:camel_husk":         {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:cat":                {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:cave_spider":        {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagRenderWhenInvisible},
	"minecraft:chest_boat":         {v2193.EntityDataFlagStackable, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagCollidable},
	"minecraft:chest_minecart":     {v2193.EntityDataFlagStackable, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity},
	"minecraft:chicken":            {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:cod":                {v2193.EntityDataFlagSwim, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:cow":                {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:creaking":           {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagRenderWhenInvisible},
	"minecraft:creeper":            {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:dolphin":            {v2193.EntityDataFlagClimb, v2193.EntityDataFlagSwim, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:donkey":             {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:drowned":            {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagRotationLockedToVehicle},
	"minecraft:elder_guardian":     {v2193.EntityDataFlagSwim, v2193.EntityDataFlagElder, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:enderman":           {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagRenderWhenInvisible},
	"minecraft:endermite":          {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:evocation_illager":  {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:fox":                {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:frog":               {v2193.EntityDataFlagSwim, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:ghast":              {v2193.EntityDataFlagFly, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagFireImmune, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:glow_squid":         {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:goat":               {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:guardian":           {v2193.EntityDataFlagSwim, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:happy_ghast":        {v2193.EntityDataFlagFly, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagRenderWhenInvisible, v2193.EntityDataFlagBodyRotationAlwaysFollowsHead},
	"minecraft:hoglin":             {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:hopper_minecart":    {v2193.EntityDataFlagStackable, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity},
	"minecraft:horse":              {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:husk":               {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagRotationLockedToVehicle},
	"minecraft:iron_golem":         {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:llama":              {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:magma_cube":         {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagFireImmune, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:minecart":           {v2193.EntityDataFlagStackable, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity},
	"minecraft:mooshroom":          {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:mule":               {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:nautilus":           {v2193.EntityDataFlagSwim, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:ocelot":             {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:panda":              {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:parched":            {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:parrot":             {v2193.EntityDataFlagFly, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:phantom":            {v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagRenderWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:pig":                {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:piglin":             {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagDoorOpener, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:piglin_brute":       {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagDoorOpener, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:pillager":           {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:polar_bear":         {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:pufferfish":         {v2193.EntityDataFlagSwim, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:rabbit":             {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:ravager":            {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:salmon":             {v2193.EntityDataFlagSwim, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:sheep":              {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:shulker":            {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagFireImmune, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagRenderWhenInvisible, v2193.EntityDataFlagCollidable, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:silverfish":         {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:skeleton":           {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagRotationLockedToVehicle},
	"minecraft:skeleton_horse":     {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:slime":              {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:sniffer":            {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:snow_golem":         {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:spider":             {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagRenderWhenInvisible},
	"minecraft:squid":              {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:stray":              {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:strider":            {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagFireImmune, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:sulfur_cube":        {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagRenderWhenInvisible},
	"minecraft:tadpole":            {v2193.EntityDataFlagBaby, v2193.EntityDataFlagSwim, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:tnt_minecart":       {v2193.EntityDataFlagStackable, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity},
	"minecraft:trader_llama":       {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:tropicalfish":       {v2193.EntityDataFlagSwim, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:turtle":             {v2193.EntityDataFlagSwim, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:vex":                {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagFireImmune, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:villager_v2":        {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagDoorOpener, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:vindicator":         {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:wandering_trader":   {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:warden":             {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagFireImmune, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:witch":              {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:wither_skeleton":    {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagFireImmune, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:wolf":               {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:zoglin":             {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagFireImmune, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:zombie":             {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible, v2193.EntityDataFlagRotationLockedToVehicle},
	"minecraft:zombie_horse":       {v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:zombie_nautilus":    {v2193.EntityDataFlagSwim, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:zombie_pigman":      {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagFireImmune, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:zombie_villager_v2": {v2193.EntityDataFlagClimb, v2193.EntityDataFlagWalk, v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	// Actors other than mobs.
	"minecraft:armor_stand":            {v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagHiddenWhenInvisible},
	"minecraft:arrow":                  {v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity},
	"minecraft:egg":                    {v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity},
	"minecraft:ender_crystal":          {v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagFireImmune, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:evocation_fang":         {},
	"minecraft:falling_block":          {v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity},
	"minecraft:fireworks_rocket":       {v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity},
	"minecraft:lightning_bolt":         {v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:snowball":               {v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity},
	"minecraft:splash_potion":          {v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity},
	"minecraft:tnt":                    {v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagUsesLegacyFriction},
	"minecraft:wind_charge_projectile": {v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity},
	"minecraft:xp_orb":                 {v2193.EntityDataFlagHasCollision, v2193.EntityDataFlagHasGravity, v2193.EntityDataFlagUsesLegacyFriction},
}
