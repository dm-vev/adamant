package loot

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type sourceEnchantmentDefinition struct {
	Name           string   `json:"name"`
	MaxLevel       int      `json:"max_level"`
	SupportedItems []string `json:"supported_items"`
}

var sourceEnchantmentDefinitions []sourceEnchantmentDefinition
var sourceEnchantmentsByID map[string]*sourceEnchantmentDefinition
var orderedEnchantmentTags map[string][]string

func loadSourceEnchantments() error {
	data, err := assets.ReadFile("assets/minecraft/enchantment_registry.json")
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, &sourceEnchantmentDefinitions); err != nil {
		return err
	}
	sourceEnchantmentsByID = make(map[string]*sourceEnchantmentDefinition, len(sourceEnchantmentDefinitions))
	for i := range sourceEnchantmentDefinitions {
		definition := &sourceEnchantmentDefinitions[i]
		slices.Sort(definition.SupportedItems)
		sourceEnchantmentsByID[definition.Name] = definition
	}
	return nil
}

// Named holder sets retain source tag order; direct sets retain their list order.
// Resolve once while decoding the immutable table, not during each roll.
func (m *modifier) resolveRandomEnchantmentOptions() error {
	switch value := m.Options.(type) {
	case nil:
		return nil
	case string:
		if strings.HasPrefix(value, "#") {
			options, ok := orderedEnchantmentTags[normalize(value[1:])]
			if !ok {
				return fmt.Errorf("unknown random-enchantment tag %q", value)
			}
			m.randomEnchantmentOptions = options
		} else {
			m.randomEnchantmentOptions = []string{normalize(value)}
		}
	case []any:
		m.randomEnchantmentOptions = make([]string, len(value))
		for i, raw := range value {
			name, ok := raw.(string)
			if !ok {
				return fmt.Errorf("invalid random-enchantment option %T", raw)
			}
			m.randomEnchantmentOptions[i] = normalize(name)
		}
	default:
		return fmt.Errorf("invalid random-enchantment holder set %T", value)
	}
	for _, name := range m.randomEnchantmentOptions {
		if _, ok := sourceEnchantmentsByID[name]; !ok {
			return fmt.Errorf("unknown random enchantment %q", name)
		}
	}
	return nil
}

func applyRandomEnchantment(id string, stackCount int, components map[string]any, m modifier, c *Context) (string, int, map[string]any, error) {
	id = normalize(id)
	count := len(sourceEnchantmentDefinitions)
	if m.Options != nil {
		count = len(m.randomEnchantmentOptions)
	}
	at := func(i int) *sourceEnchantmentDefinition {
		if m.Options == nil {
			return &sourceEnchantmentDefinitions[i]
		}
		return sourceEnchantmentsByID[m.randomEnchantmentOptions[i]]
	}
	checkCompatibility := id != "minecraft:book" && (m.OnlyCompatible == nil || *m.OnlyCompatible)
	compatible := func(definition *sourceEnchantmentDefinition) bool {
		if !checkCompatibility {
			return true
		}
		_, ok := slices.BinarySearch(definition.SupportedItems, id)
		return ok
	}
	available := 0
	for i := range count {
		if compatible(at(i)) {
			available++
		}
	}
	if available == 0 {
		return id, stackCount, components, nil
	}
	selected := int(c.Random.NextInt(int32(available)))
	var definition *sourceEnchantmentDefinition
	for i := range count {
		candidate := at(i)
		if !compatible(candidate) {
			continue
		}
		if selected == 0 {
			definition = candidate
			break
		}
		selected--
	}
	level := 1
	if definition.MaxLevel > 1 {
		level += int(c.Random.NextInt(int32(definition.MaxLevel)))
	}
	component := "minecraft:enchantments"
	if id == "minecraft:book" {
		id = "minecraft:enchanted_book"
		stackCount = 1
		components = make(map[string]any, 1)
	}
	if id == "minecraft:enchanted_book" {
		component = "minecraft:stored_enchantments"
	}
	levels, ok := components[component].(map[string]any)
	if !ok {
		levels = make(map[string]any)
		components[component] = levels
	}
	previous, err := integerNumber(levels[definition.Name], c)
	if err != nil {
		return "", 0, nil, err
	}
	levels[definition.Name] = int32(max(previous, level))
	return id, stackCount, components, nil
}
