package loot

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/item"
)

// lootItem is the item that the loot functions of an entry change, in the Java form it has until it is made:
// its id, count and components, with what the functions leave for after it is made.
type lootItem struct {
	id         string
	count      int
	components map[string]any
	// dropped is set by a function that leaves no item, so that the entry gives none.
	dropped bool
	// enchanted is set by set_enchantments, which makes a book an enchanted book.
	enchanted bool
	// stew, exploration and filters replace the item once it exists.
	stew        *item.StewType
	exploration map[string]any
	filters     []map[string]any
}

// lootFunction runs a loot function on an item, as the LootItemFunction that Java registers for its type.
type lootFunction func(it lootItem, m *modifier, c *Context) (lootItem, error)

// lootFunctions holds the loot functions by their type.
var lootFunctions = map[string]lootFunction{
	"minecraft:set_count":                    setCountFunction,
	"minecraft:limit_count":                  limitCountFunction,
	"minecraft:explosion_decay":              explosionDecayFunction,
	"minecraft:apply_bonus":                  applyBonusFunction,
	"minecraft:enchanted_count_increase":     enchantedCountIncreaseFunction,
	"minecraft:set_damage":                   setDamageFunction,
	"minecraft:set_components":               setComponentsFunction,
	"minecraft:set_enchantments":             setEnchantmentsFunction,
	"minecraft:copy_components":              copyComponentsFunction,
	"minecraft:set_potion":                   setPotionFunction,
	"minecraft:set_name":                     setNameFunction,
	"minecraft:enchant_randomly":             enchantRandomlyFunction,
	"minecraft:enchant_with_levels":          enchantWithLevelsFunction,
	"minecraft:furnace_smelt":                furnaceSmeltFunction,
	"minecraft:set_ominous_bottle_amplifier": setOminousBottleAmplifierFunction,
	"minecraft:set_stew_effect":              setStewEffectFunction,
	"minecraft:exploration_map":              explorationMapFunction,
	"minecraft:copy_state":                   copyStateFunction,
	"minecraft:set_instrument":               setInstrumentFunction,
	"minecraft:filtered":                     filteredFunction,
}

// setCountFunction ports SetItemCountFunction: the count is set, or added to when add is set.
func setCountFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	n, err := integerNumber(m.Count, c)
	if err != nil {
		return it, err
	}
	if m.Add != nil && *m.Add {
		n += it.count
	}
	it.count = n
	return it, nil
}

// limitCountFunction ports LimitCount: the count is kept between min and max, a max of 0 setting no bound.
func limitCountFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	lo, err := integerNumber(m.Min, c)
	if err != nil {
		return it, err
	}
	hi, err := integerNumber(m.Max, c)
	if err != nil {
		return it, err
	}
	if it.count < lo {
		it.count = lo
	}
	if hi > 0 && it.count > hi {
		it.count = hi
	}
	return it, nil
}

// explosionDecayFunction ports ApplyExplosionDecay: in an explosion, each item survives with a chance of one
// over the radius.
func explosionDecayFunction(it lootItem, _ *modifier, c *Context) (lootItem, error) {
	if c.ExplosionRadius > 0 {
		kept := 0
		probability := float32(1 / c.ExplosionRadius)
		for range max(0, it.count) {
			if c.Random.NextFloat() <= probability {
				kept++
			}
		}
		it.count = kept
	}
	return it, nil
}

// applyBonusFunction ports ApplyBonusCount.
func applyBonusFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	it.count = applyBonus(it.count, *m, c)
	return it, nil
}

// enchantedCountIncreaseFunction ports EnchantedCountIncreaseFunction: Looting or Fortune adds up to its level.
func enchantedCountIncreaseFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	level := 0
	if m.Enchantment == "minecraft:looting" || m.Enchantment == "minecraft:fortune" {
		level = lootEnchantmentLevel(c.ToolStack, m.Enchantment)
	}
	if level > 0 {
		it.count += int(c.Random.NextInt(int32(level + 1)))
	}
	return it, nil
}

