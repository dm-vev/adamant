package session

import (
	"bytes"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
	"unsafe"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// TestConvertMatchesGophertunnel converts random packets of gophertunnel,
// as its decoder reads them, into those of the generated codecs: their
// encoding must be gophertunnel's, byte for byte.
func TestConvertMatchesGophertunnel(t *testing.T) {
	pool := packet.NewClientPool()
	for id, f := range packet.NewServerPool() {
		pool[id] = f
	}
	ids := make([]uint32, 0, len(pool))
	for id := range pool {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	rng := rand.New(rand.NewPCG(7, 8))
	checked := 0
	for _, id := range ids {
		tested := 0
		for try := 0; try < 200 && tested < 20; try++ {
			pk := pool[id]()
			gtFill(rng, reflect.ValueOf(pk).Elem(), 0)
			gtCanonical(rng, pk)
			b, ok := gtMarshal(pk)
			if !ok {
				continue
			}
			back := pool[id]()
			if !gtUnmarshal(back, b) {
				continue
			}
			want, ok := gtMarshal(back)
			if !ok {
				continue
			}
			tested++
			ours := v2193.New(id)
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%T: %v", back, r)
					}
				}()
				convertValue(reflect.ValueOf(ours).Elem(), reflect.ValueOf(back).Elem())
			}()
			var w wire.Writer
			ours.Encode(&w)
			if !bytes.Equal(w.B, want) {
				t.Fatalf("%T converts to bytes of its own\ngophertunnel % x\nours         % x", back, want[:min(len(want), 64)], w.B[:min(len(w.B), 64)])
			}
		}
		if tested > 0 {
			checked++
		}
	}
	t.Logf("%d of %d packets checked", checked, len(ids))
}

// gtImplementations lists values of the interfaces packets hold, so that
// random packets carry each kind.
var gtImplementations = map[reflect.Type][]func() any{
	reflect.TypeFor[protocol.ItemDescriptor](): {
		func() any { return &protocol.InvalidItemDescriptor{} },
		func() any { return &protocol.DefaultItemDescriptor{} },
		func() any { return &protocol.MoLangItemDescriptor{} },
		func() any { return &protocol.ItemTagItemDescriptor{} },
	},
	reflect.TypeFor[protocol.StackRequestAction](): {
		func() any { return &protocol.TakeStackRequestAction{} },
		func() any { return &protocol.PlaceStackRequestAction{} },
		func() any { return &protocol.SwapStackRequestAction{} },
		func() any { return &protocol.DropStackRequestAction{} },
		func() any { return &protocol.DestroyStackRequestAction{} },
		func() any { return &protocol.ConsumeStackRequestAction{} },
		func() any { return &protocol.CreateStackRequestAction{} },
		func() any { return &protocol.LabTableCombineStackRequestAction{} },
		func() any { return &protocol.BeaconPaymentStackRequestAction{} },
		func() any { return &protocol.MineBlockStackRequestAction{} },
		func() any { return &protocol.CraftRecipeStackRequestAction{} },
		func() any { return &protocol.AutoCraftRecipeStackRequestAction{} },
		func() any { return &protocol.CraftCreativeStackRequestAction{} },
		func() any { return &protocol.CraftRecipeOptionalStackRequestAction{} },
		func() any { return &protocol.CraftGrindstoneRecipeStackRequestAction{} },
		func() any { return &protocol.CraftLoomRecipeStackRequestAction{} },
		func() any { return &protocol.CraftNonImplementedStackRequestAction{} },
		func() any { return &protocol.CraftResultsDeprecatedStackRequestAction{} },
	},
	reflect.TypeFor[protocol.InventoryTransactionData](): {
		func() any { return &protocol.NormalTransactionData{} },
		func() any { return &protocol.MismatchTransactionData{} },
		func() any { return &protocol.UseItemTransactionData{} },
		func() any { return &protocol.UseItemOnEntityTransactionData{} },
		func() any { return &protocol.ReleaseItemTransactionData{} },
	},
	// Only the events the game still reads.
	reflect.TypeFor[protocol.Event](): {
		func() any { return &protocol.AchievementAwardedEvent{} },
		func() any { return &protocol.EntityInteractEvent{} },
		func() any { return &protocol.PortalBuiltEvent{} },
		func() any { return &protocol.PortalUsedEvent{} },
		func() any { return &protocol.MobKilledEvent{} },
		func() any { return &protocol.CauldronUsedEvent{} },
		func() any { return &protocol.PlayerDiedEvent{} },
		func() any { return &protocol.BossKilledEvent{} },
		func() any { return &protocol.SlashCommandExecutedEvent{} },
		func() any { return &protocol.MobBornEvent{} },
		func() any { return &protocol.CauldronInteractEvent{} },
		func() any { return &protocol.ComposterInteractEvent{} },
		func() any { return &protocol.BellUsedEvent{} },
		func() any { return &protocol.EntityDefinitionTriggerEvent{} },
		func() any { return &protocol.RaidUpdateEvent{} },
		func() any { return &protocol.TargetBlockHitEvent{} },
		func() any { return &protocol.PiglinBarterEvent{} },
		func() any { return &protocol.WaxedOrUnwaxedCopperEvent{} },
		func() any { return &protocol.CodeBuilderRuntimeActionEvent{} },
		func() any { return &protocol.CodeBuilderScoreboardEvent{} },
		func() any { return &protocol.ItemUsedEvent{} },
		func() any { return &protocol.StriderRiddenInLavaInOverworldEvent{} },
		func() any { return &protocol.SneakCloseToSculkSensorEvent{} },
		func() any { return &protocol.CarefulRestorationEvent{} },
	},
	reflect.TypeFor[protocol.ShapeData](): {
		func() any { return &protocol.LastShape{} },
		func() any { return &protocol.ArrowShape{} },
		func() any { return &protocol.TextShape{} },
		func() any { return &protocol.BoxShape{} },
		func() any { return &protocol.LineShape{} },
		func() any { return &protocol.SphereShape{} },
		func() any { return &protocol.CylinderShape{} },
		func() any { return &protocol.PyramidShape{} },
		func() any { return &protocol.EllipsoidShape{} },
		func() any { return &protocol.ConeShape{} },
	},
}

