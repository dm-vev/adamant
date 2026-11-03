package creative

import (
	"testing"

	"github.com/dm-vev/adamant/net/nbt"
)

// TestCreativeGroupsHaveKnownCategories checks that every group of the
// vanilla creative inventory has the category of one of the constructors,
// which adapters number the creative tabs by.
func TestCreativeGroupsHaveKnownCategories(t *testing.T) {
	var m struct {
		Groups []creativeGroupEntry `nbt:"groups"`
		Items  []creativeItemEntry  `nbt:"items"`
	}
	if err := nbt.Unmarshal(creativeItemData, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Groups) == 0 {
		t.Fatal("no creative groups")
	}
	for _, g := range m.Groups {
		switch (Category{category(g.Category)}) {
		case ConstructionCategory(), NatureCategory(), EquipmentCategory(), ItemsCategory():
		default:
			t.Errorf("group %q has category %d, of no constructor", g.Name, g.Category)
		}
	}
}
