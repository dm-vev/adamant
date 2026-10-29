package session

import (
	"image/color"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The functions below turn values the session still builds as
// gophertunnel's protocol types into those of the generated codecs of
// protocol 2193, field for field: what goes on the wire does not change.

func toItemStack(x protocol.ItemStack) v2193.ItemStack {
	return v2193.ItemStack{
		NetworkID:      x.NetworkID,
		MetadataValue:  x.MetadataValue,
		BlockRuntimeID: x.BlockRuntimeID,
		Count:          x.Count,
		NBTData:        x.NBTData,
		CanBePlacedOn:  x.CanBePlacedOn,
		CanBreak:       x.CanBreak,
		BlockingTick:   x.BlockingTick,
	}
}

func toItemInstance(x protocol.ItemInstance) v2193.ItemInstance {
	return v2193.ItemInstance{StackNetworkID: x.StackNetworkID, Stack: toItemStack(x.Stack)}
}

func toItemInstances(l []protocol.ItemInstance) []v2193.ItemInstance {
	out := make([]v2193.ItemInstance, len(l))
	for i, x := range l {
		out[i] = toItemInstance(x)
	}
	return out
}

func toMetadata(m protocol.EntityMetadata) v2193.Metadata { return v2193.Metadata(m) }

func toEntityLinks(l []protocol.EntityLink) []v2193.EntityLink {
	out := make([]v2193.EntityLink, len(l))
	for i, x := range l {
		out[i] = v2193.EntityLink(x)
	}
	return out
}

func toAbilityData(x protocol.AbilityData) v2193.AbilityData {
	layers := make([]v2193.AbilityLayer, len(x.Layers))
	for i, l := range x.Layers {
		layers[i] = v2193.AbilityLayer(l)
	}
	return v2193.AbilityData{
		EntityUniqueID:     x.EntityUniqueID,
		PlayerPermissions:  x.PlayerPermissions,
		CommandPermissions: x.CommandPermissions,
		Layers:             layers,
	}
}

func toAttributeValues(l []protocol.AttributeValue) []v2193.AttributeValue {
	out := make([]v2193.AttributeValue, len(l))
	for i, x := range l {
		out[i] = v2193.AttributeValue{Name: x.Name, Min: x.Min, Value: x.Value, Max: x.Max}
	}
	return out
}

func toAttributes(l []protocol.Attribute) []v2193.Attribute {
	out := make([]v2193.Attribute, len(l))
	for i, x := range l {
		modifiers := make([]v2193.AttributeModifier, len(x.Modifiers))
		for j, m := range x.Modifiers {
			modifiers[j] = v2193.AttributeModifier(m)
		}
		out[i] = v2193.Attribute{
			Min: x.Min, Max: x.Max, Value: x.Value,
			DefaultMin: x.DefaultMin, DefaultMax: x.DefaultMax, Default: x.Default,
			Name: x.Name, Modifiers: modifiers,
		}
	}
	return out
}

func toEntityProperties(x protocol.EntityProperties) v2193.EntityProperties {
	ints := make([]v2193.IntegerEntityProperty, len(x.IntegerProperties))
	for i, p := range x.IntegerProperties {
		ints[i] = v2193.IntegerEntityProperty(p)
	}
	floats := make([]v2193.FloatEntityProperty, len(x.FloatProperties))
	for i, p := range x.FloatProperties {
		floats[i] = v2193.FloatEntityProperty(p)
	}
	return v2193.EntityProperties{IntegerProperties: ints, FloatProperties: floats}
}

func toColour(c color.RGBA) proto.Color { return proto.Color{c.R, c.G, c.B, c.A} }

func toSkin(x protocol.Skin) v2193.Skin {
	animations := make([]v2193.SkinAnimation, len(x.Animations))
	for i, a := range x.Animations {
		animations[i] = v2193.SkinAnimation(a)
	}
	pieces := make([]v2193.PersonaPiece, len(x.PersonaPieces))
	for i, p := range x.PersonaPieces {
		pieces[i] = v2193.PersonaPiece{PieceID: p.PieceID, PieceType: p.PieceType, PackID: p.PackID, Default: p.Default, ProductID: p.ProductID}
	}
	tints := make([]v2193.PersonaPieceTintColour, len(x.PieceTintColours))
	for i, t := range x.PieceTintColours {
		tints[i].PieceType = t.PieceType
		for j, c := range t.Colours {
			tints[i].Colours[j] = toColour(c)
		}
	}
	return v2193.Skin{
		SkinID:                    x.SkinID,
		PlayFabID:                 x.PlayFabID,
		SkinResourcePatch:         x.SkinResourcePatch,
		SkinImageWidth:            x.SkinImageWidth,
		SkinImageHeight:           x.SkinImageHeight,
		SkinData:                  x.SkinData,
		Animations:                animations,
		CapeImageWidth:            x.CapeImageWidth,
		CapeImageHeight:           x.CapeImageHeight,
		CapeData:                  x.CapeData,
		SkinGeometry:              x.SkinGeometry,
		GeometryDataEngineVersion: x.GeometryDataEngineVersion,
		AnimationData:             x.AnimationData,
		CapeID:                    x.CapeID,
		FullID:                    x.FullID,
		ArmSize:                   x.ArmSize,
		SkinColour:                toColour(x.SkinColour),
		PersonaPieces:             pieces,
		PieceTintColours:          tints,
		PremiumSkin:               x.PremiumSkin,
		PersonaSkin:               x.PersonaSkin,
		PersonaCapeOnClassicSkin:  x.PersonaCapeOnClassicSkin,
		PrimaryUser:               x.PrimaryUser,
		OverrideAppearance:        x.OverrideAppearance,
		Trusted:                   x.Trusted,
		ProfileHash:               x.ProfileHash,
	}
}

func toPlayerListEntries(l []protocol.PlayerListEntry) []v2193.PlayerListEntry {
	out := make([]v2193.PlayerListEntry, len(l))
	for i, x := range l {
		out[i] = v2193.PlayerListEntry{
			ActionType:     x.ActionType,
			UUID:           x.UUID,
			EntityUniqueID: x.EntityUniqueID,
			Username:       x.Username,
			XUID:           x.XUID,
			PlatformChatID: x.PlatformChatID,
			BuildPlatform:  x.BuildPlatform,
			Skin:           toSkin(x.Skin),
			Teacher:        x.Teacher,
			Host:           x.Host,
			SubClient:      x.SubClient,
			PlayerColour:   toColour(x.PlayerColour),
		}
	}
	return out
}

func toSubChunkEntries(l []protocol.SubChunkEntry) []v2193.SubChunkEntry {
	out := make([]v2193.SubChunkEntry, len(l))
	for i, x := range l {
		e := v2193.SubChunkEntry{
			Offset:              v2193.SubChunkOffset(x.Offset),
			Result:              x.Result,
			HeightMapType:       x.HeightMapType,
			RenderHeightMapType: x.RenderHeightMapType,
		}
		if v, ok := x.RawPayload.Value(); ok {
			e.RawPayload = proto.Some(v)
		}
		if v, ok := x.HeightMapData.Value(); ok {
			e.HeightMapData = proto.Some(v2193.HeightMap(v))
		}
		if v, ok := x.RenderHeightMapData.Value(); ok {
			e.RenderHeightMapData = proto.Some(v2193.HeightMap(v))
		}
		if v, ok := x.BlobHash.Value(); ok {
			e.BlobHash = proto.Some(v)
		}
		out[i] = e
	}
	return out
}

func toCommandOrigin(x protocol.CommandOrigin) v2193.CommandOrigin {
	return v2193.CommandOrigin{Origin: x.Origin, UUID: x.UUID, RequestID: x.RequestID, PlayerUniqueID: x.PlayerUniqueID}
}

func toColours(l []color.RGBA) []proto.Color {
	out := make([]proto.Color, len(l))
	for i, c := range l {
		out[i] = toColour(c)
	}
	return out
}
