package entity

import (
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
)

// itemNamed reports whether the stack holds an item with one of the names.
func itemNamed(stack item.Stack, names ...string) bool {
	if stack.Empty() {
		return false
	}
	name, _ := stack.Item().EncodeItem()
	for _, n := range names {
		if name == n {
			return true
		}
	}
	return false
}

// blockInTag reports whether the block is in the vanilla block tag.
func blockInTag(b world.Block, tag string) bool {
	state, err := world.CanonicalStateOf(b)
	if err != nil {
		return false
	}
	in, _ := gamedata.BlockTagContains(tag, state.Name)
	return in
}

// inItemTag reports if the item of the stack is one of the Java item tag
// passed.
func inItemTag(stack item.Stack, tag string) bool { return loot.StackInItemTag(stack, tag) }

// entityTypeInTag reports if the type of an entity is in a vanilla entity
// type tag.
func entityTypeInTag(e world.Entity, tag string) bool {
	id := e.H().Type().EncodeEntity()
	if java, ok := bedrockOnlyEntityIDs[id]; ok {
		id = java
	}
	in, _ := gamedata.EntityTypeTagContains(tag, id)
	return in
}

// bedrockOnlyEntityIDs are the Java identifiers of the entities Bedrock
// names otherwise.
var bedrockOnlyEntityIDs = map[string]string{
	"minecraft:villager_v2":        "minecraft:villager",
	"minecraft:zombie_villager_v2": "minecraft:zombie_villager",
	"minecraft:zombie_pigman":      "minecraft:zombified_piglin",
	"minecraft:evocation_illager":  "minecraft:evoker",
}
