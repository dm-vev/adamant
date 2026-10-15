package loot

import (
	"fmt"
	"strconv"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// instrumentTags holds the instrument tags of InstrumentTagsProvider, in
// their declared order.
var instrumentTags = map[string][]sound.Horn{
	"minecraft:regular_goat_horns":   {sound.Ponder(), sound.Sing(), sound.Seek(), sound.Feel()},
	"minecraft:screaming_goat_horns": {sound.Admire(), sound.Call(), sound.Yearn(), sound.Dream()},
	"minecraft:goat_horns":           sound.GoatHorns(),
}

// lootInstrument ports SetInstrumentFunction: it picks one instrument of
// options, a tag, an ID or a list of IDs, with the context's random.
func lootInstrument(options any, c *Context) (string, error) {
	var horns []sound.Horn
	switch v := options.(type) {
	case string:
		if len(v) > 0 && v[0] == '#' {
			tagged, ok := instrumentTags[normalize(v[1:])]
			if !ok {
				return "", fmt.Errorf("set_instrument references unknown instrument tag %q", v)
			}
			horns = tagged
		} else if horn, ok := item.GoatHornFromJavaInstrument(normalize(v)); ok {
			horns = []sound.Horn{horn}
		} else {
			return "", fmt.Errorf("set_instrument references unknown instrument %q", v)
		}
	case []any:
		for _, raw := range v {
			id, _ := raw.(string)
			horn, ok := item.GoatHornFromJavaInstrument(normalize(id))
			if !ok {
				return "", fmt.Errorf("set_instrument references unknown instrument %v", raw)
			}
			horns = append(horns, horn)
		}
	default:
		return "", fmt.Errorf("set_instrument has invalid options %T", options)
	}
	if len(horns) == 0 {
		return "", nil
	}
	return item.JavaInstrumentID(horns[c.Random.NextInt(int32(len(horns)))]), nil
}

// lootCopyState ports CopyBlockState: the listed properties of the broken
// block's state, if it has them, go into the item's block_state component.
func lootCopyState(raw map[string]any, components map[string]any, c *Context) error {
	if c.BlockState == nil {
		return nil
	}
	state, err := world.JavaStateOf(c.BlockState)
	if err != nil {
		return err
	}
	properties, _ := raw["properties"].([]any)
	itemState, _ := components["minecraft:block_state"].(map[string]any)
	for _, p := range properties {
		name, _ := p.(string)
		value, ok := state.Properties[name]
		if !ok {
			continue
		}
		if itemState == nil {
			itemState = make(map[string]any, len(properties))
		}
		switch v := value.(type) {
		case string:
			itemState[name] = v
		case bool:
			itemState[name] = strconv.FormatBool(v)
		case int32:
			itemState[name] = strconv.Itoa(int(v))
		default:
			return fmt.Errorf("copy_state: property %s has type %T", name, value)
		}
	}
	if itemState != nil {
		components["minecraft:block_state"] = itemState
	}
	return nil
}

// lootCopyComponents ports CopyComponentsFunction for its block_entity
// source: without a broken block entity nothing is copied. Of the components
// a block entity collects, only its custom name has a native counterpart;
// asking to copy any other from a block entity is an error rather than a
// silent loss.
func lootCopyComponents(raw map[string]any, components map[string]any, c *Context) error {
	if source, _ := raw["source"].(string); source != "block_entity" {
		return fmt.Errorf("copy_components has unsupported source %v", raw["source"])
	}
	actor, ok := c.BlockState.(world.NBTer)
	if !ok {
		return nil
	}
	data := actor.EncodeNBT()
	include, _ := raw["include"].([]any)
	for _, raw := range include {
		switch id, _ := raw.(string); id {
		case "minecraft:custom_name":
			if name, _ := data["CustomName"].(string); name != "" {
				components[id] = map[string]any{"text": name}
			}
		default:
			return fmt.Errorf("copy_components cannot copy %v from a block entity", raw)
		}
	}
	return nil
}
