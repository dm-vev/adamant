package javastructure

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// JavaBlockEntityPayload converts Java structure block-entity NBT to the
// Bedrock-compatible payload consumed by Dragonfly block entity decoders.
func JavaBlockEntityPayload(state world.BlockState, pos [3]int32, nbt map[string]any) (map[string]any, error) {
	data := cloneNBT(nbt)
	id, ok := data["id"].(string)
	if !ok || id == "" {
		if _, exists := data["id"]; exists {
			return nil, fmt.Errorf("Java block entity has no string id")
		}
		// Rule modifiers carry partial actor NBT. Source creates the actor
		// from the placed state before applying this data, not from an id.
		id = strings.TrimPrefix(state.Name, "minecraft:")
		if id == "suspicious_sand" || id == "suspicious_gravel" {
			id = "brushable_block"
		}
		if _, supported := javaBlockEntityIDs[id]; !supported {
			return nil, fmt.Errorf("Java block state %q has no supported actor type", state.Name)
		}
	}
	id = strings.TrimPrefix(id, "minecraft:")
	bedrockID, supported := javaBlockEntityIDs[id]
	if !supported {
		return nil, fmt.Errorf("unsupported Java block entity %q", id)
	}
	data["id"] = bedrockID
	data["x"], data["y"], data["z"] = pos[0], pos[1], pos[2]
	if id == "chiseled_bookshelf" {
		items, exists := data["Items"]
		if !exists {
			items = []any{}
		}
		converted, err := convertChiseledBookshelfItems(items)
		if err != nil {
			return nil, fmt.Errorf("Java block entity %q items: %w", id, err)
		}
		hasItems := false
		for _, rawItem := range converted {
			entry := rawItem.(map[string]any)
			if name, _ := entry["Name"].(string); name != "" {
				hasItems = true
				break
			}
		}
		if hasItems {
			data["Items"] = converted
		} else {
			delete(data, "Items")
		}
		lastInteracted := int64(-1)
		if raw, exists := data["last_interacted_slot"]; exists {
			value, ok := javaNBTInt(raw)
			if !ok || value < -1 || value >= 6 {
				return nil, fmt.Errorf("Java block entity %q has invalid last_interacted_slot %v", id, raw)
			}
			lastInteracted = value
		}
		delete(data, "last_interacted_slot")
		if lastInteracted >= 0 {
			data["LastInteractedSlot"] = int32(lastInteracted + 1)
		} else {
			delete(data, "LastInteractedSlot")
		}
		data["BlockEntityVersion"] = int32(0)
	} else if items, exists := data["Items"]; exists {
		converted, err := convertJavaItems(items)
		if err != nil {
			return nil, fmt.Errorf("Java block entity %q items: %w", id, err)
		}
		data["Items"] = converted
	}
	switch id {
	case "chest", "trapped_chest":
		// Java stores the pair in block states, Bedrock in block-actor NBT.
		// Recompute it so shape-driven unpairing cannot retain stale links.
		delete(data, "pairx")
		delete(data, "pairz")
		chestType, _ := state.Properties["type"].(string)
		if chestType == "left" || chestType == "right" {
			dx, dz := int32(0), int32(0)
			facing, _ := state.Properties["facing"].(string)
			switch facing {
			case "", "north":
				dx = 1
			case "south":
				dx = -1
			case "east":
				dz = 1
			case "west":
				dz = -1
			default:
				return nil, fmt.Errorf("Java chest has invalid facing %q", facing)
			}
			if chestType == "right" {
				dx, dz = -dx, -dz
			}
			data["pairx"], data["pairz"] = pos[0]+dx, pos[2]+dz
		}
	case "bell":
		// Java's BellBlockEntity has no saved ringing state; native Bell actors
		// still require their reset transient fields and unset direction marker.
		data = (block.Bell{}).EncodeNBT()
		data["x"], data["y"], data["z"] = pos[0], pos[1], pos[2]
	case "lectern":
		actor := block.Lectern{}
		if raw, exists := data["Book"]; exists {
			book, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("Java lectern Book has type %T, want compound", raw)
			}
			stack, err := item.StackFromJavaNBT(book)
			if err != nil {
				return nil, fmt.Errorf("Java lectern Book: %w", err)
			}
			actor.Book = stack
			if pages, ok := stack.Item().(interface{ TotalPages() int }); ok {
				page := int64(0)
				if raw, exists := data["Page"]; exists {
					var valid bool
					page, valid = javaNBTInt(raw)
					if !valid {
						return nil, fmt.Errorf("Java lectern Page has type %T, want integer", raw)
					}
				}
				actor.Page = int(min(max(page, 0), int64(pages.TotalPages()-1)))
			}
		}
		data = actor.EncodeNBT()
		data["x"], data["y"], data["z"] = pos[0], pos[1], pos[2]
	case "creaking_heart":
		if link, exists := data["creaking"]; exists {
			data["adamant:java"] = map[string]any{"creaking": link}
		}
		actor := (block.CreakingHeart{}).DecodeNBT(data).(block.CreakingHeart)
		data = actor.EncodeNBT()
		data["x"], data["y"], data["z"] = pos[0], pos[1], pos[2]
	case "sculk_shrieker":
		actor := (block.SculkShrieker{}).DecodeNBT(data).(block.SculkShrieker)
		data = actor.EncodeNBT()
		data["x"], data["y"], data["z"] = pos[0], pos[1], pos[2]
	case "sculk_sensor", "calibrated_sculk_sensor":
		actor, adapted, err := NativeBlockForJavaState(state)
		if err != nil {
			return nil, err
		}
		if !adapted {
			return nil, fmt.Errorf("Java sensor actor has incompatible state %q", state.Name)
		}
		source := data
		data = actor.(world.NBTer).EncodeNBT()
		java := data["adamant:java"].(map[string]any)
		for _, key := range [...]string{"last_vibration_frequency", "listener"} {
			if value, exists := source[key]; exists {
				java[key] = value
			}
		}
		data["x"], data["y"], data["z"] = pos[0], pos[1], pos[2]
	case "mob_spawner":
		convertSpawner(data)
	case "sign", "hanging_sign":
		convertSign(data)
	case "banner":
		convertBanner(data, state.Name)
	case "bee_nest", "beehive":
		if err := convertBeehive(data); err != nil {
			return nil, fmt.Errorf("Java block entity %q: %w", id, err)
		}
	case "brushable_block":
		if err := convertBrushable(data); err != nil {
			return nil, fmt.Errorf("Java block entity %q: %w", id, err)
		}
	case "decorated_pot":
		if err := convertDecoratedPot(data); err != nil {
			return nil, fmt.Errorf("Java block entity %q: %w", id, err)
		}
	}
	return data, nil
}