// setDamageFunction ports SetItemDamageFunction.
func setDamageFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	value, err := number(m.Damage, c)
	if err != nil {
		return it, err
	}
	damage, ok, err := lootSetDamage(it.id, it.components, float32(value), m.Add != nil && *m.Add)
	if err != nil {
		return it, err
	}
	if ok {
		it.components["minecraft:damage"] = damage
	}
	return it, nil
}

// setComponentsFunction ports SetComponentsFunction, with the levels of enchantment components resolved.
func setComponentsFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	for k, v := range m.Components {
		if k == "minecraft:enchantments" || k == "minecraft:stored_enchantments" {
			raw, ok := v.(map[string]any)
			if !ok {
				return it, fmt.Errorf("invalid enchantment component %T", v)
			}
			levels := make(map[string]any, len(raw))
			for id, value := range raw {
				level, err := integerNumber(value, c)
				if err != nil {
					return it, err
				}
				levels[id] = int32(level)
			}
			v = levels
		}
		it.components[k] = v
	}
	return it, nil
}

// setEnchantmentsFunction ports SetEnchantmentsFunction: the levels are set, or added to when add is set, within
// 0 to 255, a level of 0 removing the enchantment.
func setEnchantmentsFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	add := m.Add != nil && *m.Add
	levels := map[string]any{}
	if existing, ok := it.components["minecraft:enchantments"].(map[string]any); ok {
		for id, level := range existing {
			levels[id] = level
		}
	}
	for id, provider := range m.Enchantments {
		if _, ok := registeredEnchantmentType(id); !ok {
			return it, fmt.Errorf("set_enchantments references unsupported enchantment %q", id)
		}
		level, err := integerNumber(provider, c)
		if err != nil {
			return it, err
		}
		if add {
			previous, err := integerNumber(levels[normalize(id)], c)
			if err != nil {
				return it, err
			}
			level += previous
		}
		level = max(0, min(255, level))
		if level == 0 {
			delete(levels, normalize(id))
		} else {
			levels[normalize(id)] = int32(level)
		}
	}
	it.components["minecraft:enchantments"] = levels
	it.enchanted = true
	return it, nil
}

// copyComponentsFunction ports CopyComponentsFunction.
func copyComponentsFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	return it, lootCopyComponents(m.Raw, it.components, c)
}

// setPotionFunction ports SetPotionFunction.
func setPotionFunction(it lootItem, m *modifier, _ *Context) (lootItem, error) {
	it.components["minecraft:potion_contents"] = map[string]any{"potion": m.Potion}
	return it, nil
}

// setNameFunction ports SetNameFunction.
func setNameFunction(it lootItem, m *modifier, _ *Context) (lootItem, error) {
	it.components["minecraft:custom_name"] = m.Name
	return it, nil
}

// enchantRandomlyFunction ports EnchantRandomlyFunction.
func enchantRandomlyFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	var err error
	it.id, it.count, it.components, err = applyRandomEnchantment(it.id, it.count, it.components, *m, c)
	return it, err
}

// enchantWithLevelsFunction ports EnchantWithLevelsFunction: the item is enchanted as at an enchanting table
// for the levels, and leaves none if enchanting gives none.
func enchantWithLevelsFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	cost, err := integerNumber(m.Levels, c)
	if err != nil {
		return it, err
	}
	base, err := item.StackFromStructureNBT(map[string]any{"id": it.id, "count": int32(it.count), "components": it.components})
	if err != nil {
		return it, err
	}
	enchanted, err := enchantWithLevels(base, cost, m.randomEnchantmentOptions, c.Random)
	if err != nil {
		return it, err
	}
	result, err := item.StackToStructureNBT(enchanted)
	if err != nil {
		return it, err
	}
	if result == nil {
		it.dropped = true
		return it, nil
	}
	it.id = result["id"].(string)
	it.count = int(result["count"].(int32))
	it.components, _ = result["components"].(map[string]any)
	if it.components == nil {
		it.components = make(map[string]any)
	}
	return it, nil
}

