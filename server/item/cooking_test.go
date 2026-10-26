package item_test

import (
	"testing"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
)

// TestCookingDataResolves checks every item the Java cooking recipes and
// fuels name is registered, so none of them is silently left out.
func TestCookingDataResolves(t *testing.T) {
	for _, id := range item.JavaCookingItemIDs() {
		if _, ok := item.JavaItem(id); !ok {
			t.Errorf("Java cooking item %v has no registered item", id)
		}
	}
}
