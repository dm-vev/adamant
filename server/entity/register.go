package entity

import (
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/enchantment"
	"github.com/dm-vev/adamant/server/item/potion"
	"github.com/dm-vev/adamant/server/world"
)

// DefaultRegistry is a world.EntityRegistry that registers all default entities
// implemented by Dragonfly.
var DefaultRegistry world.EntityRegistry

func init() {
	DefaultRegistry = conf.New([]world.EntityType{
		entityPaintingType,
		entityChickenType,
		horseType,
		donkeyType,
		muleType,
		skeletonHorseType,
		zombieHorseType,
		llamaType,
		traderLlamaType,
		camelType,
		camelHuskType,
		entitySheepType,
		PigType,
		entityMooshroomType,
		entityRabbitType,
		entityGoatType,
		entityArmadilloType,
		entitySnifferType,
		entityFrogType,
		entityVillagerType,
		entityWanderingTraderType,
		entityWitchType,
		zombieType,
		zombieVillagerType,
		skeletonType,
		witherSkeletonType,
		entitySpiderType,
		entityCaveSpiderType,
		entityCreeperType,
		entityEndermanType,
		entitySilverfishType,
		entityEndermiteType,
		entitySlimeType,
		strayType,
		boggedType,
		huskType,
		parchedType,
		entityPhantomType,
		entityWardenType,
		entityWitherType,
		entityWitherSkullType,
		evokerType,
		entityEvokerFangsType,
		vindicatorType,
		pillagerType,
		vexType,
		entityGhastType,
		entityBlazeType,
		entityMagmaCubeType,
		entityBreezeType,
		entityCreakingType,
		entityBreezeWindChargeType,
		entityOminousItemSpawnerType,
		ravagerType,
		entityDrownedType,
		piglinType{},
		piglinBruteType{},
		zombifiedPiglinType{},
		hoglinType,
		zoglinType,
		entityStriderType,
		entityGuardianType,
		entityElderGuardianType,
		squidType,
		glowSquidType,
		turtleType,
		entityHappyGhastType,
		dolphinType,
		entityNautilusType,
		entityBatType,
		entityCatType,
		entityAllayType,
		entityOcelotType,
		entityFoxType,
		entityPandaType,
		entityPolarBearType,
		entityBeeType,
		entityParrotType,
		entityIronGolemType,
		entitySnowGolemType,
		entityCopperGolemType,
		SulfurCubeType,
		entityZombieNautilusType,
		entityWolfType,
		entityCushionType,
		entityAreaEffectCloudType,
		entityArmourStandType,
		ArrowType,
		entityBottleOfEnchantingType,
		entityEggType,
		axolotlType,
		codType,
		pufferfishType,
		salmonType,
		tadpoleType,
		tropicalFishType,
		entityFishingHookType,
		EnderPearlType,
		ExperienceOrbType,
		FallingBlockType,
		FireworkType,
		ItemType,
		entityBoatType,
		entityChestBoatType,
		entityMinecartType,
		entityCommandBlockMinecartType,
		entityChestMinecartType,
		CowType,
		entityHopperMinecartType,
		entityTNTMinecartType,
		entityLightningType,
		EndCrystalType,
		entityEnderDragonType,
		entityDragonFireballType,
		entityGhastFireballType,
		entityLlamaSpitType,
		LingeringPotionType,
		entitySnowballType,
		entityShulkerType,
		entityShulkerBulletType,
		entitySmallFireballType,
		entityWindChargeType,
		entityTridentType,
		entitySplashPotionType,
		tNTType,
		TextType,
		entityEyeOfEnderType,
		entityLeashKnotType,
	})
}

