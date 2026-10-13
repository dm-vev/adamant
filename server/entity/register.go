package entity

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
)

// DefaultRegistry is a world.EntityRegistry that registers all default entities
// implemented by Dragonfly.
var DefaultRegistry world.EntityRegistry

func init() {
	DefaultRegistry = conf.New([]world.EntityType{
		ChickenType,
		HorseType,
		DonkeyType,
		MuleType,
		SkeletonHorseType,
		ZombieHorseType,
		LlamaType,
		TraderLlamaType,
		CamelType,
		CamelHuskType,
		SheepType,
		PigType,
		MooshroomType,
		RabbitType,
		GoatType,
		ArmadilloType,
		SnifferType,
		FrogType,
		VillagerType,
		WitchType,
		ZombieType,
		ZombieVillagerType,
		SkeletonType,
		WitherSkeletonType,
		SpiderType,
		CaveSpiderType,
		CreeperType,
		EndermanType,
		SilverfishType,
		EndermiteType,
		SlimeType,
		StrayType,
		BoggedType,
		HuskType,
		ParchedType,
		PhantomType,
		WardenType,
		WitherType,
		WitherSkullType,
		EvokerType,
		EvokerFangsType,
		VindicatorType,
		PillagerType,
		VexType,
		GhastType,
		BlazeType,
		MagmaCubeType,
		BreezeType,
		CreakingType,
		BreezeWindChargeType,
		OminousItemSpawnerType,
		RavagerType,
		DrownedType,
		PiglinType{},
		PiglinBruteType{},
		HoglinType,
		ZoglinType,
		StriderType,
		GuardianType,
		ElderGuardianType,
		SquidType,
		GlowSquidType,
		TurtleType,
		HappyGhastType,
		DolphinType,
		NautilusType,
		BatType,
		CatType,
		AllayType,
		OcelotType,
		FoxType,
		PandaType,
		PolarBearType,
		BeeType,
		ParrotType,
		IronGolemType,
		SnowGolemType,
		CopperGolemType,
		SulfurCubeType,
		ZombieNautilusType,
		WolfType,
		CushionType,
		AreaEffectCloudType,
		ArmourStandType,
		ArrowType,
		BottleOfEnchantingType,
		EggType,
		AxolotlType,
		CodType,
		PufferfishType,
		SalmonType,
		TadpoleType,
		TropicalFishType,
		FishingHookType,
		EnderPearlType,
		ExperienceOrbType,
		FallingBlockType,
		FireworkType,
		ItemType,
		BoatType,
		ChestBoatType,
		MinecartType,
		ChestMinecartType,
		CowType,
		HopperMinecartType,
		TNTMinecartType,
		LightningType,
		EndCrystalType,
		EnderDragonType,
		DragonFireballType,
		GhastFireballType,
		LlamaSpitType,
		LingeringPotionType,
		SnowballType,
		ShulkerType,
		ShulkerBulletType,
		SmallFireballType,
		WindChargeType,
		TridentType,
		SplashPotionType,
		TNTType,
		TextType,
		EyeOfEnderType,
	})
}

var conf = world.EntityRegistryConfig{
	NewFromBucket:    NewFromBucket,
	EnderDragon:      NewEnderDragon,
	DragonFight:      newDragonFight,
	GolemPattern:     trySpawnConstructedGolem,
	RaidSpawner:      NewRaidSpawner,
	SpawnMob:         spawnMob,
	NaturalSpawnTick: NaturalSpawnTick,
	SpawnMobFromNBT:  spawnMobFromNBT,
	ExperienceOrbs:   NewExperienceOrbs,
	SmallFireball:    NewSmallFireball,
	OminousItemSpawner: func(opts world.EntitySpawnOpts, stack any, delay int64) *world.EntityHandle {
		return NewOminousItemSpawner(opts, stack.(item.Stack), delay)
	},
	WitherPattern:      trySpawnWitherPattern,
	TNT:                NewTNT,
	Egg:                NewEgg,
	EggVariant:         NewEggVariant,
	Snowball:           NewSnowball,
	WindCharge:         NewWindCharge,
	BottleOfEnchanting: NewBottleOfEnchanting,
	EnderPearl:         NewEnderPearl,
	FallingBlock:       NewFallingBlock,
	Lightning:          NewLightning,
	ArmourStand:        NewArmourStand,
	Cushion:            NewCushion,
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
		return opts.New(MinecartType, minecartConf)
	},
	MinecartChest: func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return opts.New(ChestMinecartType, chestMinecartConf)
	},
	MinecartHopper: func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return opts.New(HopperMinecartType, hopperMinecartConf)
	},
	MinecartTNT: func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return opts.New(TNTMinecartType, tntMinecartConf)
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
		conf.Damage, conf.Potion, conf.Owner = arrow.Damage, tip, arrow.Owner.H()
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
		return opts.New(FishingHookType, FishingHookConfig{Owner: owner.H(), Rod: stack})
	},
}
