package item

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/world"
)

// StackFromJavaNBT decodes a Java 1.20.5+ item stack compound into a Dragonfly
// stack. Unknown item identifiers are rejected rather than silently discarded.
func StackFromJavaNBT(data map[string]any) (Stack, error) {
	id, ok := data["id"].(string)
	if !ok || id == "" {
		return Stack{}, fmt.Errorf("Java item stack has no string id")
	}
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	var it world.Item
	if variant, mapID := FilledMapVariantFromJavaItemID(id); mapID {
		it = FilledMap{Variant: variant}
	} else if id == "minecraft:knowledge_book" {
		it = KnowledgeBook{}
	} else {
		var ok bool
		it, ok = world.ItemByName(id, 0)
		if !ok {
			return Stack{}, fmt.Errorf("unsupported Java item %q", id)
		}
	}
	count := 1
	if n, ok := javaInt(data["count"]); ok {
		count = int(n)
	}
	if n, ok := javaInt(data["Count"]); ok {
		count = int(n)
	}
	if count < 0 {
		return Stack{}, fmt.Errorf("Java item %q has negative count %d", id, count)
	}
	s := NewStack(it, count)
	components := make(map[string]any)
	if source, ok := data["components"].(map[string]any); ok {
		for k, v := range source {
			components[k] = v
		}
	}
	if legacy, ok := data["tag"].(map[string]any); ok {
		for k, v := range legacy {
			if _, exists := components[k]; !exists {
				components[k] = v
			}
		}
	}
	for key, value := range components {
		switch key {
		case "minecraft:damage", "damage", "Damage":
			if d, ok := javaInt(value); ok {
				if maxDamage := s.MaxDurability(); maxDamage > 0 {
					s = s.WithDurability(maxDamage - int(d))
				}
			}
		case "minecraft:custom_name", "minecraft:item_name", "custom_name", "item_name":
			if name, ok := javaText(value); ok {
				s = s.WithCustomName(name)
			}
		case "minecraft:lore", "lore":
			if lines, ok := value.([]any); ok {
				lore := make([]string, 0, len(lines))
				for _, line := range lines {
					if text, ok := javaText(line); ok {
						lore = append(lore, text)
					}
				}
				s = s.WithLore(lore...)
			}
		case "display":
			if display, ok := value.(map[string]any); ok {
				if name, ok := javaText(display["Name"]); ok {
					s = s.WithCustomName(name)
				}
				if lore, ok := display["Lore"].([]any); ok {
					lines := make([]string, 0, len(lore))
					for _, line := range lore {
						if text, ok := javaText(line); ok { lines = append(lines, text) }
					}
					s = s.WithLore(lines...)
				}
			}
		case "minecraft:repair_cost", "RepairCost":
			if cost, ok := javaInt(value); ok { s = s.WithAnvilCost(int(cost)) }
		case "minecraft:ominous_bottle_amplifier":
			amplifier, ok := javaInt(value)
			if !ok || amplifier < 0 || amplifier > 4 {
				return Stack{}, fmt.Errorf("Java ominous bottle amplifier %v is outside 0..4", value)
			}
			if _, ok := s.Item().(OminousBottle); !ok {
				return Stack{}, fmt.Errorf("Java ominous bottle amplifier is attached to item %q", id)
			}
			s = s.WithItem(OminousBottle{Amplifier: int(amplifier)})
		case "minecraft:map_id":
			mapID, ok := javaInt(value)
			if !ok || mapID < 0 || mapID > 1<<31-1 {
				return Stack{}, fmt.Errorf("Java map id %v is outside the valid integer range", value)
			}
			mapItem, ok := s.Item().(FilledMap)
			if !ok {
				return Stack{}, fmt.Errorf("Java map id is attached to non-map item %q", id)
			}
			mapItem.JavaID = int32(mapID)
			mapItem.HasJavaID = true
			s = s.WithItem(mapItem)
		case "minecraft:recipes":
			book, ok := s.Item().(KnowledgeBook)
			if !ok {
				return Stack{}, fmt.Errorf("Java recipes component is attached to non-knowledge-book item %q", id)
			}
			recipes, ok := javaRecipeIDs(value)
			if !ok {
				return Stack{}, fmt.Errorf("Java knowledge book recipes component has type %T, want list of resource IDs", value)
			}
			book.Recipes = recipes
			s = s.WithItem(book)
		case "minecraft:unbreakable", "Unbreakable":
			if unbreakable(value) { s = s.AsUnbreakable() }
		case "minecraft:enchantments", "minecraft:stored_enchantments", "ench", "Enchantments", "StoredEnchantments":
			applyJavaEnchantments(&s, value)
	}

	}
	if len(components) != 0 {
		if encoded, err := json.Marshal(components); err == nil {
			s = s.WithValue("java_components_json", string(encoded))
		}
	}
	if custom, exists := components["minecraft:custom_data"]; exists {
		if encoded, err := json.Marshal(custom); err == nil {
			s = s.WithValue("java_custom_data_json", string(encoded))
		}
	}
	return s, nil
}