var conf = world.EntityRegistryConfig{
	TieLeashedEntities: tieLeashedEntities,
	Painting:           createPainting,
	EntityCollisions:   tickEntityCollisions,
	SpawnOriginalMobs:  spawnOriginalMobs,
	EncodeSharedNBT:    encodeSharedNBT,
	DecodeSharedNBT:    decodeSharedNBT,
	NewFromBucket:      newFromBucket,
	EnderDragon:        NewEnderDragon,
	DragonFight:        newDragonFight,
	GolemPattern:       trySpawnConstructedGolem,
	RaidSpawner:        newRaidSpawner,
	StartRiding:        startRiding,
	SpawnMob:           spawnMob,
	SpawnEggOffspring:  spawnEggOffspring,
	NaturalSpawnTick:   naturalSpawnTick,
	SpawnMobFromNBT:    spawnMobFromNBT,
	ExperienceOrbs:     NewExperienceOrbs,
	SmallFireball:      NewSmallFireball,
	OminousItemSpawner: func(opts world.EntitySpawnOpts, stack any, delay int64) *world.EntityHandle {
		return NewOminousItemSpawner(opts, stack.(item.Stack), delay)
	},
	WitherPattern:      trySpawnWitherPattern,
	EquipWithDispenser: equipWithDispenser,
	ShearWithDispenser: shearWithDispenser,
	ShearLeashes:       shearOffLeashes,
	TNT:                NewTNT,
	Egg:                NewEgg,
	EggVariant:         NewEggVariant,
	Snowball:           NewSnowball,
	WindCharge:         NewWindCharge,
	BottleOfEnchanting: NewBottleOfEnchanting,
	EnderPearl:         NewEnderPearl,
	FallingBlock:       NewFallingBlock,
	Lightning:          NewLightning,
	VisualLightning: func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return NewLightningWithDamage(opts, 0, false, 0)
	},
	ArmourStand: NewArmourStand,
	Cushion:     NewCushion,
	Shulker: func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return NewShulker(opts, ShulkerBehaviourConfig{})
	},
	ShulkerBullet: NewShulkerBullet,
	EndCrystal: func(opts world.EntitySpawnOpts, showBase bool) *world.EntityHandle {
		conf := endCrystalConf
		conf.ShowBase = showBase
		return opts.New(EndCrystalType, conf)
	},
	Firework: func(opts world.EntitySpawnOpts, firework world.Item, owner world.Entity, sidewaysVelocityMultiplier, upwardsAcceleration float64, attached bool) *world.EntityHandle {
		return newFirework(opts, firework.(item.Firework), owner, sidewaysVelocityMultiplier, upwardsAcceleration, attached)
	},
	Item: func(opts world.EntitySpawnOpts, it any) *world.EntityHandle {
		return NewItem(opts, it.(item.Stack))
	},
	EyeOfEnder: NewEyeOfEnder,
	Boat: func(opts world.EntitySpawnOpts, variant int) *world.EntityHandle {
		return NewBoat(opts, variant)
	},
	ChestBoat: func(opts world.EntitySpawnOpts, variant int) *world.EntityHandle {
		return NewChestBoat(opts, variant)
	},
	Minecart: func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return opts.New(entityMinecartType, minecartConf)
	},
	MinecartChest: func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return opts.New(entityChestMinecartType, chestMinecartConf)
	},
	MinecartHopper: func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return opts.New(entityHopperMinecartType, hopperMinecartConf)
	},
	MinecartTNT: func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return opts.New(entityTNTMinecartType, tntMinecartConf)
	},
	// Like a new BaseCommandBlock, the minecart tracks its output.
	MinecartCommandBlock: func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return newCommandBlockMinecart(opts, commandBlockMinecartConfig{TrackOutput: true})
	},
	LingeringPotion: func(opts world.EntitySpawnOpts, t any, owner world.Entity) *world.EntityHandle {
		return NewLingeringPotion(opts, t.(potion.Potion), owner)
	},
	SplashPotion: func(opts world.EntitySpawnOpts, t any, owner world.Entity) *world.EntityHandle {
		return NewSplashPotion(opts, t.(potion.Potion), owner)
	},
	Arrow: func(opts world.EntitySpawnOpts, arrow world.ArrowSpawnConfig) *world.EntityHandle {
		tip := arrow.Tip.(potion.Potion)
		conf := arrowConf
		conf.Damage, conf.Potion, conf.Owner = arrow.Damage, tip, ownerHandle(arrow.Owner)
		conf.powerLevel = arrow.PowerLevel
		conf.KnockBackForceAddend = float64(arrow.PunchLevel) * enchantment.Punch.KnockBackMultiplier()
		conf.DisablePickup = arrow.DisablePickup
		if arrow.ObtainArrowOnPickup {
			conf.PickupItem = item.NewStack(item.Arrow{Tip: tip}, 1)
		}
		conf.Critical = arrow.Critical
		conf.PiercingLevel = arrow.PiercingLevel
		return opts.New(ArrowType, conf)
	},
	Trident: func(opts world.EntitySpawnOpts, owner world.Entity, stack any, loyalty, impaling int, channeling bool) *world.EntityHandle {
		s, ok := stack.(item.Stack)
		if !ok {
			panic("world.EntityRegistryConfig.Trident: stack must be item.Stack")
		}
		return NewTrident(opts, owner, s, loyalty, impaling, channeling)
	},
	FishingHook: func(opts world.EntitySpawnOpts, owner world.Entity, rod any) *world.EntityHandle {
		stack, ok := rod.(item.Stack)
		if !ok {
			panic("world.EntityRegistryConfig.FishingHook: rod must be item.Stack")
		}
		return opts.New(entityFishingHookType, fishingHookConfig{Owner: ownerHandle(owner), Rod: stack})
	},
}

