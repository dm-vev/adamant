package item

import (
	"encoding/json"
	"maps"
)

// javaLegacyComponents returns the components Java's data fixer makes of
// tag, the tag of a stack of the item id from before 1.20.5, as
// ItemStackComponentizationFix.fixItemStack moves its keys into components
// and ItemStackData.write puts what is left of it in minecraft:custom_data.
// The components are given in the form Java 26.3 reads them, which the
// fixes after 1.20.5 give them: flat enchantment levels, text components
// parsed from their JSON, custom_model_data as {floats}, dyed_color as an
// int. The tooltip flags of HideFlags, which those fixes move into
// minecraft:tooltip_display, are dropped.
//
// It converts the keys whose components a stack here reads or keeps as
// they are: Damage, RepairCost, CustomModelData, Unbreakable, Enchantments,
// StoredEnchantments (of an enchanted book), display (Name, Lore, color,
// LocName, the MapColor of a filled map), Trim, the potion of a potion or
// tipped arrow, the instrument of a goat horn, the Recipes of a knowledge
// book and the map of a filled map. The others it leaves in custom_data.
func javaLegacyComponents(id string, legacy map[string]any) map[string]any {
	tag := maps.Clone(legacy)
	c := make(map[string]any)
	take := func(key string) (any, bool) {
		v, ok := tag[key]
		delete(tag, key)
		return v, ok
	}
	// moveTagToComponent: a value equal to the default IntTag(0) is none.
	moveInt := func(key, component string) {
		if v, ok := take(key); ok && v != int32(0) {
			c[component] = v
		}
	}
	hideFlags, _ := javaInt(tag["HideFlags"])
	delete(tag, "HideFlags")
	moveInt("Damage", "minecraft:damage")
	moveInt("RepairCost", "minecraft:repair_cost")
	if v, ok := take("CustomModelData"); ok {
		// CustomModelDataExpandFix: the int becomes the first of floats.
		if n, isInt := javaInt(v); isInt {
			c["minecraft:custom_model_data"] = map[string]any{"floats": []any{float32(n)}}
		}
	}
	if v, ok := take("Unbreakable"); ok {
		if n, isInt := javaInt(v); isInt && int8(n) != 0 {
			c["minecraft:unbreakable"] = map[string]any{}
		}
	}
	javaLegacyEnchantments(take, c, "Enchantments", "minecraft:enchantments")
	if id == "minecraft:enchanted_book" {
		javaLegacyEnchantments(take, c, "StoredEnchantments", "minecraft:stored_enchantments")
	}
	if display, ok := tag["display"].(map[string]any); ok {
		if rest := javaLegacyDisplay(id, display, hideFlags, c); len(rest) != 0 {
			tag["display"] = rest
		} else {
			delete(tag, "display")
		}
	}
	if v, ok := take("Trim"); ok {
		c["minecraft:trim"] = v
	}
	switch id {
	case "minecraft:potion", "minecraft:splash_potion", "minecraft:lingering_potion", "minecraft:tipped_arrow":
		contents := make(map[string]any)
		if p, ok := take("Potion"); ok && p != "minecraft:empty" {
			if _, isString := p.(string); isString {
				contents["potion"] = p
			}
		}
		if v, ok := take("CustomPotionColor"); ok {
			contents["custom_color"] = v
		}
		if v, ok := take("custom_potion_effects"); ok {
			contents["custom_effects"] = v
		}
		if len(contents) != 0 {
			c["minecraft:potion_contents"] = contents
		}
	case "minecraft:goat_horn":
		if v, ok := take("instrument"); ok {
			c["minecraft:instrument"] = v
		}
	case "minecraft:knowledge_book":
		if v, ok := take("Recipes"); ok {
			c["minecraft:recipes"] = v
		}
	case "minecraft:filled_map":
		if v, ok := take("map"); ok {
			c["minecraft:map_id"] = v
		}
	}
	if len(tag) != 0 {
		c["minecraft:custom_data"] = tag
	}
	return c
}

// javaLegacyEnchantments moves the list of {id, lvl} of key into the
// component as ItemStackComponentizationFix.fixEnchantments does: an entry
// without a string id and a number lvl is dropped, the level is clamped to
// 0..255 and a level of 0 dropped. A list that leaves none gives an
// enchantment glint instead. The fixers before it renamed minecraft:sweeping
// to minecraft:sweeping_edge (RenameEnchantmentsFix), which this does too.
func javaLegacyEnchantments(take func(string) (any, bool), c map[string]any, key, component string) {
	raw, ok := take(key)
	if !ok {
		return
	}
	levels := make(map[string]any)
	list, _ := raw.([]any)
	for _, entry := range list {
		e, _ := entry.(map[string]any)
		id, isString := e["id"].(string)
		level, isInt := javaInt(e["lvl"])
		if isString && isInt && level > 0 {
			if javaComponentKey(id) == "minecraft:sweeping" {
				id = "minecraft:sweeping_edge"
			}
			levels[id] = int32(min(level, 255))
		}
	}
	if len(levels) == 0 {
		c["minecraft:enchantment_glint_override"] = int8(1)
		return
	}
	c[component] = levels
}

// javaLegacyDisplay moves the keys of display, a display compound, into
// components as ItemStackComponentizationFix.fixDisplay does, and returns
// what is left of it: Name and each line of Lore if they are strict JSON,
// color into minecraft:dyed_color (the colour of undyed leather if
// HideFlags hides the dye alone), LocName into minecraft:item_name, and the
// MapColor of a filled map into minecraft:map_color.
func javaLegacyDisplay(id string, display map[string]any, hideFlags int64, c map[string]any) map[string]any {
	rest := maps.Clone(display)
	if name, ok := javaJSONText(rest["Name"]); ok {
		c["minecraft:custom_name"] = name
	}
	if lore, ok := rest["Lore"].([]any); ok {
		lines := make([]any, 0, len(lore))
		for _, line := range lore {
			if text, ok := javaJSONText(line); ok {
				lines = append(lines, text)
			}
		}
		c["minecraft:lore"] = lines
	}
	color, hasColor := javaInt(rest["color"])
	if hasColor || hideFlags&64 != 0 {
		if !hasColor {
			color = 10511680
		}
		c["minecraft:dyed_color"] = int32(color)
	}
	if name, ok := rest["LocName"].(string); ok {
		c["minecraft:item_name"] = map[string]any{"translate": name}
	}
	if v, ok := rest["MapColor"]; ok && id == "minecraft:filled_map" {
		c["minecraft:map_color"] = v
		delete(rest, "MapColor")
	}
	for _, key := range []string{"Name", "Lore", "color", "LocName"} {
		delete(rest, key)
	}
	return rest
}

// javaJSONText returns the text component a JSON string holds, if it is
// strict JSON (LegacyComponentDataFixUtils.isStrictlyValidJson): a string
// or a compound such as {"text": "Sword"}.
func javaJSONText(v any) (any, bool) {
	s, ok := v.(string)
	if !ok {
		return nil, false
	}
	var text any
	if err := json.Unmarshal([]byte(s), &text); err != nil {
		return nil, false
	}
	switch text.(type) {
	case string, map[string]any, []any:
		return text, true
	}
	return nil, false
}
