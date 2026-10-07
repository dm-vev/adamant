package entity

import "github.com/df-mc/dragonfly/server/world"

// spawnMob routes Java entity identifiers to their concrete, registered species.
// A nil result is an explicit missing-factory signal; callers must not substitute
// a generic entity for an unimplemented species.
func spawnMob(id string, opts world.EntitySpawnOpts) *world.EntityHandle {
	switch id {
	case "minecraft:allay":
		return NewAllay(opts)
	case "minecraft:armadillo":
		return NewArmadillo(opts)
	case "minecraft:axolotl":
		return NewAxolotl(opts)
	case "minecraft:bat":
		return NewBat(opts)
	case "minecraft:blaze":
		return NewBlaze(opts)
	case "minecraft:bogged":
		return NewBogged(opts)
	case "minecraft:breeze":
		return NewBreeze(opts)
	case "minecraft:camel":
		return NewCamel(opts)
	case "minecraft:camel_husk":
		return NewCamelHusk(opts)
	case "minecraft:cat":
		return NewCat(opts)
	case "minecraft:bee":
		return NewBee(opts)
	case "minecraft:cave_spider":
		return NewCaveSpider(opts)
	case "minecraft:chicken":
		return NewChicken(opts, ChickenBehaviourConfig{})
	case "minecraft:cod":
		return NewCod(opts)
	case "minecraft:cow":
		return NewCow(opts)
	case "minecraft:creeper":
		return NewCreeper(opts)
	case "minecraft:dolphin":
		return NewDolphin(opts)
	case "minecraft:donkey":
		return NewDonkey(opts)
	case "minecraft:drowned":
		return NewDrowned(opts)
	case "minecraft:elder_guardian":
		return NewElderGuardian(opts)
	case "minecraft:ender_dragon":
		return NewEnderDragon(opts)
	case "minecraft:enderman":
		return NewEnderman(opts)
	case "minecraft:endermite":
		return NewEndermite(opts)
	case "minecraft:evoker":
		return NewEvoker(opts)
	case "minecraft:fox":
		return NewFox(opts)
	case "minecraft:frog":
		return NewFrog(opts)
	case "minecraft:ghast":
		return NewGhast(opts)
	case "minecraft:glow_squid":
		return NewGlowSquid(opts)
	case "minecraft:goat":
		return NewGoat(opts)
	case "minecraft:guardian":
		return NewGuardian(opts)
	case "minecraft:hoglin":
		return NewHoglin(opts)
	case "minecraft:horse":
		return NewHorse(opts)
	case "minecraft:husk":
		return NewHusk(opts)
	case "minecraft:iron_golem":
		return NewIronGolem(opts)
	case "minecraft:llama":
		return NewLlama(opts)
	case "minecraft:magma_cube":
		return NewMagmaCube(opts)
	case "minecraft:mooshroom":
		return NewMooshroom(opts)
	case "minecraft:mule":
		return NewMule(opts)
	case "minecraft:nautilus":
		return NewNautilus(opts)
	case "minecraft:ocelot":
		return NewOcelot(opts)
	case "minecraft:panda":
		return NewPanda(opts)
	case "minecraft:parched":
		return NewParched(opts)
	case "minecraft:parrot":
		return NewParrot(opts)
	case "minecraft:pig":
		return NewPig(opts)
	case "minecraft:piglin":
		return NewPiglin(opts)
	case "minecraft:piglin_brute":
		return NewPiglinBrute(opts)
	case "minecraft:pillager":
		return NewPillager(opts)
	case "minecraft:polar_bear":
		return NewPolarBear(opts)
	case "minecraft:pufferfish":
		return NewPufferfish(opts)
	case "minecraft:phantom":
		return NewPhantom(opts)
	case "minecraft:rabbit":
		return NewRabbit(opts)
	case "minecraft:ravager":
		return NewRavager(opts)
	case "minecraft:salmon":
		return NewSalmon(opts)
	case "minecraft:sheep":
		return NewSheep(opts)
	case "minecraft:silverfish":
		return NewSilverfish(opts)
	case "minecraft:shulker":
		return NewShulker(opts, ShulkerBehaviourConfig{})
	case "minecraft:skeleton":
		return NewSkeleton(opts)
	case "minecraft:skeleton_horse":
		return NewSkeletonHorse(opts)
	case "minecraft:slime":
		return NewSlime(opts)
	case "minecraft:sniffer":
		return NewSniffer(opts)
	case "minecraft:snow_golem":
		return NewSnowGolem(opts)
	case "minecraft:spider":
		return NewSpider(opts)
	case "minecraft:squid":
		return NewSquid(opts)
	case "minecraft:stray":
		return NewStray(opts)
	case "minecraft:strider":
		return NewStrider(opts)
	case "minecraft:sulfur_cube":
		return NewSulfurCube(opts)
	case "minecraft:tadpole":
		return NewTadpole(opts)
	case "minecraft:turtle":
		return NewTurtle(opts)
	case "minecraft:trader_llama":
		return NewTraderLlama(opts)
	case "minecraft:tropical_fish":
		return NewTropicalFish(opts)
	case "minecraft:vex":
		return NewVex(opts)
	case "minecraft:villager":
		return NewVillager(opts)
	case "minecraft:vindicator":
		return NewVindicator(opts)
	case "minecraft:warden":
		return NewWarden(opts)
	case "minecraft:witch":
		return NewWitch(opts)
	case "minecraft:wither":
		return NewWither(opts)
	case "minecraft:wither_skeleton":
		return NewWitherSkeleton(opts)
	case "minecraft:wolf":
		return NewWolf(opts, WolfBehaviourConfig{})
	case "minecraft:zoglin":
		return NewZoglin(opts)
	case "minecraft:zombie":
		return NewZombie(opts)
	case "minecraft:zombie_horse":
		return NewZombieHorse(opts)
	case "minecraft:zombie_nautilus":
		return NewZombieNautilus(opts)
	case "minecraft:zombie_villager":
		return NewZombieVillager(opts)
	case "minecraft:zombified_piglin":
		return NewZombifiedPiglin(opts)
	case "minecraft:happy_ghast":
		return NewHappyGhast(opts)
	default:
		return nil
	}
}

// spawnMobFromNBT applies species-specific source data before first insertion.
func spawnMobFromNBT(id string, opts world.EntitySpawnOpts, nbt map[string]any) *world.EntityHandle {
	source := world.CloneNativeEntityNBT(nbt)
	if source == nil {
		source = make(map[string]any)
	}
	source["id"] = id
	handle, err := NewFromJavaNBT(source, opts)
	if err != nil {
		return nil
	}
	return handle
}
