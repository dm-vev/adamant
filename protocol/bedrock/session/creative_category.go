package session

import (
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/server/item/creative"
)

// bedrockCreativeCategory returns the creative category CreativeContent sends
// for the category of a creative group: the number of its creative tab, or 0
// (All) for the zero Category.
func bedrockCreativeCategory(c creative.Category) uint8 {
	switch c {
	case creative.ConstructionCategory():
		return mc.CreativeCategoryConstruction
	case creative.NatureCategory():
		return mc.CreativeCategoryNature
	case creative.EquipmentCategory():
		return mc.CreativeCategoryEquipment
	case creative.ItemsCategory():
		return mc.CreativeCategoryItems
	}
	return mc.CreativeCategoryAll
}
