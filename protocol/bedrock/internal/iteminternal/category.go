package iteminternal

import (
	"github.com/df-mc/dragonfly/server/item/category"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// creativeCategory returns the creative category the item components of a
// custom item name for the category passed, whatever its group: the number
// of its creative tab, or 0 (All) for the zero Category.
func creativeCategory(c category.Category) int32 {
	switch c.WithGroup("") {
	case category.Construction():
		return mc.CreativeCategoryConstruction
	case category.Nature():
		return mc.CreativeCategoryNature
	case category.Equipment():
		return mc.CreativeCategoryEquipment
	case category.Items():
		return mc.CreativeCategoryItems
	}
	return mc.CreativeCategoryAll
}