// StackToJavaNBT encodes a Dragonfly stack as a Java ItemStack.CODEC compound.
// Raw Java components retained by StackFromJavaNBT are preserved where possible.
func StackToJavaNBT(stack Stack) (map[string]any, error) {
	if stack.Empty() {
		return nil, nil
	}
	mapItem, isMap := stack.Item().(FilledMap)
	if isMap && !mapItem.Variant.Valid() {
		return nil, fmt.Errorf("invalid filled map variant %d", mapItem.Variant)
	}
	name, _ := stack.Item().EncodeItem()
	if isMap {
		name = mapItem.Variant.JavaItemID()
	}
	if _, knowledgeBook := stack.Item().(KnowledgeBook); knowledgeBook {
		name = "minecraft:knowledge_book"
	}
	components := make(map[string]any)
	if raw, ok := stack.Value("java_components_json"); ok {
		encoded, valid := raw.(string)
		if !valid {
			return nil, fmt.Errorf("Java item components have type %T, want JSON string", raw)
		}
		if err := json.Unmarshal([]byte(encoded), &components); err != nil {
			return nil, fmt.Errorf("decode retained Java item components: %w", err)
		}
	}
	if maximum := stack.MaxDurability(); maximum > 0 {
		damage := maximum - stack.Durability()
		if damage > 0 {
			components["minecraft:damage"] = int32(damage)
		} else {
			delete(components, "minecraft:damage")
		}
	}
	if name := stack.CustomName(); name != "" {
		components["minecraft:custom_name"] = map[string]any{"text": name}
	} else {
		delete(components, "minecraft:custom_name")
	}
	if lore := stack.Lore(); len(lore) != 0 {
		lines := make([]any, len(lore))
		for i, line := range lore {
			lines[i] = map[string]any{"text": line}
		}
		components["minecraft:lore"] = lines
	} else {
		delete(components, "minecraft:lore")
	}
	if book, ok := stack.Item().(KnowledgeBook); ok {
		if book.Recipes == nil {
			delete(components, "minecraft:recipes")
		} else {
			recipes := make([]any, len(book.Recipes))
			for i, recipe := range book.Recipes {
				normalized, valid := normalizeJavaResourceID(recipe)
				if !valid {
					return nil, fmt.Errorf("invalid knowledge book recipe ID %q", recipe)
				}
				recipes[i] = normalized
			}
			components["minecraft:recipes"] = recipes
		}
	}
	if bottle, ok := stack.Item().(OminousBottle); ok {
		if bottle.Amplifier < 0 || bottle.Amplifier > 4 {
			return nil, fmt.Errorf("ominous bottle amplifier %d is outside 0..4", bottle.Amplifier)
		}
		components["minecraft:ominous_bottle_amplifier"] = int32(bottle.Amplifier)
	}
	if isMap {
		if mapItem.HasJavaID {
			if mapItem.JavaID < 0 {
				return nil, fmt.Errorf("Java map id %d is negative", mapItem.JavaID)
			}
			components["minecraft:map_id"] = mapItem.JavaID
		} else if mapItem.HasNativeID || mapItem.NativeID != 0 {
			return nil, fmt.Errorf("native map id %d has no Java map ID mapping", mapItem.NativeID)
		} else {
			delete(components, "minecraft:map_id")
		}
	}
	if cost := stack.AnvilCost(); cost > 0 {
		components["minecraft:repair_cost"] = int32(cost)
	}
	if stack.Unbreakable() {
		components["minecraft:unbreakable"] = map[string]any{}
	}
	enchantments := stack.Enchantments()
	if len(enchantments) != 0 {
		component := "minecraft:enchantments"
		if _, book := stack.Item().(EnchantedBook); book {
			component = "minecraft:stored_enchantments"
		}
		levels, _ := components[component].(map[string]any)
		if legacy, ok := levels["levels"].(map[string]any); ok {
			levels = legacy
		}
		if levels == nil {
			levels = make(map[string]any, len(enchantments))
		}
		// Retain source-only enchantments that the native registry cannot expose.
		for _, enchantment := range enchantments {
			levels["minecraft:"+strings.ToLower(strings.ReplaceAll(enchantment.Type().Name(), " ", "_"))] = int32(enchantment.Level())
		}
		components[component] = levels
	}
	data := map[string]any{"id": name, "count": int32(stack.Count())}
	if len(components) != 0 {
		data["components"] = components
	}
	return data, nil
}
func applyJavaEnchantments(s *Stack, value any) {
	component, _ := value.(map[string]any)
	levels, _ := component["levels"].(map[string]any)
	if levels == nil {
		levels = component
	}
	byName := make(map[string]EnchantmentType)
	for _, enchantment := range Enchantments() {
		name := strings.ToLower(strings.ReplaceAll(enchantment.Name(), " ", "_"))
		byName[name] = enchantment
	}
	var found []Enchantment
	for name, rawLevel := range levels {
		name = strings.TrimPrefix(name, "minecraft:")
		enchantment, ok := byName[name]
		level, valid := javaInt(rawLevel)
		if ok && valid && level > 0 {
			found = append(found, NewEnchantment(enchantment, int(min(level, int64(255)))))
		}
	}
	*s = s.WithForcedEnchantments(found...)
}

