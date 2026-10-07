package recipe

import "github.com/df-mc/dragonfly/server/item"

func init() {
	Register(NewSmithingTransform(
		item.NewStack(item.NautilusArmor{Material: item.NautilusArmorDiamond}, 1),
		item.NewStack(item.NetheriteIngot{}, 1),
		item.NewStack(item.SmithingTemplate{Template: item.TemplateNetheriteUpgrade()}, 1),
		item.NewStack(item.NautilusArmor{Material: item.NautilusArmorNetherite}, 1),
		"smithing_table",
	))
}