// fill sets a value to random contents: small numbers often, so that enum
// branches are taken, short strings and lists, NBT of one key.
func gtFill(rng *rand.Rand, v reflect.Value, depth int) {
	if !v.CanSet() {
		v = reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
	}
	small := rng.IntN(2) == 0
	if v.Type() == reflect.TypeFor[protocol.GameRule]() {
		values := []any{nil, rng.IntN(2) == 0, uint32(rng.IntN(100)), float32(rng.IntN(100)) / 4}
		v.Set(reflect.ValueOf(protocol.GameRule{Name: "rule", CanBeModifiedByPlayer: small, Value: values[rng.IntN(len(values))]}))
		return
	}
	if v.Type() == reflect.TypeFor[protocol.PackSetting]() {
		values := []any{float32(rng.IntN(100)) / 4, small, "value", []string{"a", "bc"}}
		v.Set(reflect.ValueOf(protocol.PackSetting{Name: "setting", Value: values[rng.IntN(len(values))]}))
		return
	}
	if v.Type() == reflect.TypeFor[protocol.Bitset]() {
		// The flags of an entity: the only bitset packets hold.
		b := protocol.NewBitset(protocol.EntityDataFlagCount)
		for range rng.IntN(4) {
			b.Set(rng.IntN(protocol.EntityDataFlagCount))
		}
		v.Set(reflect.ValueOf(b))
		return
	}
	if impls, ok := gtImplementations[v.Type()]; ok && depth < 6 {
		x := reflect.ValueOf(impls[rng.IntN(len(impls))]())
		gtFill(rng, x.Elem(), depth+1)
		v.Set(x)
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(rng.IntN(2) == 0)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if small {
			v.SetInt(int64(rng.IntN(12)))
		} else {
			bits := v.Type().Bits()
			v.SetInt(int64(rng.Uint64()) >> (64 - bits))
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if small {
			v.SetUint(uint64(rng.IntN(12)))
		} else {
			v.SetUint(rng.Uint64() >> (64 - v.Type().Bits()))
		}
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(rng.IntN(2000)-1000) / 8)
	case reflect.String:
		b := make([]byte, rng.IntN(6))
		for i := range b {
			b[i] = byte('a' + rng.IntN(26))
		}
		v.SetString(string(b))
	case reflect.Slice:
		if depth > 5 {
			return
		}
		n := rng.IntN(3)
		s := reflect.MakeSlice(v.Type(), n, n)
		for i := range n {
			gtFill(rng, s.Index(i), depth+1)
		}
		v.Set(s)
	case reflect.Array:
		for i := range v.Len() {
			gtFill(rng, v.Index(i), depth+1)
		}
	case reflect.Map:
		if v.Type().Key().Kind() == reflect.Uint32 && v.Type().Elem().Kind() == reflect.Interface {
			// Entity metadata: a value of each type it holds.
			m := reflect.MakeMap(v.Type())
			values := []any{byte(rng.IntN(256)), int16(rng.IntN(1000)), int32(rng.IntN(1000) - 500),
				float32(rng.IntN(100)) / 4, "name", map[string]any{"k": int32(7)},
				protocol.BlockPos{1, -2, 3}, int64(rng.Uint64() >> 1), mgl32.Vec3{1, 2, 3}}
			for range rng.IntN(4) {
				m.SetMapIndex(reflect.ValueOf(uint32(rng.IntN(130))), reflect.ValueOf(values[rng.IntN(len(values))]))
			}
			v.Set(m)
			return
		}
		if v.Type().Key().Kind() != reflect.String || v.Type().Elem().Kind() != reflect.Interface {
			return
		}
		m := reflect.MakeMap(v.Type())
		if rng.IntN(2) == 0 {
			m.SetMapIndex(reflect.ValueOf("k"), reflect.ValueOf(int32(rng.IntN(100))))
		}
		v.Set(m)
	case reflect.Struct:
		for i := range v.NumField() {
			gtFill(rng, v.Field(i), depth+1)
		}
		if skin, ok := v.Addr().Interface().(*protocol.Skin); ok {
			gtSizeImages(rng, skin)
		}
	case reflect.Pointer:
		if depth > 5 {
			return
		}
		p := reflect.New(v.Type().Elem())
		gtFill(rng, p.Elem(), depth+1)
		v.Set(p)
	}
}

