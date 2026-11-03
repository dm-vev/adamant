package bedrockdata

import (
	"maps"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/nbt"
)

// ItemRuntimeID returns the runtime ID Bedrock Edition knows the item i by,
// and its metadata: that of the vanilla item of its name, or, for a custom
// item, the ID given to it past the vanilla ones in the order the custom
// items were registered. ok is false for a name Bedrock Edition does not know.
func ItemRuntimeID(i world.Item) (rid int32, meta int16, ok bool) {
	name, meta := i.EncodeItem()
	rid, ok = items().toRID[name]
	return rid, meta, ok
}

// ItemByRuntimeID returns the item of the runtime ID and metadata passed, as
// world.ItemByName finds it by the name of the ID.
func ItemByRuntimeID(rid int32, meta int16) (world.Item, bool) {
	name, ok := items().toName[rid]
	if !ok {
		return nil, false
	}
	return world.ItemByName(name, meta)
}

// itemTable holds the runtime IDs of the items by name and the names by
// runtime ID: those of VanillaItems and of the first custom items registered.
// A table is not changed once stored.
type itemTable struct {
	custom int
	toRID  map[string]int32
	toName map[int32]string
}

var (
	// itemTables guards the building of item tables and vanillaItemTable.
	itemTables sync.Mutex
	// vanillaItemTable is the table of the vanilla items alone, built once.
	vanillaItemTable *itemTable
	// currentItemTable is the table of the vanilla items and of all custom
	// items registered when it was built.
	currentItemTable atomic.Pointer[itemTable]
)

// items returns the item table, built again if custom items were registered
// since it was last built.
func items() *itemTable {
	if t := currentItemTable.Load(); t != nil && t.custom == world.CustomItemCount() {
		return t
	}
	return rebuildItems()
}

// rebuildItems builds the item table from the vanilla items and the custom
// items registered and stores it.
func rebuildItems() *itemTable {
	itemTables.Lock()
	defer itemTables.Unlock()
	if t := currentItemTable.Load(); t != nil && t.custom == world.CustomItemCount() {
		return t
	}
	if vanillaItemTable == nil {
		vanillaItemTable = readVanillaItems()
	}
	custom := world.CustomItems()
	t := &itemTable{
		custom: len(custom),
		toRID:  maps.Clone(vanillaItemTable.toRID),
		toName: maps.Clone(vanillaItemTable.toName),
	}
	for _, c := range custom {
		// The ID past the names known so far: a custom item of a name known
		// already takes the name over and leaves the count of names as it was,
		// so that the next custom item gets the same ID.
		name, _ := c.EncodeItem()
		rid := int32(len(t.toRID))
		t.toName[rid] = name
		t.toRID[name] = rid
	}
	currentItemTable.Store(t)
	return t
}

// readVanillaItems builds the table of the items of VanillaItems.
func readVanillaItems() *itemTable {
	var m map[string]struct {
		RuntimeID      int32          `nbt:"runtime_id"`
		ComponentBased bool           `nbt:"component_based"`
		Version        int32          `nbt:"version"`
		Data           map[string]any `nbt:"data,omitempty"`
	}
	if err := nbt.Unmarshal(VanillaItems, &m); err != nil {
		panic(err)
	}
	t := &itemTable{toRID: make(map[string]int32, len(m)), toName: make(map[int32]string, len(m))}
	for name, e := range m {
		t.toRID[name] = e.RuntimeID
		t.toName[e.RuntimeID] = name
	}
	return t
}
