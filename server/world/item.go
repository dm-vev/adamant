package world

import (
	"fmt"
	"image"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/item/category"
)

// Item represents an item that may be added to an inventory. It has a method to encode the item to an ID and
// a metadata value.
type Item interface {
	// EncodeItem encodes the item to its Minecraft representation, which consists of a numerical ID and a
	// metadata value.
	EncodeItem() (name string, meta int16)
}

// CustomItem represents an item that is non-vanilla and requires a resource pack and extra steps to show it
// to the client.
type CustomItem interface {
	Item
	// Name is the name that will be displayed on the item to all clients.
	Name() string
	// Texture is the Image of the texture for this item.
	Texture() image.Image
	// Category is the category the item will be listed under in the creative inventory.
	Category() category.Category
}

// RegisterItem registers an item with the ID and meta passed. Once registered, items may be obtained from an
// ID and metadata value using ItemByName.
// If an item with the ID and meta passed already exists, RegisterItem panics.
func RegisterItem(item Item) {
	itemMu.Lock()
	defer itemMu.Unlock()

	name, meta := item.EncodeItem()
	h := itemHash{name: name, meta: meta}

	if _, ok := items[h]; ok {
		panic(fmt.Sprintf("item registered with name %v and meta %v already exists", name, meta))
	}
	if c, ok := item.(CustomItem); ok {
		customItems = append(customItems, c)
		customItemCount.Add(1)
	}
	items[h] = item
}

// itemHash is a combination of an item's name and metadata. It is used as a key in hash maps.
type itemHash struct {
	name string
	meta int16
}

var (
	// itemMu protects all item registry maps and slices.
	itemMu sync.RWMutex
	// items holds a list of all registered items, indexed using the itemHash created when calling
	// Item.EncodeItem.
	items = map[itemHash]Item{}
	// customItems holds a list of all registered custom items.
	customItems []CustomItem
	// customItemCount is the number of custom items registered, len(customItems).
	customItemCount atomic.Int64
)

// ItemByName attempts to return an item by a name and a metadata value.
func ItemByName(name string, meta int16) (Item, bool) {
	itemMu.RLock()
	defer itemMu.RUnlock()

	it, ok := items[itemHash{name: name, meta: meta}]
	if !ok {
		// Also try obtaining the item with a metadata value of 0, for cases with durability.
		it, ok = items[itemHash{name: name}]
	}
	return it, ok
}

// Items returns a slice of all registered items.
func Items() []Item {
	itemMu.RLock()
	defer itemMu.RUnlock()

	m := make([]Item, 0, len(items))
	for _, i := range items {
		m = append(m, i)
	}
	return m
}

// CustomItemCount returns the number of custom items registered, which is
// len(CustomItems()). It only grows, and it costs no lock, so that data kept
// per custom item may be checked against it cheaply.
func CustomItemCount() int {
	return int(customItemCount.Load())
}

// CustomItems returns a slice of all registered custom items, in the order
// they were registered.
func CustomItems() []CustomItem {
	itemMu.RLock()
	defer itemMu.RUnlock()
	return slices.Clone(customItems)
}
