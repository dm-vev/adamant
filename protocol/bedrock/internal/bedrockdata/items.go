package bedrockdata

import (
	"maps"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/world"
)

// ItemRuntimeID returns the runtime ID clients of protocol 2193 know the
// item i by, as Vanilla.ItemRuntimeID does.
func ItemRuntimeID(i world.Item) (rid int32, meta int16, ok bool) {
	return v2193.ItemRuntimeID(i)
}

// ItemByRuntimeID returns the item clients of protocol 2193 name by a
// runtime ID and metadata, as Vanilla.ItemByRuntimeID does.
func ItemByRuntimeID(rid int32, meta int16) (world.Item, bool) {
	return v2193.ItemByRuntimeID(rid, meta)
}

// ItemRuntimeID returns the runtime ID the clients of the protocol know the
// item i by, and its metadata: that of the vanilla item of its name, or, for
// a custom item, the ID given to it past the vanilla ones in the order the
// custom items were registered. ok is false for a name the protocol does
// not know.
func (v *Vanilla) ItemRuntimeID(i world.Item) (rid int32, meta int16, ok bool) {
	name, meta := i.EncodeItem()
	rid, ok = v.ids.table().toRID[name]
	return rid, meta, ok
}

// ItemByRuntimeID returns the item of the runtime ID and metadata passed, as
// world.ItemByName finds it by the name of the ID.
func (v *Vanilla) ItemByRuntimeID(rid int32, meta int16) (world.Item, bool) {
	name, ok := v.ids.table().toName[rid]
	if !ok {
		return nil, false
	}
	return world.ItemByName(name, meta)
}

// itemIDs holds the runtime IDs of the items of a protocol.
type itemIDs struct {
	v *Vanilla
	// mu guards the building of tables and vanilla.
	mu sync.Mutex
	// vanilla is the table of the vanilla items alone, built once.
	vanilla *itemTable
	// current is the table of the vanilla items and of all custom items
	// registered when it was built.
	current atomic.Pointer[itemTable]
}

// itemTable holds the runtime IDs of the items by name and the names by
// runtime ID: those of the vanilla items and of the first custom items
// registered. A table is not changed once stored.
type itemTable struct {
	custom int
	toRID  map[string]int32
	toName map[int32]string
}

// table returns the item table, built again if custom items were
// registered since it was last built.
func (ids *itemIDs) table() *itemTable {
	if t := ids.current.Load(); t != nil && t.custom == world.CustomItemCount() {
		return t
	}
	return ids.rebuild()
}

// rebuild builds the item table from the vanilla items and the custom
// items registered and stores it.
func (ids *itemIDs) rebuild() *itemTable {
	ids.mu.Lock()
	defer ids.mu.Unlock()
	if t := ids.current.Load(); t != nil && t.custom == world.CustomItemCount() {
		return t
	}
	if ids.vanilla == nil {
		ids.vanilla = vanillaItemTable(ids.v.Items())
	}
	custom := world.CustomItems()
	t := &itemTable{
		custom: len(custom),
		toRID:  maps.Clone(ids.vanilla.toRID),
		toName: maps.Clone(ids.vanilla.toName),
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
	ids.current.Store(t)
	return t
}

// vanillaItemTable builds the table of the vanilla items of a registry.
func vanillaItemTable(m map[string]ItemEntry) *itemTable {
	t := &itemTable{toRID: make(map[string]int32, len(m)), toName: make(map[int32]string, len(m))}
	for name, e := range m {
		t.toRID[name] = e.RuntimeID
		t.toName[e.RuntimeID] = name
	}
	return t
}