// sizeImages gives the images of a skin the sizes their dimensions call
// for, which gophertunnel checks.
func gtSizeImages(rng *rand.Rand, skin *protocol.Skin) {
	image := func(width, height *uint32, data *[]byte) {
		*width, *height = uint32(rng.IntN(3)), uint32(rng.IntN(3))
		*data = make([]byte, *width**height*4)
		for i := range *data {
			(*data)[i] = byte(rng.IntN(256))
		}
	}
	image(&skin.SkinImageWidth, &skin.SkinImageHeight, &skin.SkinData)
	image(&skin.CapeImageWidth, &skin.CapeImageHeight, &skin.CapeData)
	for i := range skin.Animations {
		a := &skin.Animations[i]
		image(&a.ImageWidth, &a.ImageHeight, &a.ImageData)
	}
}

// marshal encodes a packet with gophertunnel, reporting a value it refuses.
func gtMarshal(pk packet.Packet) (b []byte, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	var buf bytes.Buffer
	pk.Marshal(protocol.NewWriter(&buf, 0))
	return buf.Bytes(), true
}

// canonical brings the fields of a packet that the wire holds in fewer
// bits than gophertunnel into the values the wire can hold.
func gtCanonical(rng *rand.Rand, pk packet.Packet) {
	switch pk := pk.(type) {
	case *packet.RequestAbility:
		// The value of an ability is a bool or a float32.
		pk.Value = []any{rng.IntN(2) == 0, float32(rng.IntN(100)) / 4}[rng.IntN(2)]
	case *packet.PlayerList:
		// The action goes as a variant, which decides, and again as a byte.
		for i := range pk.Entries {
			pk.Entries[i].ActionType &= 1
		}
	case *packet.ClientboundUpdateSoundData:
		// Seven updates, each of seven types, are rarely all valid.
		for _, u := range []*protocol.SoundDataUpdate{&pk.Stop, &pk.SetVolume, &pk.SetPitch, &pk.Fade, &pk.SeekTo, &pk.Pause, &pk.Resume} {
			u.Type %= 7
		}
	case *packet.CraftingData:
		// The input of a material reducer goes as one varint: the network
		// ID in the high 16 bits, the metadata in the low 15.
		for i := range pk.MaterialReducers {
			in := &pk.MaterialReducers[i].InputItem
			in.NetworkID = int32(int16(in.NetworkID))
			in.MetadataValue &= 0x7fff
		}
	}
}

// unmarshal decodes a packet with gophertunnel.
func gtUnmarshal(pk packet.Packet, b []byte) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	pk.Marshal(protocol.NewReader(bytes.NewBuffer(b), 0, false))
	return true
}

// encodePacket encodes a packet of the generated codecs.
func encodePacket(pk interface{ Encode(*wire.Writer) }) []byte {
	var w wire.Writer
	pk.Encode(&w)
	return w.B
}

// TestHandConversionsMatch fills values of the types converted by hand on
// hot paths at random: each must convert as the conversion by name, which
// TestConvertMatchesGophertunnel checks against gophertunnel's encoding.
func TestHandConversionsMatch(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 10))
	random := func(v any) {
		gtFill(rng, reflect.ValueOf(v).Elem(), 0)
	}
	check := func(name string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: by hand %#v\nby name %#v", name, got, want)
		}
	}
	for range 50 {
		var item protocol.ItemInstance
		random(&item)
		check("item", toItemInstance(item), to[v2193.ItemInstance](item))
		var links []protocol.EntityLink
		random(&links)
		check("links", toEntityLinks(links), to[[]v2193.EntityLink](links))
		var abilities protocol.AbilityData
		random(&abilities)
		check("abilities", toAbilityData(abilities), to[v2193.AbilityData](abilities))
		var values []protocol.AttributeValue
		random(&values)
		check("attribute values", toAttributeValues(values), to[[]v2193.AttributeValue](values))
		var attributes []protocol.Attribute
		random(&attributes)
		check("attributes", toAttributes(attributes), to[[]v2193.Attribute](attributes))
		var properties protocol.EntityProperties
		random(&properties)
		check("properties", toEntityProperties(properties), to[v2193.EntityProperties](properties))
		var entries []protocol.PlayerListEntry
		random(&entries)
		check("player list", toPlayerListEntries(entries), to[[]v2193.PlayerListEntry](entries))
		var subChunks []protocol.SubChunkEntry
		random(&subChunks)
		check("sub-chunks", toSubChunkEntries(subChunks), to[[]v2193.SubChunkEntry](subChunks))
		var origin protocol.CommandOrigin
		random(&origin)
		check("command origin", toCommandOrigin(origin), to[v2193.CommandOrigin](origin))
	}
}
