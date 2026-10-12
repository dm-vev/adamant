package entity

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
)

// prepareNetherTemplateSpawn applies Source STRUCTURE finalizeSpawn behavior
// after the template entity's authored NBT has been loaded.
func prepareNetherTemplateSpawn(nbt map[string]any, levelRandom, entityRandom *java26.Random, context world.GenerationContext, pos [3]int32, dimension string) error {
	if levelRandom == nil || entityRandom == nil {
		return fmt.Errorf("nether template spawn requires level and entity random streams")
	}
	if nbt == nil {
		return fmt.Errorf("nether template spawn requires entity NBT")
	}
	id, _ := nbt["id"].(string)
	switch id {
	case "minecraft:piglin":
		return prepareTemplatePiglin(nbt, levelRandom, context)
	case "minecraft:piglin_brute":
		return prepareTemplatePiglinBrute(nbt, levelRandom, pos, dimension)
	case "minecraft:hoglin":
		return prepareTemplateHoglin(nbt, levelRandom)
	default:
		return fmt.Errorf("unsupported nether template mob %q", id)
	}
}

func prepareTemplatePiglin(nbt map[string]any, levelRandom *java26.Random, context world.GenerationContext) error {
	// PiglinAi.initMemories samples a 30–120 second hunt-cooldown TTL first.
	ttl := int64(600 + levelRandom.NextInt(1801))
	setTemplateBrainMemory(nbt, "minecraft:hunted_recently", byte(1), ttl)

	var adult bool
	if value, ok := nbt["IsBaby"].(byte); ok {
		adult = value == 0
	} else if value, ok := nbt["IsBaby"].(bool); ok {
		adult = !value
	} else {
		adult = true
	}
	equipment, _ := nbt["equipment"].(map[string]any)
	if equipment == nil {
		equipment = make(map[string]any)
	}
	if adult {
		for _, slot := range []struct{ key, id string }{
			{"head", "minecraft:golden_helmet"}, {"chest", "minecraft:golden_chestplate"},
			{"legs", "minecraft:golden_leggings"}, {"feet", "minecraft:golden_boots"},
		} {
			if levelRandom.NextFloat() < .1 {
				encoded, err := templateSpawnItem(slot.id)
				if err != nil {
					return err
				}
				equipment[slot.key] = encoded
			}
		}
	}
	// Mob.populateDefaultEquipmentEnchantments considers occupied slots. An
	// authored main hand remains untouched by STRUCTURE and can be enchanted.
	for _, key := range []string{"mainhand", "feet", "legs", "chest", "head"} {
		value, exists := equipment[key]
		if !exists {
			continue
		}
		stackNBT, ok := value.(map[string]any)
		if !ok || templateSpawnItemEmpty(stackNBT) {
			continue
		}
		chance := float32(.5)
		if key == "mainhand" {
			chance = .25
		}
		if levelRandom.NextFloat() >= chance*context.Difficulty.SpecialMultiplier() {
			continue
		}
		stack, err := item.StackFromJavaNBT(stackNBT)
		if err != nil {
			return err
		}
		stack, err = loot.EnchantMobSpawnEquipment(stack, context.Difficulty.SpecialMultiplier(), levelRandom)
		if err != nil {
			return err
		}
		enchanted, err := item.StackToJavaNBT(stack)
		if err != nil {
			return err
		}
		equipment[key] = mergeTemplateSpawnItemNBT(stackNBT, enchanted)
	}
	if len(equipment) > 0 {
		nbt["equipment"] = equipment
	}
	prepareTemplateMobBase(nbt, 16, levelRandom)
	return nil
}

func prepareTemplatePiglinBrute(nbt map[string]any, levelRandom *java26.Random, pos [3]int32, dimension string) error {
	setTemplateBrainMemory(nbt, "minecraft:home", map[string]any{
		"dimension": dimension, "pos": [3]int32{pos[0], pos[1], pos[2]},
	}, 0)
	axe, err := templateSpawnItem("minecraft:golden_axe")
	if err != nil {
		return err
	}
	equipment, _ := nbt["equipment"].(map[string]any)
	if equipment == nil {
		equipment = make(map[string]any)
	}
	equipment["mainhand"] = axe
	nbt["equipment"] = equipment
	prepareTemplateMobBase(nbt, 12, levelRandom)
	return nil
}

func prepareTemplateHoglin(nbt map[string]any, levelRandom *java26.Random) error {
	if levelRandom.NextFloat() < .2 {
		nbt["Age"] = int32(-24000)
	}
	prepareTemplateMobBase(nbt, 16, levelRandom)
	return nil
}

func setTemplateBrainMemory(nbt map[string]any, module string, value any, expiry int64) {
	brain, _ := nbt["Brain"].(map[string]any)
	if brain == nil {
		brain = make(map[string]any)
	}
	memories, _ := brain["memories"].(map[string]any)
	if memories == nil {
		memories = make(map[string]any)
	}
	payload := map[string]any{"value": value}
	if expiry > 0 {
		payload["ttl"] = expiry
	}
	memories[module] = payload
	brain["memories"] = memories
	nbt["Brain"] = brain
}
func templateSpawnItemEmpty(nbt map[string]any) bool {
	id, _ := nbt["id"].(string)
	count := int64(1)
	switch value := nbt["count"].(type) {
	case byte:
		count = int64(value)
	case int32:
		count = int64(value)
	case int64:
		count = value
	}
	return id == "" || id == "minecraft:air" || count <= 0
}

func mergeTemplateSpawnItemNBT(original, enchanted map[string]any) map[string]any {
	merged := make(map[string]any, len(original)+1)
	for key, value := range original {
		merged[key] = value
	}
	if count, exists := enchanted["count"]; exists {
		merged["count"] = count
	}
	originalComponents, _ := original["components"].(map[string]any)
	enchantedComponents, _ := enchanted["components"].(map[string]any)
	if len(enchantedComponents) > 0 {
		components := make(map[string]any, len(originalComponents)+len(enchantedComponents))
		for key, value := range originalComponents {
			components[key] = value
		}
		for key, value := range enchantedComponents {
			components[key] = value
		}
		merged["components"] = components
	}
	return merged
}

func templateSpawnItem(id string) (map[string]any, error) {
	var stack item.Stack
	switch id {
	case "minecraft:golden_helmet":
		stack = item.NewStack(item.Helmet{Tier: item.ArmourTierGold{}}, 1)
	case "minecraft:golden_chestplate":
		stack = item.NewStack(item.Chestplate{Tier: item.ArmourTierGold{}}, 1)
	case "minecraft:golden_leggings":
		stack = item.NewStack(item.Leggings{Tier: item.ArmourTierGold{}}, 1)
	case "minecraft:golden_boots":
		stack = item.NewStack(item.Boots{Tier: item.ArmourTierGold{}}, 1)
	case "minecraft:golden_axe":
		stack = item.NewStack(item.Axe{Tier: item.ToolTierGold}, 1)
	default:
		return nil, fmt.Errorf("unsupported nether template item %q", id)
	}
	return item.StackToJavaNBT(stack)
}
