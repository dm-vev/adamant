package entity

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
)

// DefaultRegistry is a world.EntityRegistry that registers all default entities
// implemented by Dragonfly.
var DefaultRegistry = conf.New([]world.EntityType{
	AreaEffectCloudType,
	ArmourStandType,
	ArrowType,
	BottleOfEnchantingType,
	EggType,
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
	HopperMinecartType,
	TNTMinecartType,
	LightningType,
	EndCrystalType,
	LingeringPotionType,
	SnowballType,
	WindChargeType,
	TridentType,
	SplashPotionType,
	TNTType,
	TextType,
})

var conf = world.EntityRegistryConfig{
	TNT:                NewTNT,
	Egg:                NewEgg,
	Snowball:           NewSnowball,
	WindCharge:         NewWindCharge,
	BottleOfEnchanting: NewBottleOfEnchanting,
	EnderPearl:         NewEnderPearl,
	FallingBlock:       NewFallingBlock,
	Lightning:          NewLightning,
	ArmourStand:        NewArmourStand,
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
