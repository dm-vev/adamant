package creative

import (
	"sort"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

func TestBoatsPresentInCreativeInventory(t *testing.T) {
	creativeGroups = nil
	creativeItemStacks = nil
	ensureBoatEntries = sync.Once{}

	registerCreativeItems()

	var hasBoatGroup bool
	for _, group := range Groups() {
		if group.Name == "itemGroup.name.boats" {
			hasBoatGroup = true
			break
		}
	}
	if !hasBoatGroup {
		t.Fatalf("boat group not registered in creative inventory")
	}

	expected := make(map[string]struct{})
	for _, variant := range item.BoatVariants() {
		expected[variant+":plain"] = struct{}{}
		expected[variant+":chest"] = struct{}{}
	}

	for _, entry := range Items() {
		boat, ok := entry.Stack.Item().(item.Boat)
		if !ok {
			continue
		}

		key := boat.Variant + ":plain"
		if boat.Chest {
			key = boat.Variant + ":chest"
		}
		delete(expected, key)
	}

	if len(expected) > 0 {
		missing := make([]string, 0, len(expected))
		for key := range expected {
			missing = append(missing, key)
		}
		sort.Strings(missing)
		t.Fatalf("missing boat creative entries: %v", missing)
	}
}
