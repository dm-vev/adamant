package item_test

import (
	"testing"

	_ "github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/item"
)

// TestCookingDataResolves checks every item the Java cooking recipes and
// fuels name is registered, so none of them is silently left out.
func TestCookingDataResolves(t *testing.T) {
	for _, id := range item.CookingItemIDs() {
		if _, ok := item.CanonicalItem(id); !ok {
			t.Errorf("Java cooking item %v has no registered item", id)
		}
	}
}