var javaBlockEntityIDs = map[string]string{
	"chest": "Chest", "trapped_chest": "Chest", "barrel": "Barrel",
	"furnace": "Furnace", "smoker": "Smoker", "blast_furnace": "BlastFurnace",
	"brewing_stand": "BrewingStand", "hopper": "Hopper", "dispenser": "Dispenser", "dropper": "Dropper",
	"chiseled_bookshelf": "ChiseledBookshelf",
	"shulker_box":        "ShulkerBox", "sign": "Sign", "hanging_sign": "Sign", "mob_spawner": "MobSpawner",
	"banner": "Banner", "bee_nest": "Beehive", "beehive": "Beehive", "brushable_block": "BrushableBlock",
	"decorated_pot": "DecoratedPot", "campfire": "Campfire", "end_gateway": "EndGateway", "skull": "Skull",
	"trial_spawner": "TrialSpawner", "vault": "Vault",
	"bed": "Bed", "copper_golem_statue": "CopperGolemStatue",
	"creaking_heart": "CreakingHeart", "comparator": "Comparator", "bell": "Bell",
	"sculk_shrieker": "SculkShrieker",
	"sculk_sensor":   "SculkSensor", "calibrated_sculk_sensor": "CalibratedSculkSensor",
	"lectern": "Lectern",
}