// furnaceSmeltFunction ports SmeltItemFunction: the item becomes its furnace product, keeping the count;
// unsmeltable items are left unchanged.
func furnaceSmeltFunction(it lootItem, _ *modifier, _ *Context) (lootItem, error) {
	base, err := item.StackFromStructureNBT(map[string]any{"id": it.id, "count": int32(max(it.count, 1)), "components": it.components})
	if err != nil {
		return it, err
	}
	recipe, ok := item.CookingRecipeFor(item.Smelting, base.Item())
	if !ok {
		return it, nil
	}
	result, err := item.StackToStructureNBT(item.NewStack(recipe.Product.Item(), max(it.count, 1)))
	if err != nil {
		return it, err
	}
	it.id = result["id"].(string)
	it.components, _ = result["components"].(map[string]any)
	if it.components == nil {
		it.components = make(map[string]any)
	}
	return it, nil
}

// setOminousBottleAmplifierFunction ports SetOminousBottleAmplifierFunction: the amplifier, clamped to 0 to 4.
func setOminousBottleAmplifierFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	amplifier, err := integerNumber(m.Amplifier, c)
	if err != nil {
		return it, err
	}
	it.components["minecraft:ominous_bottle_amplifier"] = int32(max(0, min(4, amplifier)))
	return it, nil
}

// setStewEffectFunction ports SetStewEffectFunction: the effect is chosen now and given once the item exists.
func setStewEffectFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	chosen, err := lootStewEffect(*m, c)
	if err != nil {
		return it, err
	}
	it.stew = chosen
	return it, nil
}

// explorationMapFunction ports ExplorationMapFunction, which runs once the item exists.
func explorationMapFunction(it lootItem, m *modifier, _ *Context) (lootItem, error) {
	it.exploration = m.Raw
	return it, nil
}

// copyStateFunction ports CopyBlockState.
func copyStateFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	return it, lootCopyState(m.Raw, it.components, c)
}

// setInstrumentFunction ports SetInstrumentFunction.
func setInstrumentFunction(it lootItem, m *modifier, c *Context) (lootItem, error) {
	instrument, err := lootInstrument(m.Options, c)
	if err != nil {
		return it, err
	}
	if instrument != "" {
		it.components["minecraft:instrument"] = instrument
	}
	return it, nil
}

// filteredFunction ports FilteredFunction, which runs once the item exists.
func filteredFunction(it lootItem, m *modifier, _ *Context) (lootItem, error) {
	it.filters = append(it.filters, m.Raw)
	return it, nil
}

// make makes the native stack of the item, and gives it what the functions left for after: the enchanted book
// of set_enchantments, the effect of a suspicious stew, the exploration map and the filters.
func (it lootItem) make(c *Context) (item.Stack, error) {
	if it.count <= 0 {
		return item.Stack{}, nil
	}
	native, err := item.StackFromStructureNBT(map[string]any{"id": it.id, "count": int32(it.count), "components": it.components})
	if err != nil {
		return item.Stack{}, fmt.Errorf("decode loot item %q: %w", it.id, err)
	}
	if it.enchanted && it.id == "minecraft:book" {
		native = native.WithItem(item.EnchantedBook{})
	}
	if _, ok := native.Item().(item.SuspiciousStew); ok && it.stew != nil {
		native = item.NewSuspiciousStewStack(*it.stew).Grow(native.Count() - 1)
	}
	if it.exploration != nil {
		if native, err = lootExplorationMap(native, it.exploration, c); err != nil {
			return item.Stack{}, err
		}
	}
	for _, filter := range it.filters {
		if native, err = lootFiltered(native, filter); err != nil || native.Empty() {
			return item.Stack{}, err
		}
	}
	return native, nil
}
