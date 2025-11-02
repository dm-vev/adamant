package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/painting"
	"github.com/df-mc/dragonfly/server/world"
)

// DefaultRegistry is a world.EntityRegistry that registers all default entities
// implemented by Dragonfly.
var DefaultRegistry = conf.New([]world.EntityType{
	AreaEffectCloudType,
	ArrowType,
	BottleOfEnchantingType,
	EggType,
	FishingHookType,
	EnderPearlType,
	ExperienceOrbType,
	FallingBlockType,
	FireworkType,
	ItemType,
	LightningType,
	EndCrystalType,
	LingeringPotionType,
	SnowballType,
	WindChargeType,
	TridentType,
	SplashPotionType,
	TNTType,
	PaintingType,
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
	Painting: func(opts world.EntitySpawnOpts, motive any, direction cube.Direction) *world.EntityHandle {
		m, ok := motive.(painting.Motive)
		if !ok {
			panic("world.EntityRegistryConfig.Painting: motive must be painting.Motive")
		}
		return NewPainting(opts, m, direction)
	},
	LingeringPotion: func(opts world.EntitySpawnOpts, t any, owner world.Entity) *world.EntityHandle {
		return NewLingeringPotion(opts, t.(potion.Potion), owner)
	},
	SplashPotion: func(opts world.EntitySpawnOpts, t any, owner world.Entity) *world.EntityHandle {
		return NewSplashPotion(opts, t.(potion.Potion), owner)
	},
	Arrow: func(opts world.EntitySpawnOpts, damage float64, owner world.Entity, critical, disallowPickup, obtainArrowOnPickup bool, punchLevel int, tip any) *world.EntityHandle {
		conf := arrowConf
		conf.Damage, conf.Potion, conf.Owner = damage, tip.(potion.Potion), owner.H()
		conf.KnockBackForceAddend = float64(punchLevel) * enchantment.Punch.KnockBackMultiplier()
		conf.DisablePickup = disallowPickup
		if obtainArrowOnPickup {
			conf.PickupItem = item.NewStack(item.Arrow{Tip: tip.(potion.Potion)}, 1)
		}
		conf.Critical = critical
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
