package loot

import (
	"fmt"
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
)

type selectedEnchantment struct {
	definition *sourceEnchantmentDefinition
	level      int
}

// selectSourceEnchantments follows EnchantmentHelper.selectEnchantment, including
// the order in which random draws occur and the source holder iteration order.
func selectSourceEnchantments(random *vanilla.Random, stack item.Stack, cost int, options []string) []selectedEnchantment {
	if stack.Empty() {
		return nil
	}
	value, ok := stack.Item().(interface{ EnchantmentValue() int })
	if !ok {
		return nil
	}
	enchantability := value.EnchantmentValue()
	cost += 1 + int(random.NextInt(int32(enchantability/4+1))) + int(random.NextInt(int32(enchantability/4+1)))
	span := (random.NextFloat() + random.NextFloat() - 1) * .15
	cost = max(1, int(math.Floor(float64(float32(cost)+float32(cost)*span+.5))))
	available := availableSourceEnchantments(stack, cost, options)
	if len(available) == 0 {
		return nil
	}
	result := []selectedEnchantment{weightedSourceEnchantment(random, available)}
	for random.NextInt(50) <= int32(cost) {
		available = filterSourceCompatible(available, result[len(result)-1].definition)
		if len(available) == 0 {
			break
		}
		result = append(result, weightedSourceEnchantment(random, available))
		cost /= 2
	}
	return result
}

func availableSourceEnchantments(stack item.Stack, cost int, options []string) []selectedEnchantment {
	id, _ := stack.Item().EncodeItem()
	book := id == "minecraft:book"
	if options == nil {
		options = make([]string, len(sourceEnchantmentDefinitions))
		for i := range options {
			options[i] = sourceEnchantmentDefinitions[i].Name
		}
	}
	available := make([]selectedEnchantment, 0, len(options))
	for _, name := range options {
		definition := sourceEnchantmentsByID[normalize(name)]
		if definition == nil || definition.Weight <= 0 {
			continue
		}
		if !book {
			_, supported := slices.BinarySearch(definition.SupportedItems, id)
			if !supported {
				continue
			}
			if len(definition.PrimaryItems) != 0 {
				_, primary := slices.BinarySearch(definition.PrimaryItems, id)
				if !primary {
					continue
				}
			}
		}
		for level := definition.MaxLevel; level >= 1; level-- {
			minCost := definition.MinCostBase + (level-1)*definition.MinCostPer
			maxCost := definition.MaxCostBase + (level-1)*definition.MaxCostPer
			if cost >= minCost && cost <= maxCost {
				available = append(available, selectedEnchantment{definition, level})
				break
			}
		}
	}
	return available
}

func weightedSourceEnchantment(random *vanilla.Random, candidates []selectedEnchantment) selectedEnchantment {
	total := int32(0)
	for _, candidate := range candidates {
		total += int32(candidate.definition.Weight)
	}
	draw := random.NextInt(total)
	for _, candidate := range candidates {
		draw -= int32(candidate.definition.Weight)
		if draw < 0 {
			return candidate
		}
	}
	panic("weighted enchantment selection exceeded total weight")
}

func filterSourceCompatible(candidates []selectedEnchantment, selected *sourceEnchantmentDefinition) []selectedEnchantment {
	filtered := candidates[:0]
	for _, candidate := range candidates {
		if sourceEnchantmentsCompatible(selected, candidate.definition) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func sourceEnchantmentsCompatible(a, b *sourceEnchantmentDefinition) bool {
	if a.Name == b.Name {
		return false
	}
	_, excluded := slices.BinarySearch(a.ExclusiveSet, b.Name)
	if excluded {
		return false
	}
	_, excluded = slices.BinarySearch(b.ExclusiveSet, a.Name)
	return !excluded
}

// enchantWithLevels applies the loot-function form of Java's enchantItem.
func enchantWithLevels(stack item.Stack, cost int, options []string, random *vanilla.Random) (item.Stack, error) {
	data, err := item.StackToStructureNBT(stack)
	if err != nil {
		return item.Stack{}, err
	}
	if data == nil {
		return stack, nil
	}
	components, _ := data["components"].(map[string]any)
	if components == nil {
		components = make(map[string]any)
	}
	component := "minecraft:enchantments"
	if id, _ := stack.Item().EncodeItem(); id == "minecraft:enchanted_book" {
		component = "minecraft:stored_enchantments"
	}
	levels, _ := components[component].(map[string]any)
	if levels == nil {
		levels = make(map[string]any)
	}
	for _, selected := range selectSourceEnchantments(random, stack, cost, options) {
		old, err := integerNumber(levels[selected.definition.Name], nil)
		if err != nil {
			return item.Stack{}, err
		}
		levels[selected.definition.Name] = int32(max(old, selected.level))
	}
	if id, _ := stack.Item().EncodeItem(); id == "minecraft:book" {
		data["id"] = "minecraft:enchanted_book"
		data["count"] = int32(1)
		components = make(map[string]any)
		component = "minecraft:stored_enchantments"
	}
	if len(levels) > 0 {
		components[component] = levels
	}
	data["components"] = components
	result, err := item.StackFromStructureNBT(data)
	if err != nil {
		return item.Stack{}, fmt.Errorf("decode enchanted item: %w", err)
	}
	return result, nil
}

// EnchantMobSpawnEquipment applies the vanilla mob-spawn equipment enchantment
// provider. The caller owns the per-slot random gate.
func EnchantMobSpawnEquipment(stack item.Stack, specialMultiplier float32, random *vanilla.Random) (item.Stack, error) {
	if random == nil {
		return item.Stack{}, fmt.Errorf("mob-spawn enchantment provider requires a random source")
	}
	maxCost := 5 + int(specialMultiplier*17)
	cost := 5 + int(random.NextInt(int32(maxCost-5+1)))
	data, err := item.StackToStructureNBT(stack)
	if err != nil {
		return item.Stack{}, err
	}
	if data == nil {
		return stack, nil
	}
	components, _ := data["components"].(map[string]any)
	if components == nil {
		components = make(map[string]any)
	}
	levels, _ := components["minecraft:enchantments"].(map[string]any)
	if levels == nil {
		levels = make(map[string]any)
	}
	options := orderedEnchantmentTags["minecraft:on_mob_spawn_equipment"]
	for _, selected := range selectSourceEnchantments(random, stack, cost, options) {
		old, _ := integerNumber(levels[selected.definition.Name], nil)
		levels[selected.definition.Name] = int32(max(old, selected.level))
	}
	if len(levels) != 0 {
		components["minecraft:enchantments"] = levels
	}
	data["components"] = components
	result, err := item.StackFromStructureNBT(data)
	if err != nil {
		return item.Stack{}, fmt.Errorf("decode mob-spawn enchanted item: %w", err)
	}
	return result, nil
}