func convertChiseledBookshelfItems(raw any) ([]any, error) {
	count, ok := templateListLen(raw)
	if !ok {
		return nil, fmt.Errorf("Items has type %T, want list", raw)
	}
	out := make([]any, 6)
	for slot := range out {
		out[slot] = map[string]any{
			"Name": "", "Count": byte(0), "Damage": int16(0), "WasPickedUp": byte(0),
		}
	}
	for index := range count {
		rawItem, _ := templateListValue(raw, index)
		entry, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d has type %T, want compound", index, rawItem)
		}
		slot := int64(0)
		if rawSlot, exists := entry["Slot"]; exists {
			var valid bool
			slot, valid = javaNBTInt(rawSlot)
			if !valid {
				return nil, fmt.Errorf("item %d Slot has type %T, want integer", index, rawSlot)
			}
		}
		slot = int64(uint8(slot))
		if slot >= int64(len(out)) {
			continue
		}
		stack, err := item.StackFromJavaNBT(entry)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", index, err)
		}
		if stack.Empty() {
			continue
		}
		encoded := item.WriteNBT(stack, true)
		encoded["WasPickedUp"] = byte(0)
		out[slot] = encoded
	}
	return out, nil
}

func convertJavaItems(raw any) ([]any, error) {
	count, ok := templateListLen(raw)
	if !ok {
		return nil, fmt.Errorf("Items has type %T, want list", raw)
	}
	out := make([]any, 0, count)
	for i := range count {
		value, _ := templateListValue(raw, i)
		entry, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d has type %T, want compound", i, value)
		}
		stack, err := item.StackFromJavaNBT(entry)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		if stack.Empty() {
			continue
		}
		encoded := item.WriteNBT(stack, true)
		slot := int64(0)
		if rawSlot, exists := entry["Slot"]; exists {
			var valid bool
			slot, valid = javaNBTInt(rawSlot)
			if !valid {
				return nil, fmt.Errorf("item %d Slot has type %T, want integer", i, rawSlot)
			}
		}
		encoded["Slot"] = uint8(slot)
		out = append(out, encoded)
	}
	return out, nil
}

func convertSpawner(data map[string]any) {
	if spawn, ok := data["SpawnData"].(map[string]any); ok {
		entity, _ := spawn["entity"].(map[string]any)
		if entity == nil {
			entity = spawn
		}
		if id, ok := entity["id"].(string); ok {
			data["EntityIdentifier"] = id
		}
	}
	if count, ok := templateListLen(data["SpawnPotentials"]); ok {
		for i := range count {
			raw, _ := templateListValue(data["SpawnPotentials"], i)
			potential, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			weighted, _ := potential["data"].(map[string]any)
			entity, _ := weighted["entity"].(map[string]any)
			if entity == nil {
				entity = weighted
			}
			id, _ := entity["id"].(string)
			if id != "" {
				potential["EntityIdentifier"] = id
			}
			if weight, exists := potential["weight"]; exists {
				potential["Weight"] = weight
			}
		}
	}
}

func convertSign(data map[string]any) {
	front, _ := data["front_text"].(map[string]any)
	if front == nil {
		front, _ = data["FrontText"].(map[string]any)
	}
	back, _ := data["back_text"].(map[string]any)
	if back == nil {
		back, _ = data["BackText"].(map[string]any)
	}
	if front != nil {
		data["FrontText"] = bedrockSignText(front)
	}
	if back != nil {
		data["BackText"] = bedrockSignText(back)
	}
	if waxed, ok := data["is_waxed"]; ok {
		data["IsWaxed"] = waxed
	}
}

