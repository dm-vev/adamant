package c2193

// sameHand lists what the codec packages code by hand that has the same
// layout in 2169 and 2193. InputFlags (presence bool), PackSetting
// (string-list type), ShapeData (TextShape.LineGapHeight),
// InventoryTransaction (hasType/hasActions bools, UseItemTransactionData.Hand)
// and HeightMap changed between 2169 and 2193.
var sameHand = []string{
	// Special wire types.
	"Metadata", "ItemStack", "ItemInstance", "StackRequestItem", "ItemDescriptorCount", "StackRequestAction",
	"MaterialReducer", "Event", "GameRule", "AbilityValue", "Bitset",
	// Declarations coded by hand.
	"PlayerListEntry", "Skin", "PersonaPieceTintColour",
	"Text", "ResourcePackClientResponse", "PlayerUpdateEntityOverrides", "ScoreboardEntry",
	"RecipeIngredient", "Optional",
}

// convertTypes lists the union members whose converters hand_convert.go calls.
var convertTypes = []string{
	"LegacySetItemSlot", "InventoryAction",
	"NormalTransactionData", "MismatchTransactionData", "UseItemTransactionData",
	"UseItemOnEntityTransactionData", "ReleaseItemTransactionData",
	"TakeStackRequestAction", "PlaceStackRequestAction", "SwapStackRequestAction", "DropStackRequestAction",
	"DestroyStackRequestAction", "ConsumeStackRequestAction", "CreateStackRequestAction",
	"LabTableCombineStackRequestAction", "BeaconPaymentStackRequestAction", "MineBlockStackRequestAction",
	"CraftRecipeStackRequestAction", "AutoCraftRecipeStackRequestAction", "CraftCreativeStackRequestAction",
	"CraftRecipeOptionalStackRequestAction", "CraftGrindstoneRecipeStackRequestAction",
	"CraftLoomRecipeStackRequestAction", "CraftNonImplementedStackRequestAction",
	"CraftResultsDeprecatedStackRequestAction",
	"InvalidItemDescriptor", "DefaultItemDescriptor", "MoLangItemDescriptor", "ItemTagItemDescriptor",
	"LastShape", "ArrowShape", "TextShape", "BoxShape", "LineShape",
	"SphereShape", "CylinderShape", "PyramidShape", "EllipsoidShape", "ConeShape",
}
