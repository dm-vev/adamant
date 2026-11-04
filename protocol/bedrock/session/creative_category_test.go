package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item/creative"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// TestCreativeCategoryValues holds the category CreativeContent sends for
// each creative category, and for the zero Category, to the protocol's
// constant and to the number Category.Uint8 gave before the adapter mapped
// it: 1 to 4 in the order of the constructors, and 0.
func TestCreativeCategoryValues(t *testing.T) {
	var values []gameValue
	for _, c := range []struct {
		name      string
		category  creative.Category
		want, was int64
	}{
		{"ConstructionCategory", creative.ConstructionCategory(), mc.CreativeCategoryConstruction, 1},
		{"NatureCategory", creative.NatureCategory(), mc.CreativeCategoryNature, 2},
		{"EquipmentCategory", creative.EquipmentCategory(), mc.CreativeCategoryEquipment, 3},
		{"ItemsCategory", creative.ItemsCategory(), mc.CreativeCategoryItems, 4},
		{"Category{}", creative.Category{}, mc.CreativeCategoryAll, 0},
	} {
		got := int64(bedrockCreativeCategory(c.category))
		values = append(values, gameValue{c.name, got, c.want}, gameValue{c.name + " (Uint8)", got, c.was})
	}
	checkGameValues(t, values)
}