// spawnTable holds the constructor of each mob that the SpawnMob of the registry config makes, by Java
// identifier.
var spawnTable = map[string]func(world.EntitySpawnOpts) *world.EntityHandle{
	"minecraft:allay":       NewAllay,
	"minecraft:armadillo":   NewArmadillo,
	"minecraft:axolotl":     NewAxolotl,
	"minecraft:bat":         NewBat,
	"minecraft:blaze":       NewBlaze,
	"minecraft:bogged":      NewBogged,
	"minecraft:breeze":      NewBreeze,
	"minecraft:camel":       NewCamel,
	"minecraft:creaking":    NewCreaking,
	"minecraft:camel_husk":  NewCamelHusk,
	"minecraft:cat":         NewCat,
	"minecraft:bee":         NewBee,
	"minecraft:cave_spider": NewCaveSpider,
	"minecraft:chicken": func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return NewChicken(opts, ChickenBehaviourConfig{})
	},
	"minecraft:cod":            NewCod,
	"minecraft:cow":            NewCow,
	"minecraft:creeper":        NewCreeper,
	"minecraft:dolphin":        NewDolphin,
	"minecraft:donkey":         NewDonkey,
	"minecraft:drowned":        NewDrowned,
	"minecraft:elder_guardian": NewElderGuardian,
	"minecraft:ender_dragon":   NewEnderDragon,
	"minecraft:enderman":       NewEnderman,
	"minecraft:endermite":      NewEndermite,
	"minecraft:evoker":         NewEvoker,
	"minecraft:fox":            NewFox,
	"minecraft:frog":           NewFrog,
	"minecraft:ghast":          NewGhast,
	"minecraft:glow_squid":     NewGlowSquid,
	"minecraft:goat":           NewGoat,
	"minecraft:guardian":       NewGuardian,
	"minecraft:hoglin":         NewHoglin,
	"minecraft:horse":          NewHorse,
	"minecraft:husk":           NewHusk,
	"minecraft:iron_golem":     NewIronGolem,
	"minecraft:llama":          NewLlama,
	"minecraft:magma_cube":     NewMagmaCube,
	"minecraft:mooshroom":      NewMooshroom,
	"minecraft:mule":           NewMule,
	"minecraft:nautilus":       NewNautilus,
	"minecraft:ocelot":         NewOcelot,
	"minecraft:panda":          NewPanda,
	"minecraft:parched":        NewParched,
	"minecraft:parrot":         NewParrot,
	"minecraft:pig":            NewPig,
	"minecraft:piglin":         NewPiglin,
	"minecraft:piglin_brute":   NewPiglinBrute,
	"minecraft:pillager":       NewPillager,
	"minecraft:polar_bear":     NewPolarBear,
	"minecraft:pufferfish":     NewPufferfish,
	"minecraft:phantom":        NewPhantom,
	"minecraft:rabbit":         NewRabbit,
	"minecraft:ravager":        NewRavager,
	"minecraft:salmon":         NewSalmon,
	"minecraft:sheep":          NewSheep,
	"minecraft:silverfish":     NewSilverfish,
	"minecraft:shulker": func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return NewShulker(opts, ShulkerBehaviourConfig{})
	},
	"minecraft:skeleton":         NewSkeleton,
	"minecraft:skeleton_horse":   NewSkeletonHorse,
	"minecraft:slime":            NewSlime,
	"minecraft:sniffer":          NewSniffer,
	"minecraft:snow_golem":       NewSnowGolem,
	"minecraft:spider":           NewSpider,
	"minecraft:squid":            NewSquid,
	"minecraft:stray":            NewStray,
	"minecraft:strider":          NewStrider,
	"minecraft:sulfur_cube":      NewSulfurCube,
	"minecraft:tadpole":          NewTadpole,
	"minecraft:turtle":           NewTurtle,
	"minecraft:trader_llama":     NewTraderLlama,
	"minecraft:tropical_fish":    NewTropicalFish,
	"minecraft:vex":              NewVex,
	"minecraft:villager":         NewVillager,
	"minecraft:vindicator":       NewVindicator,
	"minecraft:wandering_trader": NewWanderingTrader,
	"minecraft:warden":           NewWarden,
	"minecraft:witch":            NewWitch,
	"minecraft:wither":           NewWither,
	"minecraft:wither_skeleton":  NewWitherSkeleton,
	"minecraft:wolf":             func(opts world.EntitySpawnOpts) *world.EntityHandle { return NewWolf(opts, WolfBehaviourConfig{}) },
	"minecraft:zoglin":           NewZoglin,
	"minecraft:zombie":           NewZombie,
	"minecraft:zombie_horse":     NewZombieHorse,
	"minecraft:zombie_nautilus":  NewZombieNautilus,
	"minecraft:zombie_villager":  NewZombieVillager,
	"minecraft:zombified_piglin": NewZombifiedPiglin,
	"minecraft:happy_ghast":      NewHappyGhast,
	"minecraft:copper_golem":     NewCopperGolem,
}

// spawnMob routes Java entity identifiers to their concrete, registered species.
// A nil result is an explicit missing-factory signal; callers must not substitute
// a generic entity for an unimplemented species.
func spawnMob(id string, opts world.EntitySpawnOpts) *world.EntityHandle {
	if f, ok := spawnTable[id]; ok {
		return f(opts)
	}
	return nil
}
