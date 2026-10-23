package entity

import (
	"encoding/binary"
	"fmt"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// PrepareDrownedStructureSpawn performs the WorldGenRegion spawn chain before
// the marker is replaced. Level and Entity random sources have separate owners.
// The returned vehicle, if any, must enter the world before its drowned rider.
// No World transactions or native entity implementations run on the worker.
func PrepareDrownedStructureSpawn(levelRandom, entityRandom *vanilla.Random, context world.GenerationContext, biome string) (drowned, vehicle map[string]any, err error) {
	if levelRandom == nil || entityRandom == nil {
		return nil, nil, fmt.Errorf("drowned structure spawn requires level and entity random streams")
	}
	difficulty := context.Difficulty.SpecialMultiplier()
	drowned = map[string]any{"id": "minecraft:drowned", "PersistenceRequired": byte(1), "Health": float32(20)}
	follow := spawnFollowRange(levelRandom, 35)
	drowned["LeftHanded"] = boolByte(levelRandom.NextFloat() < .05)
	drowned["CanPickUpLoot"] = boolByte(levelRandom.NextFloat() < float32(.55)*difficulty)
	baby := levelRandom.NextFloat() < .05
	drowned["IsBaby"] = boolByte(baby)
	if baby {
		// WorldGenRegion.getEntities is empty. The first branch still consumes
		// its draw; only the else-if branch can create a new chicken jockey.
		if float64(levelRandom.NextFloat()) >= .05 && float64(levelRandom.NextFloat()) < .05 {
			vehicle, err = prepareStructureChicken(levelRandom, biome)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	doors := levelRandom.NextFloat() < difficulty*float32(.1)
	equipment := make(map[string]any, 2)
	if levelRandom.NextFloat() > .9 {
		var main item.Stack
		if levelRandom.NextInt(16) < 10 {
			main = item.NewStack(item.Trident{}, 1)
		} else {
			main = item.NewStack(item.FishingRod{}, 1)
		}
		if levelRandom.NextFloat() < float32(.25)*difficulty {
			main, err = loot.EnchantMobSpawnEquipment(main, difficulty, levelRandom)
			if err != nil {
				return nil, nil, err
			}
		}
		equipment["mainhand"], err = item.StackToStructureNBT(main)
		if err != nil {
			return nil, nil, err
		}
	}
	dropChances := make(map[string]any, 2)
	if context.Halloween && levelRandom.NextFloat() < .25 {
		head := "minecraft:carved_pumpkin"
		if levelRandom.NextFloat() < .1 {
			head = "minecraft:jack_o_lantern"
		}
		equipment["head"] = map[string]any{"id": head, "count": int32(1)}
		dropChances["head"] = float32(0)
	}
	// Zombie.handleAttributes consumes Entity.random, never the region stream.
	reinforcement := spawnAttribute("minecraft:spawn_reinforcements", entityRandom.NextDouble()*float64(float32(.1)))
	knockback := spawnAttribute("minecraft:knockback_resistance", 0)
	spawnModifier(knockback, "minecraft:random_spawn_bonus", entityRandom.NextDouble()*float64(float32(.05)), "add_value")
	followBonus := entityRandom.NextDouble() * 1.5 * float64(difficulty)
	if followBonus > 1 {
		spawnModifier(follow, "minecraft:zombie_random_spawn_bonus", followBonus, "add_multiplied_total")
	}
	attributes := []any{follow, reinforcement, knockback}
	if baby {
		attributes = append(attributes, spawnAttribute("minecraft:movement_speed", float64(float32(.23))))
	}
	if entityRandom.NextFloat() < difficulty*float32(.05) {
		spawnModifier(reinforcement, "minecraft:leader_zombie_bonus", entityRandom.NextDouble()*.25+.5, "add_value")
		health := spawnAttribute("minecraft:max_health", 20)
		bonus := entityRandom.NextDouble()*3 + 1
		spawnModifier(health, "minecraft:leader_zombie_bonus", bonus, "add_multiplied_total")
		attributes = append(attributes, health)
		drowned["Health"] = float32(20 * (1 + bonus))
		doors = true
	}
	drowned["CanBreakDoors"], drowned["attributes"] = boolByte(doors), attributes
	if levelRandom.NextFloat() < .03 {
		equipment["offhand"] = map[string]any{"id": "minecraft:nautilus_shell", "count": int32(1)}
		dropChances["offhand"] = float32(2)
	}
	if len(equipment) != 0 {
		drowned["equipment"] = equipment
	}
	if len(dropChances) != 0 {
		drowned["drop_chances"] = dropChances
	}
	if main, ok := equipment["mainhand"].(map[string]any); ok && main["id"] == "minecraft:trident" && levelRandom.NextFloat() < .5 && !baby {
		frequent, tagErr := gamedata.BiomeTagContains("minecraft:more_frequent_drowned_spawns", biome)
		if tagErr != nil {
			return nil, nil, tagErr
		}
		if !frequent {
			vehicle, err = prepareStructureZombieNautilus(levelRandom, biome)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	return drowned, vehicle, nil
}

func spawnAttribute(id string, base float64) map[string]any {
	return map[string]any{"id": id, "base": base}
}
func spawnModifier(attribute map[string]any, id string, amount float64, operation string) {
	modifiers, _ := attribute["modifiers"].([]any)
	attribute["modifiers"] = append(modifiers, map[string]any{"id": id, "amount": amount, "operation": operation})
}
func spawnFollowRange(random *vanilla.Random, base float64) map[string]any {
	attribute := spawnAttribute("minecraft:follow_range", base)
	spawnModifier(attribute, "minecraft:random_spawn_bonus", .11485000000000001*(random.NextDouble()-random.NextDouble()), "add_multiplied_base")
	return attribute
}

func prepareStructureZombieNautilus(random *vanilla.Random, biome string) (map[string]any, error) {
	warm, err := gamedata.BiomeTagContains("minecraft:spawns_coral_variant_zombie_nautilus", biome)
	if err != nil {
		return nil, err
	}
	variant := "minecraft:temperate"
	if warm {
		variant = "minecraft:warm"
	}
	// PriorityProvider.pick consumes nextInt even when exactly one variant wins.
	random.NextInt(1)
	cooldown := int32(2400) + random.NextInt(1201)
	follow := spawnFollowRange(random, 16)
	leftHanded := random.NextFloat() < .05
	return map[string]any{
		"id": "minecraft:zombie_nautilus", "PersistenceRequired": byte(1), "Health": float32(15), "variant": variant,
		"LeftHanded": boolByte(leftHanded), "attributes": []any{follow},
		"Brain": map[string]any{"memories": map[string]any{"minecraft:attack_target_cooldown": map[string]any{"value": cooldown}}},
	}, nil
}

func prepareStructureChicken(random *vanilla.Random, biome string) (map[string]any, error) {
	variant := "minecraft:temperate"
	for _, candidate := range []struct{ tag, variant string }{{"minecraft:spawns_cold_variant_farm_animals", "minecraft:cold"}, {"minecraft:spawns_warm_variant_farm_animals", "minecraft:warm"}} {
		matches, err := gamedata.BiomeTagContains(candidate.tag, biome)
		if err != nil {
			return nil, err
		}
		if matches {
			variant = candidate.variant
			break
		}
	}
	random.NextInt(1)
	sound := [...]string{"minecraft:classic", "minecraft:picky"}[random.NextInt(2)]
	follow := spawnFollowRange(random, 16)
	leftHanded := random.NextFloat() < .05
	// Chicken's egg timer belongs to its newly created independent RNG. It
	// must not draw from either its rider's stream or the WorldGenRegion.
	id := uuid.New()
	childRandom := vanilla.NewLegacyRandom(int64(binary.LittleEndian.Uint64(id[:8]) ^ binary.LittleEndian.Uint64(id[8:])))
	return map[string]any{
		"id": "minecraft:chicken", "Health": float32(4), "IsChickenJockey": byte(1), "Age": int32(0),
		"variant": variant, "sound_variant": sound, "EggLayTime": int32(6000) + childRandom.NextInt(6000),
		"LeftHanded": boolByte(leftHanded), "attributes": []any{follow},
	}, nil
}
