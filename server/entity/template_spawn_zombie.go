package entity

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
)

// prepareZombieVillagerTemplateSpawn follows ZombieVillager -> Zombie -> Mob
// for STRUCTURE and a null group, after the authored template NBT is loaded.
func prepareZombieVillagerTemplateSpawn(nbt map[string]any, levelRandom, entityRandom *vanilla.Random, context world.GenerationContext, biome string) (vehicle map[string]any, err error) {
	finalizeTemplateVillagerType(nbt, biome)
	prepareTemplateMobBase(nbt, 35, levelRandom)
	difficulty := context.Difficulty.SpecialMultiplier()
	nbt["CanPickUpLoot"] = boolByte(levelRandom.NextFloat() < float32(.55)*difficulty)
	spawnBaby := levelRandom.NextFloat() < .05
	if spawnBaby {
		nbt["IsBaby"] = byte(1)
		// WorldGenRegion entity queries are empty. Only the second branch
		// creates a chicken; an authored baby alone does not enter this branch.
		if float64(levelRandom.NextFloat()) >= .05 && float64(levelRandom.NextFloat()) < .05 {
			vehicle, err = prepareStructureChicken(levelRandom, biome)
			if err != nil {
				return nil, err
			}
		}
	}
	// Zombie.readAdditionalSaveData calls setBaby even for an adult, creating
	// the speed AttributeInstance. Its transient baby modifier is not saved.
	templateSpawnAttribute(nbt, "minecraft:movement_speed", float64(float32(.23)))
	nbt["CanBreakDoors"] = boolByte(levelRandom.NextFloat() < difficulty*float32(.1))
	equipment, _ := nbt["equipment"].(map[string]any)
	if equipment == nil {
		equipment = make(map[string]any)
	}
	if levelRandom.NextFloat() < float32(.15)*difficulty {
		armorType := levelRandom.NextInt(3)
		for range 3 {
			if levelRandom.NextFloat() < float32(.1087) {
				armorType++
			}
		}
		partialChance := float32(.25)
		if context.Difficulty.Base == world.DifficultyHard {
			partialChance = .1
		}
		for i, slot := range []string{"head", "chest", "legs", "feet"} {
			if i != 0 && levelRandom.NextFloat() < partialChance {
				break
			}
			current, _ := equipment[slot].(map[string]any)
			if !templateSpawnItemEmpty(current) {
				continue
			}
			material := [...]string{"leather", "copper", "golden", "chainmail", "iron", "diamond"}[armorType]
			part := [...]string{"helmet", "chestplate", "leggings", "boots"}[i]
			equipment[slot] = map[string]any{"id": "minecraft:" + material + "_" + part, "count": int32(1)}
		}
	}
	weaponChance := float32(.01)
	if context.Difficulty.Base == world.DifficultyHard {
		weaponChance = .05
	}
	if levelRandom.NextFloat() < weaponChance {
		weapon := "minecraft:iron_shovel"
		switch levelRandom.NextInt(6) {
		case 0:
			weapon = "minecraft:iron_sword"
		case 1:
			weapon = "minecraft:iron_spear"
		}
		equipment["mainhand"] = map[string]any{"id": weapon, "count": int32(1)}
	}
	for _, slot := range []string{"mainhand", "feet", "legs", "chest", "head"} {
		current, _ := equipment[slot].(map[string]any)
		if templateSpawnItemEmpty(current) {
			continue
		}
		chance := float32(.5)
		if slot == "mainhand" {
			chance = .25
		}
		if levelRandom.NextFloat() >= chance*difficulty {
			continue
		}
		stack, decodeErr := item.StackFromStructureNBT(current)
		if decodeErr != nil {
			return nil, decodeErr
		}
		stack, err = loot.EnchantMobSpawnEquipment(stack, difficulty, levelRandom)
		if err != nil {
			return nil, err
		}
		encoded, encodeErr := item.StackToStructureNBT(stack)
		if encodeErr != nil {
			return nil, encodeErr
		}
		equipment[slot] = mergeTemplateSpawnItemNBT(current, encoded)
	}
	head, _ := equipment["head"].(map[string]any)
	if templateSpawnItemEmpty(head) && context.Halloween && levelRandom.NextFloat() < .25 {
		id := "minecraft:carved_pumpkin"
		if levelRandom.NextFloat() < .1 {
			id = "minecraft:jack_o_lantern"
		}
		equipment["head"] = map[string]any{"id": id, "count": int32(1)}
		drops, _ := nbt["drop_chances"].(map[string]any)
		if drops == nil {
			drops = make(map[string]any)
			nbt["drop_chances"] = drops
		}
		drops["head"] = float32(0)
	}
	if len(equipment) != 0 {
		nbt["equipment"] = equipment
	}
	// Zombie.handleAttributes uses the independent Entity.random stream.
	reinforcement := templateSpawnAttribute(nbt, "minecraft:spawn_reinforcements", 0)
	reinforcement["base"] = entityRandom.NextDouble() * float64(float32(.1))
	knockback := templateSpawnAttribute(nbt, "minecraft:knockback_resistance", 0)
	templateReplaceSpawnModifier(knockback, "minecraft:random_spawn_bonus", entityRandom.NextDouble()*float64(float32(.05)), "add_value")
	bonus := entityRandom.NextDouble() * 1.5 * float64(difficulty)
	if bonus > 1 {
		follow := templateSpawnAttribute(nbt, "minecraft:follow_range", 35)
		templateReplaceSpawnModifier(follow, "minecraft:zombie_random_spawn_bonus", bonus, "add_multiplied_total")
	}
	if entityRandom.NextFloat() < difficulty*float32(.05) {
		templateReplaceSpawnModifier(reinforcement, "minecraft:leader_zombie_bonus", entityRandom.NextDouble()*.25+.5, "add_value")
		health := templateSpawnAttribute(nbt, "minecraft:max_health", 20)
		templateReplaceSpawnModifier(health, "minecraft:leader_zombie_bonus", entityRandom.NextDouble()*3+1, "add_multiplied_total")
		nbt["Health"] = float32(templateSpawnAttributeValue(health))
		nbt["CanBreakDoors"] = byte(1)
	}
	return vehicle, nil
}

func templateReplaceSpawnModifier(attribute map[string]any, id string, amount float64, operation string) {
	modifiers, _ := attribute["modifiers"].([]any)
	for i, value := range modifiers {
		if modifier, ok := value.(map[string]any); ok && modifier["id"] == id {
			modifiers[i] = map[string]any{"id": id, "amount": amount, "operation": operation}
			return
		}
	}
	spawnModifier(attribute, id, amount, operation)
}

func templateSpawnAttributeValue(attribute map[string]any) float64 {
	base, ok := attribute["base"].(float64)
	if !ok {
		panic(fmt.Sprintf("Source attribute base is %T, not double", attribute["base"]))
	}
	modifiers, _ := attribute["modifiers"].([]any)
	value := base
	for _, entry := range modifiers {
		modifier := entry.(map[string]any)
		if modifier["operation"] == "add_value" {
			value += modifier["amount"].(float64)
		}
	}
	added := value
	for _, entry := range modifiers {
		modifier := entry.(map[string]any)
		if modifier["operation"] == "add_multiplied_base" {
			value += added * modifier["amount"].(float64)
		}
	}
	for _, entry := range modifiers {
		modifier := entry.(map[string]any)
		if modifier["operation"] == "add_multiplied_total" {
			value *= 1 + modifier["amount"].(float64)
		}
	}
	return max(1, min(1024, value))
}
