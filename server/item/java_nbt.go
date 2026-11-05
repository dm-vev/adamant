package item

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// ErrJavaStackCount is the error StackFromStructureNBT wraps for a stack
// whose count ItemStack.CODEC refuses, a negative one or one above 99. A
// count above the max stack size of the item is not refused, as Java keeps
// such a stack. Java drops a refused stack from a list of stacks it loads,
// such as the items of a container, and loads the rest.
var ErrJavaStackCount = errors.New("count outside 1..99")

// StackFromStructureNBT decodes a Java 1.20.5+ item stack compound into a Dragonfly
// stack. Unknown item identifiers are rejected rather than silently discarded.
// A count of 0 gives an empty stack. The tag of a stack from before 1.20.5 is
// turned into components as Java's data fixer does (javaLegacyComponents).
func StackFromStructureNBT(data map[string]any) (Stack, error) {
	id, ok := data["id"].(string)
	if !ok || id == "" {
		return Stack{}, fmt.Errorf("Java item stack has no string id")
	}
	id = javaComponentKey(id)
	var it world.Item
	if variant, mapID := FilledMapVariantFromJavaItemID(id); mapID {
		it = FilledMap{Variant: variant}
	} else if id == "minecraft:knowledge_book" {
		it = KnowledgeBook{}
	} else {
		var ok bool
		if it, ok = javaItem(id); !ok {
			return Stack{}, fmt.Errorf("unsupported Java item %q", id)
		}
	}
	count, err := javaCount(data)
	if err != nil {
		return Stack{}, fmt.Errorf("item %q: %w", id, err)
	}
	s := NewStack(it, count)
	components := make(map[string]any)
	if source, ok := data["components"].(map[string]any); ok {
		for k, v := range source {
			components[javaComponentKey(k)] = javaComponentValue(javaComponentKey(k), v)
		}
	}
	if legacy, ok := data["tag"].(map[string]any); ok {
		for k, v := range javaLegacyComponents(id, legacy) {
			if _, exists := components[k]; !exists {
				components[k] = v
			}
		}
	}
	for key, value := range components {
		switch key {
		case "minecraft:damage":
			if s, err = withJavaDamage(s, value); err != nil {
				return Stack{}, fmt.Errorf("item %q: %w", id, err)
			}
		case "minecraft:custom_name", "minecraft:item_name":
			if name, ok := javaText(value); ok {
				s = s.WithCustomName(name)
			}
		case "minecraft:lore":
			s = withJavaLore(s, value)
		case "minecraft:repair_cost":
			if cost, ok := javaInt(value); ok {
				s = s.WithAnvilCost(int(cost))
			}
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
		case "minecraft:unbreakable":
			if unbreakable(value) {
				s = s.AsUnbreakable()
			}
		case "minecraft:enchantments", "minecraft:stored_enchantments":
			applyJavaEnchantments(&s, value)
		case "minecraft:potion_contents":
			s = javaPotionContents(s, value)
		case "minecraft:instrument":
			if horn, ok := s.Item().(GoatHorn); ok {
				id, _ := value.(string)
				if horn.Type, ok = GoatHornFromJavaInstrument(id); !ok {
					return Stack{}, fmt.Errorf("unknown Java goat horn instrument %v", value)
				}
				s = s.WithItem(horn)
			}
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

// StackToStructureNBT encodes a Dragonfly stack as a Java ItemStack.CODEC compound.
// Raw Java components retained by StackFromStructureNBT are preserved where possible.
func StackToStructureNBT(stack Stack) (map[string]any, error) {
	if stack.Empty() {
		return nil, nil
	}
	mapItem, isMap := stack.Item().(FilledMap)
	if isMap && !mapItem.Variant.Valid() {
		return nil, fmt.Errorf("invalid filled map variant %d", mapItem.Variant)
	}
	name, _ := stack.Item().EncodeItem()
	if name == "minecraft:lit_pumpkin" {
		name = "minecraft:jack_o_lantern"
	}
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
		// item_name was read as the name too, so a cleared name clears both.
		delete(components, "minecraft:custom_name")
		delete(components, "minecraft:item_name")
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
	if horn, ok := stack.Item().(GoatHorn); ok {
		components["minecraft:instrument"] = JavaInstrumentID(horn.Type)
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
	writeJavaStackState(components, stack)
	writeJavaPotionContents(components, stack)
	data := map[string]any{"id": name, "count": int32(stack.Count())}
	if len(components) != 0 {
		data["components"] = components
	}
	return data, nil
}

// writeJavaStackState writes the repair cost, unbreakable and enchantments of
// a stack into components. The components a stack was imported with are only a
// base: what the server models is written from the stack as it is now, so
// that what an anvil or a grindstone took off is not written back.
func writeJavaStackState(components map[string]any, stack Stack) {
	if cost := stack.AnvilCost(); cost > 0 {
		components["minecraft:repair_cost"] = int32(cost)
	} else {
		delete(components, "minecraft:repair_cost")
	}
	if stack.Unbreakable() {
		components["minecraft:unbreakable"] = map[string]any{}
	} else {
		delete(components, "minecraft:unbreakable")
	}
	// Both enchantment components were read as the stack's enchantments, so
	// only what the stack did not take from them is kept: enchantments the
	// server has no type of and levels it cannot hold.
	for _, key := range []string{"minecraft:enchantments", "minecraft:stored_enchantments"} {
		levels, _ := components[key].(map[string]any)
		if legacy, ok := levels["levels"].(map[string]any); ok {
			levels = legacy
		}
		for id, raw := range levels {
			if _, valid := javaEnchantmentLevel(raw); !valid {
				continue
			}
			if _, known := enchantmentByJavaID(id); known {
				delete(levels, id)
			}
		}
		if len(levels) == 0 {
			delete(components, key)
		} else {
			components[key] = levels
		}
	}
	enchantments := stack.Enchantments()
	if len(enchantments) == 0 {
		return
	}
	component := "minecraft:enchantments"
	if _, book := stack.Item().(EnchantedBook); book {
		component = "minecraft:stored_enchantments"
	}
	levels, _ := components[component].(map[string]any)
	if levels == nil {
		levels = make(map[string]any, len(enchantments))
	}
	for _, enchantment := range enchantments {
		levels[JavaEnchantmentID(enchantment.Type())] = int32(enchantment.Level())
	}
	components[component] = levels
}

// javaPotionOf returns the potion of a potion, a splash or lingering potion or
// a tipped arrow, and whether the item is one.
func javaPotionOf(it world.Item) (potion.Potion, bool) {
	switch it := it.(type) {
	case Potion:
		return it.Type, true
	case SplashPotion:
		return it.Type, true
	case LingeringPotion:
		return it.Type, true
	case Arrow:
		return it.Tip, true
	}
	return potion.Potion{}, false
}

// writeJavaPotionContents writes the potion_contents component of a stack
// from its potion as it is now, as PotionContents does: the potion
// identifier, beside the custom_color, custom_effects and custom_name the
// stack was imported with, which the server does not model. A potion brewed
// or given in game has no retained component and so gets the type alone.
// Left as they came are a potion the server has no type of, a plain arrow,
// whose potion Water means none, and a potion of custom effects only.
func writeJavaPotionContents(components map[string]any, stack Stack) {
	p, ok := javaPotionOf(stack.Item())
	if !ok {
		return
	}
	contents, _ := components["minecraft:potion_contents"].(map[string]any)
	if id, isString := components["minecraft:potion_contents"].(string); isString {
		contents = map[string]any{"potion": id}
	}
	id, hasID := contents["potion"].(string)
	if hasID {
		if _, known := potion.FromJavaID(id); !known {
			return
		}
	}
	_, arrow := stack.Item().(Arrow)
	if p == potion.Water() && (arrow || !hasID && len(contents) != 0) {
		return
	}
	out := make(map[string]any, len(contents)+1)
	maps.Copy(out, contents)
	out["potion"] = p.JavaID()
	components["minecraft:potion_contents"] = out
}

// javaComponentKey returns the ID of a component as Java parses it, an
// Identifier in the minecraft namespace unless it names another: damage
// is minecraft:damage. Kept as it came, the short form would be written
// back beside the full one, and Java refuses the duplicate.
func javaComponentKey(k string) string {
	if strings.Contains(k, ":") {
		return k
	}
	return "minecraft:" + k
}

// javaComponentValue returns the value of the component k with the
// enchantment IDs of an enchantments or stored_enchantments component in
// full, as ItemEnchantments.CODEC parses them: unbreaking is
// minecraft:unbreaking. Of an ID given in both forms the full one is kept.
func javaComponentValue(k string, v any) any {
	if k != "minecraft:enchantments" && k != "minecraft:stored_enchantments" {
		return v
	}
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	if levels, ok := m["levels"].(map[string]any); ok {
		out := maps.Clone(m)
		out["levels"] = javaComponentValue(k, levels)
		return out
	}
	out := make(map[string]any, len(m))
	for id, level := range m {
		if full := javaComponentKey(id); full == id || out[full] == nil {
			out[full] = level
		}
	}
	return out
}

// javaCount returns the count of a Java stack: count, or Count before
// 1.20.5, 1 if neither is set. ItemStack.CODEC reads it as an
// intRange(1, 99).
func javaCount(data map[string]any) (int, error) {
	count := int64(1)
	if n, ok := javaInt(data["count"]); ok {
		count = n
	}
	if n, ok := javaInt(data["Count"]); ok {
		count = n
	}
	if count < 0 || count > 99 {
		return 0, fmt.Errorf("count %d: %w", count, ErrJavaStackCount)
	}
	return int(count), nil
}

// withJavaLore applies a lore component: a list of lines.
func withJavaLore(s Stack, value any) Stack {
	lines, ok := value.([]any)
	if !ok {
		return s
	}
	lore := make([]string, 0, len(lines))
	for _, line := range lines {
		if text, ok := javaText(line); ok {
			lore = append(lore, text)
		}
	}
	return s.WithLore(lore...)
}

// withJavaDamage applies a damage component to a durable stack.
// ItemStack.CODEC reads minecraft:damage as a NON_NEGATIVE_INT
// (DataComponents.DAMAGE), on any item: it refuses a negative damage or one
// that is no number, which withJavaDamage returns an error for. A float is
// read as its int (Number.intValue). ItemStack.getDamageValue clamps the
// damage to the max_damage, and the codec keeps a stack at or past it. A
// durability of 0 breaks a stack here, so such a stack keeps one point,
// which its next use takes.
func withJavaDamage(s Stack, value any) (Stack, error) {
	d, ok := javaInt(value)
	switch f := value.(type) {
	case float32:
		d, ok = int64(f), true
	case float64:
		d, ok = int64(f), true
	}
	if !ok || d < 0 {
		return Stack{}, fmt.Errorf("damage %v is not a non-negative integer", value)
	}
	if maxDamage := s.MaxDurability(); maxDamage > 0 {
		return s.WithDurability(maxDamage - int(min(d, int64(maxDamage-1)))), nil
	}
	return s, nil
}

// applyJavaEnchantments applies an enchantments component: a map of IDs to
// levels, optionally under "levels", or the list of {id, lvl} compounds of
// the tag of stacks before 1.20.5.
func applyJavaEnchantments(s *Stack, value any) {
	component, _ := value.(map[string]any)
	levels, _ := component["levels"].(map[string]any)
	if levels == nil {
		levels = component
	}
	if list, ok := value.([]any); ok {
		levels = make(map[string]any, len(list))
		for _, entry := range list {
			e, _ := entry.(map[string]any)
			if id, ok := e["id"].(string); ok {
				levels[id] = e["lvl"]
			}
		}
	}
	byName := make(map[string]EnchantmentType)
	for _, enchantment := range Enchantments() {
		byName[strings.TrimPrefix(JavaEnchantmentID(enchantment), "minecraft:")] = enchantment
	}
	var found []Enchantment
	for name, rawLevel := range levels {
		name = strings.TrimPrefix(name, "minecraft:")
		enchantment, ok := byName[name]
		level, valid := javaEnchantmentLevel(rawLevel)
		if ok && valid {
			found = append(found, NewEnchantment(enchantment, int(min(level, int64(255)))))
		}
	}
	*s = s.WithForcedEnchantments(found...)
}

// javaEnchantmentLevel returns the level of an enchantment as a component
// holds it, if it is one the stack takes: a positive number. Components that
// went through JSON hold their numbers as floats, which NbtOps reads as numbers
// all the same (NumericTag.intValue).
func javaEnchantmentLevel(raw any) (int64, bool) {
	level, ok := javaInt(raw)
	if f, isFloat := raw.(float64); isFloat {
		level, ok = int64(f), true
	}
	return level, ok && level > 0
}

// JavaEnchantmentID returns the Java registry ID of an enchantment: its name
// in snake case, except for the curses, which Java names after their effect.
func JavaEnchantmentID(e EnchantmentType) string {
	switch name := e.Name(); name {
	case "Curse of Binding":
		return "minecraft:binding_curse"
	case "Curse of Vanishing":
		return "minecraft:vanishing_curse"
	default:
		return "minecraft:" + strings.ToLower(strings.ReplaceAll(name, " ", "_"))
	}
}

// JavaInstrumentID returns the Java instrument a goat horn plays, such as
// minecraft:ponder_goat_horn.
func JavaInstrumentID(h sound.Horn) string {
	return "minecraft:" + strings.ToLower(h.Name()) + "_goat_horn"
}

// GoatHornFromJavaInstrument returns the goat horn playing a Java
// instrument.
func GoatHornFromJavaInstrument(id string) (sound.Horn, bool) {
	for _, h := range sound.GoatHorns() {
		if JavaInstrumentID(h) == id || strings.TrimPrefix(JavaInstrumentID(h), "minecraft:") == id {
			return h, true
		}
	}
	return sound.Horn{}, false
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
