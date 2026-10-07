package recipe

import "github.com/df-mc/dragonfly/server/item"

func init() {
	empty := NewItemTag("", 0)
	stick := item.NewStack(item.Stick{}, 1)

	materials := []struct {
		tag    string
		item   item.Stack
		output item.Spear
	}{
		{tag: "minecraft:planks", output: item.Spear{Tier: item.ToolTierWood}},
		{tag: "minecraft:stone_tool_materials", output: item.Spear{Tier: item.ToolTierStone}},
		{item: item.NewStack(item.CopperIngot{}, 1), output: item.Spear{Tier: item.ToolTierCopper}},
		{item: item.NewStack(item.IronIngot{}, 1), output: item.Spear{Tier: item.ToolTierIron}},
		{item: item.NewStack(item.GoldIngot{}, 1), output: item.Spear{Tier: item.ToolTierGold}},
		{item: item.NewStack(item.Diamond{}, 1), output: item.Spear{Tier: item.ToolTierDiamond}},
	}
	for _, material := range materials {
		var ingredient Item
		if material.tag != "" {
			ingredient = NewItemTag(material.tag, 1)
		} else {
			ingredient = material.item
		}
		Register(NewShaped([]Item{
			empty, empty, ingredient,
			empty, stick, empty,
			stick, empty, empty,
		}, item.NewStack(material.output, 1), NewShape(3, 3), "crafting_table"))
	}

	Register(NewSmithingTransform(
		item.NewStack(item.Spear{Tier: item.ToolTierDiamond}, 1),
		item.NewStack(item.NetheriteIngot{}, 1),
		item.NewStack(item.SmithingTemplate{Template: item.TemplateNetheriteUpgrade()}, 1),
		item.NewStack(item.Spear{Tier: item.ToolTierNetherite}, 1),
		"smithing_table",
	))
}
