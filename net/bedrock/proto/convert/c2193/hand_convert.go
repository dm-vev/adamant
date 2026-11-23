package c2193

import (
	"github.com/dm-vev/adamant/net/bedrock/proto/v2169"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

func upItemInstance(in *v2169.ItemInstance) v2193.ItemInstance {
	return v2193.ItemInstance{StackNetworkID: in.StackNetworkID, Stack: v2193.ItemStack(in.Stack)}
}

func downItemInstance(in *v2193.ItemInstance) v2169.ItemInstance {
	return v2169.ItemInstance{StackNetworkID: in.StackNetworkID, Stack: v2169.ItemStack(in.Stack)}
}

func upStackRequestItem(in *v2169.StackRequestItem) v2193.StackRequestItem {
	return v2193.StackRequestItem(*in)
}

func downStackRequestItem(in *v2193.StackRequestItem) v2169.StackRequestItem {
	return v2169.StackRequestItem(*in)
}

func upRecipeIngredient(in *v2169.RecipeIngredient) v2193.RecipeIngredient {
	return v2193.RecipeIngredient{Descriptor: upItemDescriptor(in.Descriptor), Count: in.Count}
}

func downRecipeIngredient(in *v2193.RecipeIngredient) v2169.RecipeIngredient {
	return v2169.RecipeIngredient{Descriptor: downItemDescriptor(in.Descriptor), Count: in.Count}
}

func upItemDescriptor(d v2169.ItemDescriptor) v2193.ItemDescriptor {
	switch d := d.(type) {
	case *v2169.InvalidItemDescriptor:
		x := upInvalidItemDescriptor(d)
		return &x
	case *v2169.DefaultItemDescriptor:
		x := upDefaultItemDescriptor(d)
		return &x
	case *v2169.MoLangItemDescriptor:
		x := upMoLangItemDescriptor(d)
		return &x
	case *v2169.ItemTagItemDescriptor:
		x := upItemTagItemDescriptor(d)
		return &x
	}
	return nil
}

func downItemDescriptor(d v2193.ItemDescriptor) v2169.ItemDescriptor {
	switch d := d.(type) {
	case *v2193.InvalidItemDescriptor:
		x := downInvalidItemDescriptor(d)
		return &x
	case *v2193.DefaultItemDescriptor:
		x := downDefaultItemDescriptor(d)
		return &x
	case *v2193.MoLangItemDescriptor:
		x := downMoLangItemDescriptor(d)
		return &x
	case *v2193.ItemTagItemDescriptor:
		x := downItemTagItemDescriptor(d)
		return &x
	}
	return nil
}

func upPackSetting(in *v2169.PackSetting) v2193.PackSetting {
	return v2193.PackSetting(*in)
}

func downPackSetting(in *v2193.PackSetting) v2169.PackSetting {
	if _, ok := in.Value.([]string); ok {
		return v2169.PackSetting{Name: in.Name, Value: ""}
	}
	return v2169.PackSetting(*in)
}

func upShapeData(in *v2169.ShapeData) v2193.ShapeData {
	switch s := (*in).(type) {
	case *v2169.LastShape:
		x := upLastShape(s)
		return &x
	case *v2169.ArrowShape:
		x := upArrowShape(s)
		return &x
	case *v2169.TextShape:
		x := upTextShape(s)
		return &x
	case *v2169.BoxShape:
		x := upBoxShape(s)
		return &x
	case *v2169.LineShape:
		x := upLineShape(s)
		return &x
	case *v2169.SphereShape:
		x := upSphereShape(s)
		return &x
	case *v2169.CylinderShape:
		x := upCylinderShape(s)
		return &x
	case *v2169.PyramidShape:
		x := upPyramidShape(s)
		return &x
	case *v2169.EllipsoidShape:
		x := upEllipsoidShape(s)
		return &x
	case *v2169.ConeShape:
		x := upConeShape(s)
		return &x
	}
	return nil
}

func downShapeData(in *v2193.ShapeData) v2169.ShapeData {
	switch s := (*in).(type) {
	case *v2193.LastShape:
		x := downLastShape(s)
		return &x
	case *v2193.ArrowShape:
		x := downArrowShape(s)
		return &x
	case *v2193.TextShape:
		x := downTextShape(s)
		return &x
	case *v2193.BoxShape:
		x := downBoxShape(s)
		return &x
	case *v2193.LineShape:
		x := downLineShape(s)
		return &x
	case *v2193.SphereShape:
		x := downSphereShape(s)
		return &x
	case *v2193.CylinderShape:
		x := downCylinderShape(s)
		return &x
	case *v2193.PyramidShape:
		x := downPyramidShape(s)
		return &x
	case *v2193.EllipsoidShape:
		x := downEllipsoidShape(s)
		return &x
	case *v2193.ConeShape:
		x := downConeShape(s)
		return &x
	}
	return nil
}

func upStackRequestAction(in *v2169.StackRequestAction) v2193.StackRequestAction {
	switch a := (*in).(type) {
	case *v2169.TakeStackRequestAction:
		x := upTakeStackRequestAction(a)
		return &x
	case *v2169.PlaceStackRequestAction:
		x := upPlaceStackRequestAction(a)
		return &x
	case *v2169.SwapStackRequestAction:
		x := upSwapStackRequestAction(a)
		return &x
	case *v2169.DropStackRequestAction:
		x := upDropStackRequestAction(a)
		return &x
	case *v2169.DestroyStackRequestAction:
		x := upDestroyStackRequestAction(a)
		return &x
	case *v2169.ConsumeStackRequestAction:
		x := upConsumeStackRequestAction(a)
		return &x
	case *v2169.CreateStackRequestAction:
		x := upCreateStackRequestAction(a)
		return &x
	case *v2169.LabTableCombineStackRequestAction:
		x := upLabTableCombineStackRequestAction(a)
		return &x
	case *v2169.BeaconPaymentStackRequestAction:
		x := upBeaconPaymentStackRequestAction(a)
		return &x
	case *v2169.MineBlockStackRequestAction:
		x := upMineBlockStackRequestAction(a)
		return &x
	case *v2169.CraftRecipeStackRequestAction:
		x := upCraftRecipeStackRequestAction(a)
		return &x
	case *v2169.AutoCraftRecipeStackRequestAction:
		x := upAutoCraftRecipeStackRequestAction(a)
		return &x
	case *v2169.CraftCreativeStackRequestAction:
		x := upCraftCreativeStackRequestAction(a)
		return &x
	case *v2169.CraftRecipeOptionalStackRequestAction:
		x := upCraftRecipeOptionalStackRequestAction(a)
		return &x
	case *v2169.CraftGrindstoneRecipeStackRequestAction:
		x := upCraftGrindstoneRecipeStackRequestAction(a)
		return &x
	case *v2169.CraftLoomRecipeStackRequestAction:
		x := upCraftLoomRecipeStackRequestAction(a)
		return &x
	case *v2169.CraftNonImplementedStackRequestAction:
		x := upCraftNonImplementedStackRequestAction(a)
		return &x
	case *v2169.CraftResultsDeprecatedStackRequestAction:
		x := upCraftResultsDeprecatedStackRequestAction(a)
		return &x
	}
	return nil
}

func downStackRequestAction(in *v2193.StackRequestAction) v2169.StackRequestAction {
	switch a := (*in).(type) {
	case *v2193.TakeStackRequestAction:
		x := downTakeStackRequestAction(a)
		return &x
	case *v2193.PlaceStackRequestAction:
		x := downPlaceStackRequestAction(a)
		return &x
	case *v2193.SwapStackRequestAction:
		x := downSwapStackRequestAction(a)
		return &x
	case *v2193.DropStackRequestAction:
		x := downDropStackRequestAction(a)
		return &x
	case *v2193.DestroyStackRequestAction:
		x := downDestroyStackRequestAction(a)
		return &x
	case *v2193.ConsumeStackRequestAction:
		x := downConsumeStackRequestAction(a)
		return &x
	case *v2193.CreateStackRequestAction:
		x := downCreateStackRequestAction(a)
		return &x
	case *v2193.LabTableCombineStackRequestAction:
		x := downLabTableCombineStackRequestAction(a)
		return &x
	case *v2193.BeaconPaymentStackRequestAction:
		x := downBeaconPaymentStackRequestAction(a)
		return &x
	case *v2193.MineBlockStackRequestAction:
		x := downMineBlockStackRequestAction(a)
		return &x
	case *v2193.CraftRecipeStackRequestAction:
		x := downCraftRecipeStackRequestAction(a)
		return &x
	case *v2193.AutoCraftRecipeStackRequestAction:
		x := downAutoCraftRecipeStackRequestAction(a)
		return &x
	case *v2193.CraftCreativeStackRequestAction:
		x := downCraftCreativeStackRequestAction(a)
		return &x
	case *v2193.CraftRecipeOptionalStackRequestAction:
		x := downCraftRecipeOptionalStackRequestAction(a)
		return &x
	case *v2193.CraftGrindstoneRecipeStackRequestAction:
		x := downCraftGrindstoneRecipeStackRequestAction(a)
		return &x
	case *v2193.CraftLoomRecipeStackRequestAction:
		x := downCraftLoomRecipeStackRequestAction(a)
		return &x
	case *v2193.CraftNonImplementedStackRequestAction:
		x := downCraftNonImplementedStackRequestAction(a)
		return &x
	case *v2193.CraftResultsDeprecatedStackRequestAction:
		x := downCraftResultsDeprecatedStackRequestAction(a)
		return &x
	}
	return nil
}

func upTransactionData(d v2169.InventoryTransactionData) v2193.InventoryTransactionData {
	switch d := d.(type) {
	case *v2169.NormalTransactionData:
		x := upNormalTransactionData(d)
		return &x
	case *v2169.MismatchTransactionData:
		x := upMismatchTransactionData(d)
		return &x
	case *v2169.UseItemTransactionData:
		x := upUseItemTransactionData(d)
		return &x
	case *v2169.UseItemOnEntityTransactionData:
		x := upUseItemOnEntityTransactionData(d)
		return &x
	case *v2169.ReleaseItemTransactionData:
		x := upReleaseItemTransactionData(d)
		return &x
	}
	return nil
}

func downTransactionData(d v2193.InventoryTransactionData) v2169.InventoryTransactionData {
	switch d := d.(type) {
	case *v2193.NormalTransactionData:
		x := downNormalTransactionData(d)
		return &x
	case *v2193.MismatchTransactionData:
		x := downMismatchTransactionData(d)
		return &x
	case *v2193.UseItemTransactionData:
		x := downUseItemTransactionData(d)
		return &x
	case *v2193.UseItemOnEntityTransactionData:
		x := downUseItemOnEntityTransactionData(d)
		return &x
	case *v2193.ReleaseItemTransactionData:
		x := downReleaseItemTransactionData(d)
		return &x
	}
	return nil
}

func upInventoryTransactionPacket(in *v2169.InventoryTransactionPacket) *v2193.InventoryTransactionPacket {
	out := &v2193.InventoryTransactionPacket{LegacyRequestID: in.LegacyRequestID, TransactionData: upTransactionData(in.TransactionData)}
	if in.LegacySetItemSlots != nil {
		out.LegacySetItemSlots = make([]v2193.LegacySetItemSlot, len(in.LegacySetItemSlots))
		for i := range in.LegacySetItemSlots {
			out.LegacySetItemSlots[i] = upLegacySetItemSlot(&in.LegacySetItemSlots[i])
		}
	}
	if in.Actions != nil {
		out.Actions = make([]v2193.InventoryAction, len(in.Actions))
		for i := range in.Actions {
			out.Actions[i] = upInventoryAction(&in.Actions[i])
		}
	}
	return out
}

func downInventoryTransactionPacket(in *v2193.InventoryTransactionPacket) *v2169.InventoryTransactionPacket {
	out := &v2169.InventoryTransactionPacket{LegacyRequestID: in.LegacyRequestID, TransactionData: downTransactionData(in.TransactionData)}
	if in.LegacySetItemSlots != nil {
		out.LegacySetItemSlots = make([]v2169.LegacySetItemSlot, len(in.LegacySetItemSlots))
		for i := range in.LegacySetItemSlots {
			out.LegacySetItemSlots[i] = downLegacySetItemSlot(&in.LegacySetItemSlots[i])
		}
	}
	if in.Actions != nil {
		out.Actions = make([]v2169.InventoryAction, len(in.Actions))
		for i := range in.Actions {
			out.Actions[i] = downInventoryAction(&in.Actions[i])
		}
	}
	return out
}
