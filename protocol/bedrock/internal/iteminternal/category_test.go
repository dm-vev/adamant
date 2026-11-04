package iteminternal

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item/category"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// TestCreativeCategoryValues holds the creative category of each category of
// custom items, with and without a group, and of the zero Category, to the
// protocol's constant and to the number Category.Uint8 gave before the
// adapter mapped it: 1 to 4 in the order of the constructors, and 0.
func TestCreativeCategoryValues(t *testing.T) {
	for _, c := range []struct {
		name      string
		category  category.Category
		want, was int32
	}{
		{"Construction", category.Construction(), mc.CreativeCategoryConstruction, 1},
		{"Nature", category.Nature(), mc.CreativeCategoryNature, 2},
		{"Equipment", category.Equipment(), mc.CreativeCategoryEquipment, 3},
		{"Items", category.Items(), mc.CreativeCategoryItems, 4},
		{"Category{}", category.Category{}, mc.CreativeCategoryAll, 0},
	} {
		for _, group := range []string{"", "sword"} {
			got := creativeCategory(c.category.WithGroup(group))
			if got != c.want || got != c.was {
				t.Errorf("creativeCategory(%s, group %q) = %d, protocol has %d, Uint8 gave %d", c.name, group, got, c.want, c.was)
			}
		}
	}
}

// TestComponentsNameCreativeCategory checks that the components of a custom
// item carry the creative category and group of the item.
func TestComponentsNameCreativeCategory(t *testing.T) {
	m := NewComponentBuilder("Test", "test:item", category.Equipment().WithGroup("sword")).Construct()
	props := m["components"].(map[string]any)["item_properties"].(map[string]any)
	if props["creative_category"] != int32(mc.CreativeCategoryEquipment) || props["creative_group"] != "itemGroup.name.sword" {
		t.Errorf("creative_category %v, creative_group %v", props["creative_category"], props["creative_group"])
	}
}