func bedrockSignText(src map[string]any) map[string]any {
	out := cloneNBT(src)
	if count, ok := templateListLen(src["messages"]); ok {
		lines := make([]string, 0, count)
		for i := range count {
			raw, _ := templateListValue(src["messages"], i)
			if text, ok := raw.(string); ok {
				lines = append(lines, text)
			}
		}
		out["Text"] = strings.Join(lines, "\n")
	}
	if colour, ok := src["color"].(string); ok {
		if rgba, valid := javaSignColour(colour); valid {
			out["SignTextColor"] = rgba
		}
	}
	if glowing, ok := src["has_glowing_text"]; ok {
		out["IgnoreLighting"] = glowing
	}
	if owner, ok := src["allowed_player_editor"]; ok {
		out["TextOwner"] = owner
	}
	return out
}

func convertBanner(data map[string]any, stateName string) {
	if count, ok := templateListLen(data["patterns"]); ok {
		converted := make([]any, 0, count)
		for i := range count {
			raw, _ := templateListValue(data["patterns"], i)
			p, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			layer := map[string]any{}
			if pattern, ok := p["pattern"].(string); ok {
				layer["Pattern"] = javaBannerPattern(pattern)
			}
			if colour, ok := p["color"].(string); ok {
				layer["Color"] = int32(^javaDyeColor(colour) & 0xf)
			}
			converted = append(converted, layer)
		}
		data["Patterns"] = converted
	}
	if base, ok := data["base"].(string); ok {
		data["Base"] = int32(^javaDyeColor(base) & 0xf)
	} else if _, exists := data["Base"]; !exists {
		stateName = strings.TrimPrefix(stateName, "minecraft:")
		stateName = strings.TrimSuffix(stateName, "_wall_banner")
		stateName = strings.TrimSuffix(stateName, "_banner")
		if _, ok := javaColorIDs[stateName]; ok {
			data["Base"] = int32(^javaDyeColor(stateName) & 0xf)
		}
	}
}

var javaColorIDs = map[string]int32{
	"white": 0, "orange": 1, "magenta": 2, "light_blue": 3, "yellow": 4,
	"lime": 5, "pink": 6, "gray": 7, "light_gray": 8, "cyan": 9, "purple": 10,
	"blue": 11, "brown": 12, "green": 13, "red": 14, "black": 15,
}

func javaDyeColor(name string) int32 { return javaColorIDs[name] }

func javaBannerPattern(name string) string {
	name = strings.TrimPrefix(name, "minecraft:")
	patterns := map[string]string{
		"border": "bo", "bricks": "bri", "circle": "mc", "creeper": "cre", "cross": "cr",
		"curly_border": "cbo", "diagonal_left": "lud", "diagonal_right": "rd", "diagonal_up_left": "ld",
		"diagonal_up_right": "rud", "flower": "flo", "gradient": "gra", "gradient_up": "gru",
		"half_horizontal": "hh", "half_horizontal_bottom": "hhb", "half_vertical": "vh", "half_vertical_right": "vhr",
		"mojang": "moj", "rhombus": "mr", "skull": "sku", "small_stripes": "ss",
		"square_bottom_left": "bl", "square_bottom_right": "br", "square_top_left": "tl", "square_top_right": "tr",
		"straight_cross": "sc", "stripe_bottom": "bs", "stripe_center": "cs", "stripe_downleft": "dls",
		"stripe_downright": "drs", "stripe_left": "ls", "stripe_middle": "ms", "stripe_right": "rs", "stripe_top": "ts",
		"triangle_bottom": "bt", "triangle_top": "tt", "triangles_bottom": "bts", "triangles_top": "tts",
		"globe": "glb", "piglin": "pig", "flow": "flw", "guster": "gus",
	}
	if pattern, ok := patterns[name]; ok {
		return pattern
	}
	return name
}

