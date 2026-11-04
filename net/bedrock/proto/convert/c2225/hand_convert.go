package c2225

import (
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// The converters of what the codec packages code by hand. The types alike
// in both convert by a Go conversion; the unions convert kind by kind,
// with the converters convert_gen.go writes of the types convertTypes
// lists.

func upGameRule(in *v2193.GameRule) v2225.GameRule   { return v2225.GameRule(*in) }
func downGameRule(in *v2225.GameRule) v2193.GameRule { return v2193.GameRule(*in) }

func upItemInstance(in *v2193.ItemInstance) v2225.ItemInstance {
	return v2225.ItemInstance{StackNetworkID: in.StackNetworkID, Stack: v2225.ItemStack(in.Stack)}
}

func downItemInstance(in *v2225.ItemInstance) v2193.ItemInstance {
	return v2193.ItemInstance{StackNetworkID: in.StackNetworkID, Stack: v2193.ItemStack(in.Stack)}
}

func upStackRequestItem(in *v2193.StackRequestItem) v2225.StackRequestItem {
	return v2225.StackRequestItem(*in)
}

func downStackRequestItem(in *v2225.StackRequestItem) v2193.StackRequestItem {
	return v2193.StackRequestItem(*in)
}

func upRecipeIngredient(in *v2193.RecipeIngredient) v2225.RecipeIngredient {
	return v2225.RecipeIngredient{Descriptor: upItemDescriptor(in.Descriptor), Count: in.Count}
}

func downRecipeIngredient(in *v2225.RecipeIngredient) v2193.RecipeIngredient {
	return v2193.RecipeIngredient{Descriptor: downItemDescriptor(in.Descriptor), Count: in.Count}
}

func upItemDescriptor(d v2193.ItemDescriptor) v2225.ItemDescriptor {
	switch d := d.(type) {
	case *v2193.InvalidItemDescriptor:
		x := upInvalidItemDescriptor(d)
		return &x
	case *v2193.DefaultItemDescriptor:
		x := upDefaultItemDescriptor(d)
		return &x
	case *v2193.MoLangItemDescriptor:
		x := upMoLangItemDescriptor(d)
		return &x
	case *v2193.ItemTagItemDescriptor:
		x := upItemTagItemDescriptor(d)
		return &x
	}
	return nil
}

func downItemDescriptor(d v2225.ItemDescriptor) v2193.ItemDescriptor {
	switch d := d.(type) {
	case *v2225.InvalidItemDescriptor:
		x := downInvalidItemDescriptor(d)
		return &x
	case *v2225.DefaultItemDescriptor:
		x := downDefaultItemDescriptor(d)
		return &x
	case *v2225.MoLangItemDescriptor:
		x := downMoLangItemDescriptor(d)
		return &x
	case *v2225.ItemTagItemDescriptor:
		x := downItemTagItemDescriptor(d)
		return &x
	}
	return nil
}

// upStackRequestAction converts an action of a stack request kind by kind.
func upStackRequestAction(in *v2193.StackRequestAction) v2225.StackRequestAction {
	switch a := (*in).(type) {
	case *v2193.TakeStackRequestAction:
		x := upTakeStackRequestAction(a)
		return &x
	case *v2193.PlaceStackRequestAction:
		x := upPlaceStackRequestAction(a)
		return &x
	case *v2193.SwapStackRequestAction:
		x := upSwapStackRequestAction(a)
		return &x
	case *v2193.DropStackRequestAction:
		x := upDropStackRequestAction(a)
		return &x
	case *v2193.DestroyStackRequestAction:
		x := upDestroyStackRequestAction(a)
		return &x
	case *v2193.ConsumeStackRequestAction:
		x := upConsumeStackRequestAction(a)
		return &x
	case *v2193.CreateStackRequestAction:
		x := upCreateStackRequestAction(a)
		return &x
	case *v2193.LabTableCombineStackRequestAction:
		x := upLabTableCombineStackRequestAction(a)
		return &x
	case *v2193.BeaconPaymentStackRequestAction:
		x := upBeaconPaymentStackRequestAction(a)
		return &x
	case *v2193.MineBlockStackRequestAction:
		x := upMineBlockStackRequestAction(a)
		return &x
	case *v2193.CraftRecipeStackRequestAction:
		x := upCraftRecipeStackRequestAction(a)
		return &x
	case *v2193.AutoCraftRecipeStackRequestAction:
		x := upAutoCraftRecipeStackRequestAction(a)
		return &x
	case *v2193.CraftCreativeStackRequestAction:
		x := upCraftCreativeStackRequestAction(a)
		return &x
	case *v2193.CraftRecipeOptionalStackRequestAction:
		x := upCraftRecipeOptionalStackRequestAction(a)
		return &x
	case *v2193.CraftGrindstoneRecipeStackRequestAction:
		x := upCraftGrindstoneRecipeStackRequestAction(a)
		return &x
	case *v2193.CraftLoomRecipeStackRequestAction:
		x := upCraftLoomRecipeStackRequestAction(a)
		return &x
	case *v2193.CraftNonImplementedStackRequestAction:
		x := upCraftNonImplementedStackRequestAction(a)
		return &x
	case *v2193.CraftResultsDeprecatedStackRequestAction:
		x := upCraftResultsDeprecatedStackRequestAction(a)
		return &x
	}
	return nil
}

// downStackRequestAction converts an action of a stack request kind by
// kind. The reserved craft of 2225 has no form in 2193: it is nil, which
// fixDownItemStackRequest leaves out of its request.
func downStackRequestAction(in *v2225.StackRequestAction) v2193.StackRequestAction {
	switch a := (*in).(type) {
	case *v2225.TakeStackRequestAction:
		x := downTakeStackRequestAction(a)
		return &x
	case *v2225.PlaceStackRequestAction:
		x := downPlaceStackRequestAction(a)
		return &x
	case *v2225.SwapStackRequestAction:
		x := downSwapStackRequestAction(a)
		return &x
	case *v2225.DropStackRequestAction:
		x := downDropStackRequestAction(a)
		return &x
	case *v2225.DestroyStackRequestAction:
		x := downDestroyStackRequestAction(a)
		return &x
	case *v2225.ConsumeStackRequestAction:
		x := downConsumeStackRequestAction(a)
		return &x
	case *v2225.CreateStackRequestAction:
		x := downCreateStackRequestAction(a)
		return &x
	case *v2225.LabTableCombineStackRequestAction:
		x := downLabTableCombineStackRequestAction(a)
		return &x
	case *v2225.BeaconPaymentStackRequestAction:
		x := downBeaconPaymentStackRequestAction(a)
		return &x
	case *v2225.MineBlockStackRequestAction:
		x := downMineBlockStackRequestAction(a)
		return &x
	case *v2225.CraftRecipeStackRequestAction:
		x := downCraftRecipeStackRequestAction(a)
		return &x
	case *v2225.AutoCraftRecipeStackRequestAction:
		x := downAutoCraftRecipeStackRequestAction(a)
		return &x
	case *v2225.CraftCreativeStackRequestAction:
		x := downCraftCreativeStackRequestAction(a)
		return &x
	case *v2225.CraftRecipeOptionalStackRequestAction:
		x := downCraftRecipeOptionalStackRequestAction(a)
		return &x
	case *v2225.CraftGrindstoneRecipeStackRequestAction:
		x := downCraftGrindstoneRecipeStackRequestAction(a)
		return &x
	case *v2225.CraftLoomRecipeStackRequestAction:
		x := downCraftLoomRecipeStackRequestAction(a)
		return &x
	case *v2225.CraftNonImplementedStackRequestAction:
		x := downCraftNonImplementedStackRequestAction(a)
		return &x
	case *v2225.CraftResultsDeprecatedStackRequestAction:
		x := downCraftResultsDeprecatedStackRequestAction(a)
		return &x
	}
	return nil
}

func upTransactionData(d v2193.InventoryTransactionData) v2225.InventoryTransactionData {
	switch d := d.(type) {
	case *v2193.NormalTransactionData:
		x := upNormalTransactionData(d)
		return &x
	case *v2193.MismatchTransactionData:
		x := upMismatchTransactionData(d)
		return &x
	case *v2193.UseItemTransactionData:
		x := upUseItemTransactionData(d)
		return &x
	case *v2193.UseItemOnEntityTransactionData:
		// The Hand of 2225 is zero: the main hand, as 2193 meant.
		x := upUseItemOnEntityTransactionData(d)
		return &x
	case *v2193.ReleaseItemTransactionData:
		x := upReleaseItemTransactionData(d)
		return &x
	}
	return nil
}

func downTransactionData(d v2225.InventoryTransactionData) v2193.InventoryTransactionData {
	switch d := d.(type) {
	case *v2225.NormalTransactionData:
		x := downNormalTransactionData(d)
		return &x
	case *v2225.MismatchTransactionData:
		x := downMismatchTransactionData(d)
		return &x
	case *v2225.UseItemTransactionData:
		x := downUseItemTransactionData(d)
		return &x
	case *v2225.UseItemOnEntityTransactionData:
		x := downUseItemOnEntityTransactionData(d)
		return &x
	case *v2225.ReleaseItemTransactionData:
		x := downReleaseItemTransactionData(d)
		return &x
	}
	return nil
}

func upInventoryTransactionPacket(in *v2193.InventoryTransactionPacket) *v2225.InventoryTransactionPacket {
	out := &v2225.InventoryTransactionPacket{LegacyRequestID: in.LegacyRequestID, TransactionData: upTransactionData(in.TransactionData)}
	if in.LegacySetItemSlots != nil {
		out.LegacySetItemSlots = make([]v2225.LegacySetItemSlot, len(in.LegacySetItemSlots))
		for i := range in.LegacySetItemSlots {
			out.LegacySetItemSlots[i] = upLegacySetItemSlot(&in.LegacySetItemSlots[i])
		}
	}
	if in.Actions != nil {
		out.Actions = make([]v2225.InventoryAction, len(in.Actions))
		for i := range in.Actions {
			out.Actions[i] = upInventoryAction(&in.Actions[i])
		}
	}
	return out
}

func downInventoryTransactionPacket(in *v2225.InventoryTransactionPacket) *v2193.InventoryTransactionPacket {
	out := &v2193.InventoryTransactionPacket{LegacyRequestID: in.LegacyRequestID, TransactionData: downTransactionData(in.TransactionData)}
	if in.LegacySetItemSlots != nil {
		out.LegacySetItemSlots = make([]v2193.LegacySetItemSlot, len(in.LegacySetItemSlots))
		for i := range in.LegacySetItemSlots {
			out.LegacySetItemSlots[i] = downLegacySetItemSlot(&in.LegacySetItemSlots[i])
		}
	}
	if in.Actions != nil {
		out.Actions = make([]v2193.InventoryAction, len(in.Actions))
		for i := range in.Actions {
			out.Actions[i] = downInventoryAction(&in.Actions[i])
		}
	}
	return out
}

// upPlayerListEntry converts an entry of the player list. The PlayFab ID
// 2193 sends in the skin goes in the entry in 2225, whose skin has none.
func upPlayerListEntry(in *v2193.PlayerListEntry) v2225.PlayerListEntry {
	return v2225.PlayerListEntry{
		ActionType:     in.ActionType,
		UUID:           in.UUID,
		EntityUniqueID: in.EntityUniqueID,
		Username:       in.Username,
		XUID:           in.XUID,
		PlayFabID:      in.Skin.PlayFabID,
		PlatformChatID: in.PlatformChatID,
		BuildPlatform:  in.BuildPlatform,
		Skin:           upSkin(&in.Skin),
		Teacher:        in.Teacher,
		Host:           in.Host,
		SubClient:      in.SubClient,
		PlayerColour:   in.PlayerColour,
	}
}

// downPlayerListEntry converts an entry of the player list back: the
// PlayFab ID of the entry goes in the skin, or, if the entry has none, the
// one the skin of the model keeps.
func downPlayerListEntry(in *v2225.PlayerListEntry) v2193.PlayerListEntry {
	out := v2193.PlayerListEntry{
		ActionType:     in.ActionType,
		UUID:           in.UUID,
		EntityUniqueID: in.EntityUniqueID,
		Username:       in.Username,
		XUID:           in.XUID,
		PlatformChatID: in.PlatformChatID,
		BuildPlatform:  in.BuildPlatform,
		Skin:           downSkin(&in.Skin),
		Teacher:        in.Teacher,
		Host:           in.Host,
		SubClient:      in.SubClient,
		PlayerColour:   in.PlayerColour,
	}
	if in.PlayFabID != "" {
		out.Skin.PlayFabID = in.PlayFabID
	}
	return out
}

// upSkin converts a skin, which has no PlayFab ID on the wire of 2225: it
// keeps that of 2193 in the field the codec of 2225 leaves out.
func upSkin(in *v2193.Skin) v2225.Skin {
	out := v2225.Skin{
		PlayFabID:                 in.PlayFabID,
		SkinID:                    in.SkinID,
		SkinResourcePatch:         in.SkinResourcePatch,
		SkinImageWidth:            in.SkinImageWidth,
		SkinImageHeight:           in.SkinImageHeight,
		SkinData:                  in.SkinData,
		CapeImageWidth:            in.CapeImageWidth,
		CapeImageHeight:           in.CapeImageHeight,
		CapeData:                  in.CapeData,
		SkinGeometry:              in.SkinGeometry,
		GeometryDataEngineVersion: in.GeometryDataEngineVersion,
		AnimationData:             in.AnimationData,
		CapeID:                    in.CapeID,
		FullID:                    in.FullID,
		ArmSize:                   in.ArmSize,
		SkinColour:                in.SkinColour,
		PremiumSkin:               in.PremiumSkin,
		PersonaSkin:               in.PersonaSkin,
		PersonaCapeOnClassicSkin:  in.PersonaCapeOnClassicSkin,
		PrimaryUser:               in.PrimaryUser,
		OverrideAppearance:        in.OverrideAppearance,
		Trusted:                   in.Trusted,
		ProfileHash:               in.ProfileHash,
	}
	if in.Animations != nil {
		out.Animations = make([]v2225.SkinAnimation, len(in.Animations))
		for i := range in.Animations {
			out.Animations[i] = upSkinAnimation(&in.Animations[i])
		}
	}
	if in.PersonaPieces != nil {
		out.PersonaPieces = make([]v2225.PersonaPiece, len(in.PersonaPieces))
		for i := range in.PersonaPieces {
			out.PersonaPieces[i] = upPersonaPiece(&in.PersonaPieces[i])
		}
	}
	if in.PieceTintColours != nil {
		out.PieceTintColours = make([]v2225.PersonaPieceTintColour, len(in.PieceTintColours))
		for i := range in.PieceTintColours {
			out.PieceTintColours[i] = v2225.PersonaPieceTintColour(in.PieceTintColours[i])
		}
	}
	return out
}

// downSkin converts a skin back, with the PlayFab ID upSkin kept: that of
// a player list entry is the entry's.
func downSkin(in *v2225.Skin) v2193.Skin {
	out := v2193.Skin{
		PlayFabID:                 in.PlayFabID,
		SkinID:                    in.SkinID,
		SkinResourcePatch:         in.SkinResourcePatch,
		SkinImageWidth:            in.SkinImageWidth,
		SkinImageHeight:           in.SkinImageHeight,
		SkinData:                  in.SkinData,
		CapeImageWidth:            in.CapeImageWidth,
		CapeImageHeight:           in.CapeImageHeight,
		CapeData:                  in.CapeData,
		SkinGeometry:              in.SkinGeometry,
		GeometryDataEngineVersion: in.GeometryDataEngineVersion,
		AnimationData:             in.AnimationData,
		CapeID:                    in.CapeID,
		FullID:                    in.FullID,
		ArmSize:                   in.ArmSize,
		SkinColour:                in.SkinColour,
		PremiumSkin:               in.PremiumSkin,
		PersonaSkin:               in.PersonaSkin,
		PersonaCapeOnClassicSkin:  in.PersonaCapeOnClassicSkin,
		PrimaryUser:               in.PrimaryUser,
		OverrideAppearance:        in.OverrideAppearance,
		Trusted:                   in.Trusted,
		ProfileHash:               in.ProfileHash,
	}
	if in.Animations != nil {
		out.Animations = make([]v2193.SkinAnimation, len(in.Animations))
		for i := range in.Animations {
			out.Animations[i] = downSkinAnimation(&in.Animations[i])
		}
	}
	if in.PersonaPieces != nil {
		out.PersonaPieces = make([]v2193.PersonaPiece, len(in.PersonaPieces))
		for i := range in.PersonaPieces {
			out.PersonaPieces[i] = downPersonaPiece(&in.PersonaPieces[i])
		}
	}
	if in.PieceTintColours != nil {
		out.PieceTintColours = make([]v2193.PersonaPieceTintColour, len(in.PieceTintColours))
		for i := range in.PieceTintColours {
			out.PieceTintColours[i] = v2193.PersonaPieceTintColour(in.PieceTintColours[i])
		}
	}
	return out
}
