package entity

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/item"
)

// projectTemplateMobNBT supplies the native fields consumed by the concrete
// NPC codecs. The independent Source snapshot retains bases/modifiers, Brain,
// sound variants and fields with no equivalent native actor tag.
func projectTemplateMobNBT(source map[string]any) (map[string]any, error) {
	id, _ := source["id"].(string)
	if _, known := templateSpawnRegistry.AllowedInPeaceful[id]; !known || id == "minecraft:armor_stand" || id == "minecraft:cushion" {
		return nil, nil
	}
	native := make(map[string]any)
	attributes, _ := source["attributes"].([]any)
	projected := make([]NativeMobAttribute, 0, len(attributes)+1)
	hasHealth := false
	for _, value := range attributes {
		attribute, ok := value.(map[string]any)
		if !ok {
			continue
		}
		name, _ := attribute["id"].(string)
		base := nativeNBTNumber(attribute["base"])
		current := sourceTemplateAttributeValue(base, attribute["modifiers"])
		minimum, maximum := 0.0, 2048.0
		switch name {
		case "minecraft:max_health":
			name, minimum, maximum, hasHealth = "minecraft:health", 1, 1024, true
		case "minecraft:movement_speed":
			name, maximum = "minecraft:movement", 1024
		case "minecraft:jump_strength":
			name, maximum = "minecraft:horse.jump_strength", 32
		case "minecraft:follow_range", "minecraft:attack_damage", "minecraft:armor", "minecraft:armor_toughness":
		case "minecraft:knockback_resistance", "minecraft:spawn_reinforcements":
			maximum = 1
		default:
			continue
		}
		current = math.Max(minimum, math.Min(maximum, current))
		entry := NativeMobAttribute{Name: name, Base: current, Current: current, DefaultMin: minimum, DefaultMax: current, Min: minimum, Max: maximum}
		if name == "minecraft:health" {
			if health, present := source["Health"]; present {
				entry.Current = nativeNBTNumber(health)
			}
		}
		projected = append(projected, entry)
	}
	if !hasHealth {
		maximum := templateMobDefaultHealth(id)
		current := maximum
		if health, present := source["Health"]; present {
			current = nativeNBTNumber(health)
		}
		projected = append(projected, NativeMobAttribute{Name: "minecraft:health", Base: maximum, Current: current, DefaultMax: maximum, Max: maximum})
	}
	native["Attributes"] = EncodeNativeMobAttributes(nil, projected)
	if equipment, ok := source["equipment"].(map[string]any); ok {
		decode := func(slot string) (NativeMobItem, error) {
			raw, _ := equipment[slot].(map[string]any)
			if templateSpawnItemEmpty(raw) {
				return nativeMobItemFromStack(item.Stack{}), nil
			}
			stack, err := item.StackFromStructureNBT(raw)
			if err != nil {
				return NativeMobItem{}, fmt.Errorf("Source %s %s: %w", id, slot, err)
			}
			return nativeMobItemFromStack(stack), nil
		}
		out := NativeMobEquipment{Armor: make([]NativeMobItem, 4)}
		for index, slot := range [...]string{"head", "chest", "legs", "feet"} {
			value, err := decode(slot)
			if err != nil {
				return nil, err
			}
			out.Armor[index] = value
		}
		main, err := decode("mainhand")
		if err != nil {
			return nil, err
		}
		off, err := decode("offhand")
		if err != nil {
			return nil, err
		}
		out.Mainhand, out.Offhand = []NativeMobItem{main}, []NativeMobItem{off}
		for key, value := range EncodeNativeMobEquipment(out) {
			native[key] = value
		}
	}
	return native, nil
}

func templateMobDefaultHealth(id string) float64 {
	switch id {
	case "minecraft:cat", "minecraft:cow", "minecraft:pig":
		return 10
	case "minecraft:sheep":
		return 8
	case "minecraft:iron_golem":
		return 100
	case "minecraft:camel":
		return 32
	case "minecraft:piglin":
		return 16
	case "minecraft:piglin_brute":
		return 50
	case "minecraft:hoglin":
		return 40
	default:
		return 20
	}
}

// AttributeInstance applies each operation in encounter order; summing a
// modifier group before multiplication changes double rounding at boundaries.
func sourceTemplateAttributeValue(base float64, raw any) float64 {
	modifiers, _ := raw.([]any)
	value := base
	for _, entry := range modifiers {
		modifier := entry.(map[string]any)
		if modifier["operation"] == "add_value" {
			value += nativeNBTNumber(modifier["amount"])
		}
	}
	added := value
	for _, entry := range modifiers {
		modifier := entry.(map[string]any)
		if modifier["operation"] == "add_multiplied_base" {
			value += added * nativeNBTNumber(modifier["amount"])
		}
	}
	for _, entry := range modifiers {
		modifier := entry.(map[string]any)
		if modifier["operation"] == "add_multiplied_total" {
			value *= 1 + nativeNBTNumber(modifier["amount"])
		}
	}
	return value
}
