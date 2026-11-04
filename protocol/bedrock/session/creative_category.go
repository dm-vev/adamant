package session

import (
	"github.com/df-mc/dragonfly/server/item/creative"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
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