func javaSignColour(name string) (int32, bool) {
	colours := map[string]int32{
		"black": -16777216, "dark_blue": -16777046, "dark_green": -16733696, "dark_aqua": -16733526,
		"dark_red": -5636096, "dark_purple": -5635926, "gold": -22016, "gray": -5592406,
		"dark_gray": -11184811, "blue": -11141291, "green": -11141121, "aqua": -11141051,
		"red": -43691, "light_purple": -43656, "yellow": -171, "white": -1,
	}
	value, ok := colours[name]
	return value, ok
}
func convertBeehive(data map[string]any) error {
	if honey, exists := data["honey_level"]; exists {
		level, ok := javaNBTInt(honey)
		if !ok || level < 0 || level > 5 {
			return fmt.Errorf("invalid Java beehive honey_level %v", honey)
		}
		data["HoneyLevel"] = int32(level)
		delete(data, "honey_level")
	}
	if raw, exists := data["flower_pos"]; exists {
		position, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("Java beehive flower_pos has type %T, want block position", raw)
		}
		converted := make(map[string]any, 3)
		for javaKey, nativeKey := range map[string]string{"x": "X", "y": "Y", "z": "Z"} {
			value, valid := javaNBTInt(position[javaKey])
			if !valid {
				return fmt.Errorf("Java beehive flower_pos.%s has type %T, want integer", javaKey, position[javaKey])
			}
			converted[nativeKey] = int32(value)
		}
		data["FlowerPos"] = converted
		delete(data, "flower_pos")
	}
	bees := data["Bees"]
	count, ok := templateListLen(bees)
	if !ok {
		bees = data["bees"]
		count, ok = templateListLen(bees)
	}
	if !ok {
		return nil
	}
	occupants := make([]any, 0, count)
	for i := range count {
		raw, _ := templateListValue(bees, i)
		bee, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("bee %d has type %T, want compound", i, raw)
		}
		entityData, _ := bee["EntityData"].(map[string]any)
		if entityData == nil {
			entityData, _ = bee["entity_data"].(map[string]any)
		}
		entityID, _ := entityData["id"].(string)
		if entityID == "" {
			return fmt.Errorf("bee %d has no entity id", i)
		}
		ticks, _ := javaNBTInt(bee["TicksInHive"])
		minimum, _ := javaNBTInt(bee["MinOccupationTicks"])
		if minimum == 0 {
			minimum = 600
		}
		occupants = append(occupants, map[string]any{
			"ActorIdentifier": entityID,
			"SaveData":        entityData,
			"TicksLeftToStay": int32(max(int64(0), minimum-ticks)),
		})
	}
	data["Occupants"] = occupants
	return nil
}

func convertBrushable(data map[string]any) error {
	if loot, ok := data["loot_table"].(string); ok {
		data["LootTable"] = loot
	}
	if seed, ok := data["loot_table_seed"]; ok {
		data["LootTableSeed"] = seed
	}
	if itemData, ok := data["item"].(map[string]any); ok {
		stack, err := item.StackFromJavaNBT(itemData)
		if err != nil {
			return fmt.Errorf("item: %w", err)
		}
		data["Item"] = item.WriteNBT(stack, true)
	}
	return nil
}

func convertDecoratedPot(data map[string]any) error {
	if itemData, ok := data["item"].(map[string]any); ok {
		stack, err := item.StackFromJavaNBT(itemData)
		if err != nil {
			return fmt.Errorf("item: %w", err)
		}
		data["item"] = item.WriteNBT(stack, true)
	}
	return nil
}

func cloneNBT(data map[string]any) map[string]any {
	out := make(map[string]any, len(data)+3)
	for key, value := range data {
		out[key] = cloneNBTValue(value)
	}
	return out
}

func cloneNBTValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneNBT(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneNBTValue(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(v))
		for i, compound := range v {
			out[i] = cloneNBT(compound)
		}
		return out
	case []byte:
		return append([]byte(nil), v...)
	case []int8:
		return append([]int8(nil), v...)
	case []int32:
		return append([]int32(nil), v...)
	case []int64:
		return append([]int64(nil), v...)
	case []string:
		return append([]string(nil), v...)
	default:
		return value
	}
}
