package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// spawnEggOffspring ports SpawnEggItem.spawnOffspringFromSpawnEgg: an
// ageable mob makes its breeding offspring, any other mob a fresh one made a
// baby, and a mob that can't be a baby gives none. The zombie and fox
// onOffspringSpawnedFromEgg overrides follow.
func spawnEggOffspring(tx *world.Tx, parent, spawner world.Entity) *world.EntityHandle {
	id := parent.H().Type().EncodeEntity()
	var h *world.EntityHandle
	if b, ok := parent.(breeder); ok && !spawnEggNoOffspring[id] {
		h = b.breedOffspring(tx, b)
	}
	if v, ok := parent.(*Villager); ok {
		h = v.eggOffspring(tx)
	}
	if h == nil {
		data := spawnEggBabyNBT(tx, parent, id)
		if data == nil {
			return nil
		}
		h = spawnMobFromNBT(id, world.EntitySpawnOpts{Position: parent.Position()}, data)
		if h == nil {
			return nil
		}
	}
	if fox, ok := tx.AddEntity(h).(*Fox); ok && spawner != nil {
		s, trusted := fox.b(), spawner.H().UUID().String()
		if len(s.trusted) == 0 || s.trusted[0] != trusted {
			s.trusted = append(s.trusted, trusted)
		}
	}
	return h
}

// spawnEggNoOffspring holds the ageable mobs whose getBreedOffspring
// returns null.
var spawnEggNoOffspring = map[string]bool{
	"minecraft:camel_husk": true, "minecraft:parrot": true, "minecraft:wandering_trader": true, "minecraft:zombie_nautilus": true,
}

// spawnEggBabyNBT returns the state of the baby of a mob that is not a Go
// breeder, or nil if the mob has none: AgeableMob.setBaby sets Age to
// -24000, and zombies, piglins and zoglins keep an IsBaby flag.
func spawnEggBabyNBT(tx *world.Tx, parent world.Entity, id string) map[string]any {
	data := map[string]any{"Age": int32(babyStartAge), "IsBaby": byte(1)}
	switch id {
	case "minecraft:dolphin", "minecraft:squid", "minecraft:glow_squid", "minecraft:polar_bear", "minecraft:frog",
		"minecraft:turtle", "minecraft:piglin", "minecraft:zoglin":
	case "minecraft:sulfur_cube":
		data["Size"] = int32(0)
	case "minecraft:nautilus":
		if n, ok := parent.(*Nautilus); ok && n.b().tamed {
			data["Tame"] = byte(1)
		}
	case "minecraft:zombie", "minecraft:husk", "minecraft:drowned", "minecraft:zombie_villager", "minecraft:zombified_piglin":
		// Zombie.onOffspringSpawnedFromEgg.
		multiplier := tx.LocalDifficulty(cube.PosFromVec3(parent.Position())).SpecialMultiplier()
		data["CanPickUpLoot"] = boolByte(rand.Float32() < 0.55*multiplier)
	default:
		return nil
	}
	return data
}

// eggOffspring ports Villager.getBreedOffspring with itself as the partner:
// the baby takes the biome's type half of the time, otherwise the parent's.
func (v *Villager) eggOffspring(tx *world.Tx) *world.EntityHandle {
	variant := villagerTypeForBiome(world.JavaBiomeName(tx.Biome(cube.PosFromVec3(v.Position()))))
	if rand.Float64() >= 0.5 {
		variant = v.Variant()
	}
	conf := VillagerBehaviourConfig{Profession: "minecraft:none", Type: variant, Level: 1, Age: babyStartAge, Finalized: true}
	return world.EntitySpawnOpts{Position: v.Position()}.New(VillagerType, conf)
}
