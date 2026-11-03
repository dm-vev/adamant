package iteminternal

import (
	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// creativeCategory returns the creative category the item components of a
// custom item name for the category passed, whatever its group: the number
// of its creative tab, or 0 (All) for the zero Category.
func creativeCategory(c category.Category) int32 {
	switch c.WithGroup("") {
	case category.Construction():
		return v2193.CreativeCategoryConstruction
	case category.Nature():
		return v2193.CreativeCategoryNature
	case category.Equipment():
		return v2193.CreativeCategoryEquipment
	case category.Items():
		return v2193.CreativeCategoryItems
	}
	return v2193.CreativeCategoryAll
}