func unbreakable(value any) bool {
	if value == true || value == int8(1) || value == int32(1) {
		return true
	}
	if data, ok := value.(map[string]any); ok {
		if flag, exists := data["unbreakable"]; exists {
			return flag == true || flag == int8(1)
		}
		return true
	}
	return false
}

func javaInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}

func javaText(v any) (string, bool) {
	if s, ok := v.(string); ok {
		return s, true
	}
	if m, ok := v.(map[string]any); ok {
		if text, ok := m["text"].(string); ok {
			return text, true
		}
		if translate, ok := m["translate"].(string); ok {
			return translate, true
		}
	}
	return "", false
}

func javaRecipeIDs(value any) ([]string, bool) {
	switch recipes := value.(type) {
	case []any:
		out := make([]string, len(recipes))
		for i, recipe := range recipes {
			id, ok := recipe.(string)
			if !ok {
				return nil, false
			}
			normalized, ok := normalizeJavaResourceID(id)
			if !ok {
				return nil, false
			}
			out[i] = normalized
		}
		return out, true
	case []string:
		out := make([]string, len(recipes))
		for i, recipe := range recipes {
			normalized, ok := normalizeJavaResourceID(recipe)
			if !ok {
				return nil, false
			}
			out[i] = normalized
		}
		return out, true
	default:
		return nil, false
	}
}

func normalizeJavaResourceID(id string) (string, bool) {
	namespace, path, found := strings.Cut(id, ":")
	if !found {
		namespace, path = "minecraft", id
	}
	if namespace == "" || path == "" || strings.Contains(path, ":") {
		return "", false
	}
	for _, r := range namespace {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-') {
			return "", false
		}
	}
	for _, r := range path {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-' || r == '/') {
			return "", false
		}
	}
	return namespace + ":" + path, true
}
