package c2225

// sameHand lists what the codec packages code by hand that has the same
// layout in 2193 and 2225, as gophertunnel's compare v1.62.0...master
// shows: declarations of the schemas by name, special wire types by the
// name of their Go type. The rest of what is coded by hand changed: the
// Skin and the PlayerListEntry (the PlayFab ID), the actions of a stack
// request (the reserved craft is 18), the data of an inventory transaction
// and the transaction (the Hand of two of its kinds), and Optional, the
// easing of a spline key frame, new in 2225.
var sameHand = []string{
	// Special wire types.
	"Metadata", "ItemStack", "ItemInstance", "StackRequestItem", "ItemDescriptorCount", "MaterialReducer",
	"Event", "UseItemTransactionData", "GameRule", "AbilityValue", "PackSetting", "ShapeData",
	"InputFlags", "Bitset",
	// Declarations coded by hand.
	"Text", "ResourcePackClientResponse", "PlayerUpdateEntityOverrides", "ScoreboardEntry", "HeightMap",
	"PersonaPieceTintColour", "RecipeIngredient",
}

// convertTypes lists the types the converters of hand_convert.go convert,
// which no field of the schemas holds: the kinds of the unions the codec
// packages code by hand.
var convertTypes = []string{
	"LegacySetItemSlot", "InventoryAction", "SkinAnimation", "PersonaPiece",
	"NormalTransactionData", "MismatchTransactionData", "UseItemTransactionData",
	"UseItemOnEntityTransactionData", "ReleaseItemTransactionData",
	"TakeStackRequestAction", "PlaceStackRequestAction", "SwapStackRequestAction", "DropStackRequestAction",
	"DestroyStackRequestAction", "ConsumeStackRequestAction", "CreateStackRequestAction",
	"LabTableCombineStackRequestAction", "BeaconPaymentStackRequestAction", "MineBlockStackRequestAction",
	"CraftRecipeStackRequestAction", "AutoCraftRecipeStackRequestAction", "CraftCreativeStackRequestAction",
	"CraftRecipeOptionalStackRequestAction", "CraftGrindstoneRecipeStackRequestAction",
	"CraftLoomRecipeStackRequestAction", "CraftReservedStackRequestAction",
	"CraftNonImplementedStackRequestAction", "CraftResultsDeprecatedStackRequestAction",
	"InvalidItemDescriptor", "DefaultItemDescriptor", "MoLangItemDescriptor", "ItemTagItemDescriptor",
	"SystemCategory", "CameraProgressOption", "CameraRotationOption",
	"CameraSplineProgressKeyFrame", "CameraSplineRotationKeyFrame",
	"AttributeTransitionSettings", "AttributeNoiseTransitionSettings", "NoiseAlignment", "SoundDataUpdate",
}
