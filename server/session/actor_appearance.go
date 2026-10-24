package session

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
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
func writeActorShape(e world.Entity, id string, m protocol.EntityMetadata) {
	if flags, ok := bedrockActorFlags[id]; ok {
		m[protocol.EntityDataKeyFlags], m[protocol.EntityDataKeyFlagsTwo] = int64(0), int64(0)
		for _, f := range flags {
			setActorFlag(m, f)
		}
	}
	if shape, ok := actorShape(e, id); ok {
		m[protocol.EntityDataKeyWidth], m[protocol.EntityDataKeyHeight] = shape.width, shape.height
		m[protocol.EntityDataKeyScale] = shape.scale
	}
}

// setActorFlag sets a flag in whichever of the two flag fields holds it.
func setActorFlag(m protocol.EntityMetadata, flag uint8) {
	if flag < 64 {
		m.SetFlag(protocol.EntityDataKeyFlags, flag)
		return
	}
	m.SetFlag(protocol.EntityDataKeyFlagsTwo, flag-64)
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
func (s *Session) writeActorAppearance(e world.Entity, m protocol.EntityMetadata) {
	switch e := e.(type) {
	case *entity.Pig, *entity.Cow, *entity.Chicken:
		// The climate variant property colours these.
		m[protocol.EntityDataKeyVariant] = int32(0)
	case *entity.Cat:
		m[protocol.EntityDataKeyVariant] = e.BedrockVariant()
		writeTameMetadata(m, e.Tamed(), e.Sitting(), e.CollarColor())
		if e.Lying() {
			setActorFlag(m, protocol.EntityDataFlagLayingDown)
		}
		if e.Crouching() {
			setActorFlag(m, protocol.EntityDataFlagSneaking)
		}
	case *entity.PolarBear:
		if e.Standing() {
			setActorFlag(m, protocol.EntityDataFlagStanding)
		}
	case *entity.Turtle:
		if e.HasEgg() {
			setActorFlag(m, protocol.EntityDataFlagPregnant)
		}
		if e.LayingEgg() {
			setActorFlag(m, protocol.EntityDataFlagLayingEgg)
		}
	case *entity.Ocelot:
		if e.Trusting() {
			setActorFlag(m, protocol.EntityDataFlagTrusting)
		}
		if e.Crouching() {
			setActorFlag(m, protocol.EntityDataFlagSneaking)
		}
	case *entity.Wolf:
		m[protocol.EntityDataKeyVariant] = e.BedrockVariant()
		writeTameMetadata(m, e.Tamed(), e.Sitting(), e.CollarColor())
		if e.Angry() {
			setActorFlag(m, protocol.EntityDataFlagAngry)
		}
		if e.Interested() {
			setActorFlag(m, protocol.EntityDataFlagInterested)
		}
	case *entity.Rabbit:
		// Bedrock has no killer bunny.
		if v := e.Variant(); v <= 5 {
			m[protocol.EntityDataKeyVariant] = int32(v)
		} else {
			m[protocol.EntityDataKeyVariant] = int32(0)
		}
	case *entity.Fox:
		m[protocol.EntityDataKeyVariant] = int32(e.Variant())
		if e.Sitting() {
			setActorFlag(m, protocol.EntityDataFlagSitting)
		}
		if e.Sleeping() {
			setActorFlag(m, protocol.EntityDataFlagSleeping)
		}
		if e.Crouching() {
			setActorFlag(m, protocol.EntityDataFlagStalking)
		}
		if e.Interested() {
			setActorFlag(m, protocol.EntityDataFlagInterested)
		}
	case *entity.Mooshroom:
		m[protocol.EntityDataKeyVariant] = int32(e.Variant())
		m[protocol.EntityDataKeyMarkVariant] = int32(-1)
	case *entity.Panda:
		m[protocol.EntityDataKeyVariant] = int32(e.Variant())
		for flag, set := range map[uint8]bool{
			protocol.EntityDataFlagSitting: e.Sitting(), protocol.EntityDataFlagRolling: e.Rolling(),
			protocol.EntityDataFlagLayingDown: e.LyingOnBack(), protocol.EntityDataFlagSneezing: e.Sneezing(),
			protocol.EntityDataFlagEating: e.Eating(), protocol.EntityDataFlagScared: e.Scared(),
		} {
			if set {
				setActorFlag(m, flag)
			}
		}
	case *entity.Axolotl:
		m[protocol.EntityDataKeyVariant], _, _, _ = e.BedrockVariant()
	case *entity.TropicalFish:
		variant, mark, color, color2 := e.BedrockVariant()
		m[protocol.EntityDataKeyVariant], m[protocol.EntityDataKeyMarkVariant] = variant, mark
		m[protocol.EntityDataKeyColorIndex], m[protocol.EntityDataKeyColorTwoIndex] = color, color2
	case *entity.Salmon:
		// The scale sets the size of a salmon.
		m[protocol.EntityDataKeyVariant] = int32(0)
	case *entity.Slime:
		m[protocol.EntityDataKeyVariant] = int32(e.Size())
	case *entity.MagmaCube:
		m[protocol.EntityDataKeyVariant] = int32(e.Size())
	case *entity.SulfurCube:
		m[protocol.EntityDataKeyVariant] = int32(e.Size())
	case *entity.Goat:
		left, right := e.Horns()
		m[protocol.EntityDataKeyGoatHornCount] = int32(boolByte(left) + boolByte(right))
		if e.Screaming() {
			m[protocol.EntityDataKeyVariant] = int32(1)
		}
	case *entity.TraderLlama:
		m[protocol.EntityDataKeyMarkVariant] = int32(1)
	case *entity.Piglin:
		// Piglins with a crossbow are ranged units, the others melee units.
		variant := int32(1)
		if main, _ := e.HeldItems(); !main.Empty() {
			if _, ok := main.Item().(item.Crossbow); ok {
				variant = 0
			}
		}
		m[protocol.EntityDataKeyVariant] = variant
	case *entity.PiglinBrute:
		m[protocol.EntityDataKeyVariant] = int32(1)
	case *entity.WitherSkeleton:
		m[protocol.EntityDataKeyVariant] = int32(1)
	case *entity.Stray, *entity.Husk:
		m[protocol.EntityDataKeyVariant] = int32(2)
	case *entity.Bogged:
		m[protocol.EntityDataKeyVariant] = int32(3)
	case *entity.Parched:
		m[protocol.EntityDataKeyVariant] = int32(4)
	case *entity.Pillager:
		m[protocol.EntityDataKeyVariant] = int32(boolByte(e.IsCaptain()))
	case *entity.Vindicator:
		m[protocol.EntityDataKeyVariant] = int32(boolByte(e.IsCaptain()))
	case *entity.Villager:
		writeVillagerMetadata(m, e.Profession(), e.Variant())
		m[protocol.EntityDataKeyTradeTier] = int32(max(e.MerchantLevel()-1, 0))
		m[protocol.EntityDataKeySkinID] = int32(e.H().UUID()[15] % 6)
	case *entity.ZombieVillager:
		profession, villagerType := e.VillagerData()
		writeVillagerMetadata(m, profession, villagerType)
	case *entity.ZombifiedPiglin:
		if e.Angry() {
			setActorFlag(m, protocol.EntityDataFlagAngry)
		}
	case *entity.Bee:
		if e.Angry() {
			setActorFlag(m, protocol.EntityDataFlagAngry)
		}
	case *entity.Enderman:
		carried, ok := e.CarriedBlock()
		if !ok {
			carried = block.Air{}
		}
		m[protocol.EntityDataKeyCarryBlockRuntimeID] = int32(networkBlockID(s.br, carried))
		if e.Angry() {
			setActorFlag(m, protocol.EntityDataFlagAngry)
		}
	}
}

// writeTameMetadata writes the state of a cat or wolf that may be tamed. Only
// a tamed one shows its collar.
func writeTameMetadata(m protocol.EntityMetadata, tamed, sitting bool, collar uint8) {
	if tamed {
		setActorFlag(m, protocol.EntityDataFlagTamed)
		m[protocol.EntityDataKeyColorIndex] = collar
	}
	if sitting {
		setActorFlag(m, protocol.EntityDataFlagSitting)
	}
}

// writeVillagerMetadata writes the profession and biome type of a villager or
// zombie villager.
func writeVillagerMetadata(m protocol.EntityMetadata, profession, villagerType string) {
	m[protocol.EntityDataKeyVariant] = bedrockProfessions[strings.TrimPrefix(profession, "minecraft:")]
	m[protocol.EntityDataKeyMarkVariant] = bedrockVillagerTypes[strings.TrimPrefix(villagerType, "minecraft:")]
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
	"minecraft:allay":              {protocol.EntityDataFlagFly, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:armadillo":          {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:axolotl":            {protocol.EntityDataFlagSwim, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:bat":                {protocol.EntityDataFlagFly, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:bee":                {protocol.EntityDataFlagFly, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:blaze":              {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagFireImmune, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:boat":               {protocol.EntityDataFlagStackable, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagCollidable},
	"minecraft:bogged":             {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:breeze":             {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:camel":              {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:camel_husk":         {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:cat":                {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:cave_spider":        {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagRenderWhenInvisible},
	"minecraft:chest_boat":         {protocol.EntityDataFlagStackable, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagCollidable},
	"minecraft:chest_minecart":     {protocol.EntityDataFlagStackable, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity},
	"minecraft:chicken":            {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:cod":                {protocol.EntityDataFlagSwim, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:cow":                {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:creaking":           {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagRenderWhenInvisible},
	"minecraft:creeper":            {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:dolphin":            {protocol.EntityDataFlagClimb, protocol.EntityDataFlagSwim, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:donkey":             {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:drowned":            {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagRotationLockedToVehicle},
	"minecraft:elder_guardian":     {protocol.EntityDataFlagSwim, protocol.EntityDataFlagElder, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:enderman":           {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagRenderWhenInvisible},
	"minecraft:endermite":          {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:evocation_illager":  {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:fox":                {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:frog":               {protocol.EntityDataFlagSwim, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:ghast":              {protocol.EntityDataFlagFly, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagFireImmune, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:glow_squid":         {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:goat":               {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:guardian":           {protocol.EntityDataFlagSwim, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:happy_ghast":        {protocol.EntityDataFlagFly, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagRenderWhenInvisible, protocol.EntityDataFlagBodyRotationAlwaysFollowsHead},
	"minecraft:hoglin":             {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:hopper_minecart":    {protocol.EntityDataFlagStackable, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity},
	"minecraft:horse":              {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:husk":               {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagRotationLockedToVehicle},
	"minecraft:iron_golem":         {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:llama":              {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:magma_cube":         {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagFireImmune, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:minecart":           {protocol.EntityDataFlagStackable, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity},
	"minecraft:mooshroom":          {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:mule":               {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:nautilus":           {protocol.EntityDataFlagSwim, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:ocelot":             {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:panda":              {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:parched":            {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:parrot":             {protocol.EntityDataFlagFly, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:phantom":            {protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagRenderWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:pig":                {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:piglin":             {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagDoorOpener, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:piglin_brute":       {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagDoorOpener, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:pillager":           {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:polar_bear":         {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:pufferfish":         {protocol.EntityDataFlagSwim, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:rabbit":             {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:ravager":            {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:salmon":             {protocol.EntityDataFlagSwim, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:sheep":              {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:shulker":            {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagFireImmune, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagRenderWhenInvisible, protocol.EntityDataFlagCollidable, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:silverfish":         {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:skeleton":           {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagRotationLockedToVehicle},
	"minecraft:skeleton_horse":     {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:slime":              {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:sniffer":            {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:snow_golem":         {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:spider":             {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagRenderWhenInvisible},
	"minecraft:squid":              {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:stray":              {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:strider":            {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagFireImmune, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:sulfur_cube":        {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagRenderWhenInvisible},
	"minecraft:tadpole":            {protocol.EntityDataFlagBaby, protocol.EntityDataFlagSwim, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:tnt_minecart":       {protocol.EntityDataFlagStackable, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity},
	"minecraft:trader_llama":       {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:tropicalfish":       {protocol.EntityDataFlagSwim, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:turtle":             {protocol.EntityDataFlagSwim, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:vex":                {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagFireImmune, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:villager_v2":        {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagDoorOpener, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:vindicator":         {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:wandering_trader":   {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:warden":             {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagFireImmune, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:witch":              {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:wither_skeleton":    {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagFireImmune, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:wolf":               {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:zoglin":             {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagFireImmune, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:zombie":             {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible, protocol.EntityDataFlagRotationLockedToVehicle},
	"minecraft:zombie_horse":       {protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:zombie_nautilus":    {protocol.EntityDataFlagSwim, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:zombie_pigman":      {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagFireImmune, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:zombie_villager_v2": {protocol.EntityDataFlagClimb, protocol.EntityDataFlagWalk, protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	// Actors other than mobs.
	"minecraft:armor_stand":            {protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagHiddenWhenInvisible},
	"minecraft:arrow":                  {protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity},
	"minecraft:egg":                    {protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity},
	"minecraft:ender_crystal":          {protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagFireImmune, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:evocation_fang":         {},
	"minecraft:falling_block":          {protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity},
	"minecraft:fireworks_rocket":       {protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity},
	"minecraft:lightning_bolt":         {protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:snowball":               {protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity},
	"minecraft:splash_potion":          {protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity},
	"minecraft:tnt":                    {protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagUsesLegacyFriction},
	"minecraft:wind_charge_projectile": {protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity},
	"minecraft:xp_orb":                 {protocol.EntityDataFlagHasCollision, protocol.EntityDataFlagHasGravity, protocol.EntityDataFlagUsesLegacyFriction},
}
