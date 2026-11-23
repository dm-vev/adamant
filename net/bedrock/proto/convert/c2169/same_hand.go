package c2169

// sameHand lists what the codec packages code by hand that has the same
// layout in 2168 and 2169: everything except ScoreboardEntry, whose
// remove variant uses a double optional in 2168 (1.26.44 hotfix) and a
// single optional in 2169 (1.26.45).
var sameHand = []string{
	// Special wire types.
	"Metadata", "ItemStack", "ItemInstance", "StackRequestItem", "ItemDescriptorCount", "StackRequestAction",
	"MaterialReducer", "Event", "InventoryTransactionData", "UseItemTransactionData", "GameRule",
	"AbilityValue", "PackSetting", "ShapeData", "InputFlags", "Bitset",
	// Declarations coded by hand.
	"InventoryTransaction", "PlayerListEntry", "Skin", "PersonaPieceTintColour",
	"Text", "ResourcePackClientResponse", "PlayerUpdateEntityOverrides", "RecipeIngredient", "Optional",
}
