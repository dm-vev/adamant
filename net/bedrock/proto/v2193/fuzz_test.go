package v2193

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// FuzzDecode decodes hostile bytes as every packet: a decoder may fail but
// never panic, and what it decodes encodes to bytes that come back the
// same through another round.
func FuzzDecode(f *testing.F) {
	f.Add(uint16(13), []byte{2, 4, 3, 'p', 'i', 'g'})
	f.Add(uint16(5), []byte{1, 0, 2, 'h', 'i', 0})
	f.Fuzz(func(t *testing.T, id uint16, b []byte) {
		pk := New(uint32(id) & 0x3ff)
		if pk == nil {
			return
		}
		r := wire.NewReader(b)
		pk.Decode(r)
		if r.Err() != nil {
			return
		}
		var w wire.Writer
		pk.Encode(&w)
		again := New(pk.ID())
		r = wire.NewReader(w.B)
		again.Decode(r)
		if r.Err() != nil || r.Len() != 0 {
			t.Fatalf("packet %d: what it decoded does not decode again: %v, %d bytes left", pk.ID(), r.Err(), r.Len())
		}
		var w2 wire.Writer
		again.Encode(&w2)
		if !bytes.Equal(w2.B, w.B) {
			t.Fatalf("packet %d: encodes differently once decoded again\nfirst % x\nthen  % x", pk.ID(), w.B, w2.B)
		}
	})
}

func TestEveryPacketDecodesEmpty(t *testing.T) {
	for id := uint32(0); id < 1024; id++ {
		if pk := New(id); pk != nil {
			r := wire.NewReader(nil)
			pk.Decode(r)
		}
	}
}

// TestListsAreBounded decodes a packet of a list as long as MaxList and
// one of a longer list, which fails as a server reads what clients send.
func TestListsAreBounded(t *testing.T) {
	encode := func(n int) []byte {
		pk := &InventoryContentPacket{WindowID: 1, Content: make([]ItemInstance, n)}
		var w wire.Writer
		pk.Encode(&w)
		return w.B
	}
	for n, want := range map[int]error{1024: nil, 1025: wire.ErrTooLarge} {
		r := wire.NewReader(encode(n))
		r.MaxList = 1024
		var pk InventoryContentPacket
		pk.Decode(r)
		if !errors.Is(r.Err(), want) {
			t.Fatalf("%d items: %v, want %v", n, r.Err(), want)
		}
		// Without a bound, a list takes what the bytes hold.
		r = wire.NewReader(encode(n))
		pk.Decode(r)
		if r.Err() != nil || len(pk.Content) != n {
			t.Fatalf("%d items without a bound: %v, %d decoded", n, r.Err(), len(pk.Content))
		}
	}
}

// TestNestingIsBounded decodes data store values, which hold values of
// their own type, nested to the bound and past it.
func TestNestingIsBounded(t *testing.T) {
	nested := func(lists int) []byte {
		var w wire.Writer
		w.Varuint32(1) // One update,
		w.Varuint32(1) // a change of a property.
		w.String("store")
		w.String("property")
		w.Uint32(0)
		for range lists {
			w.Int32(5) // A list of one value,
			w.Varuint32(1)
		}
		w.Int32(0) // ending in no value.
		return w.B
	}
	for _, c := range []struct {
		lists int
		ok    bool
	}{{wire.MaxDepth - 1, true}, {wire.MaxDepth, false}, {wire.MaxDepth * 100, false}} {
		b := nested(c.lists)
		var pk ClientBoundDataStorePacket
		r := wire.NewReader(b)
		pk.Decode(r)
		if c.ok {
			var w wire.Writer
			pk.Encode(&w)
			if r.Err() != nil || r.Len() != 0 || !bytes.Equal(w.B, b) {
				t.Errorf("%d nested lists: %v, %d bytes left", c.lists, r.Err(), r.Len())
			}
		} else if !errors.Is(r.Err(), wire.ErrDepth) {
			t.Errorf("%d nested lists: error %v, want %v", c.lists, r.Err(), wire.ErrDepth)
		}
	}
}

type blockPos [3]int32
type vec3 [3]float32

// TestMetadataOfNamedTypes writes metadata holding a position and a vector
// of named types, as a server built on gophertunnel holds them.
func TestMetadataOfNamedTypes(t *testing.T) {
	m := Metadata{1: blockPos{1, -2, 3}, 2: vec3{0.5, 1, 2}}
	var w wire.Writer
	writeMetadata(&w, &m)
	var back Metadata
	r := wire.NewReader(w.B)
	readMetadata(r, &back)
	if r.Err() != nil || back[1] != [3]int32{1, -2, 3} || back[2] != [3]float32{0.5, 1, 2} {
		t.Fatalf("read back %v, %v", back, r.Err())
	}
}

// decodeBounded decodes the bytes of a packet of an ID as a server decodes
// what a client sends: lists of up to 1024 and the memory budget passed.
func decodeBounded(id uint32, b []byte, budget int) (proto.Packet, error) {
	pk := New(id)
	r := wire.NewReader(b)
	r.MaxList, r.MaxAlloc = 1024, budget
	pk.Decode(r)
	if r.Err() == nil && r.Len() != 0 {
		return pk, fmt.Errorf("%d bytes left", r.Len())
	}
	return pk, r.Err()
}

// encode returns the bytes of a packet.
func encode(pk proto.Packet) []byte {
	var w wire.Writer
	pk.Encode(&w)
	return w.B
}

// mobEquipment returns the bytes of a MobEquipment of an item of the user
// data passed.
func mobEquipment(data []byte) []byte {
	var w wire.Writer
	w.Varuint64(0) // entity
	w.Int16(1)     // network ID
	w.Uint16(1)    // count
	w.Varuint32(0) // metadata
	w.Bool(false)  // no stack network ID
	w.Varuint32(0) // block runtime ID
	w.ByteSlice(data)
	w.Bytes([]byte{0, 0, 0}) // slots and window
	return w.B
}

// TestItemData decodes the user data of items within the limits of their
// packet: NBT, the blocks they may be placed on and break, and the tick a
// shield started blocking at. Data past the limits or malformed fails.
func TestItemData(t *testing.T) {
	SetShieldID(7)
	defer SetShieldID(0)
	stack := ItemStack{
		NetworkID: 7, Count: 1, NBTData: map[string]any{"Damage": int32(3)},
		CanBePlacedOn: []string{"minecraft:stone"}, CanBreak: []string{"minecraft:dirt", ""}, BlockingTick: 42,
	}
	pk, err := decodeBounded(IDMobEquipment, encode(&MobEquipmentPacket{NewItem: ItemInstance{Stack: stack}}), 1<<20)
	if err != nil || !reflect.DeepEqual(pk.(*MobEquipmentPacket).NewItem.Stack, stack) {
		t.Fatalf("read back %+v, %v", pk.(*MobEquipmentPacket).NewItem.Stack, err)
	}
	canBreak := func(n int) []byte {
		return encode(&MobEquipmentPacket{NewItem: ItemInstance{Stack: ItemStack{NetworkID: 1, CanBreak: make([]string, n)}}})
	}
	// The strings are charged to the budget, 16 bytes each.
	if _, err := decodeBounded(IDMobEquipment, canBreak(1000), 16000); err != nil {
		t.Fatalf("1000 strings in a budget of 16000: %v", err)
	}
	if _, err := decodeBounded(IDMobEquipment, canBreak(1000), 15999); !errors.Is(err, wire.ErrAlloc) {
		t.Fatalf("1000 strings in a budget of 15999: %v", err)
	}
	if _, err := decodeBounded(IDMobEquipment, canBreak(1025), 1<<20); !errors.Is(err, wire.ErrTooLarge) {
		t.Fatalf("1025 strings: %v", err)
	}
	for name, c := range map[string]struct {
		data []byte
		nbt  bool
		ok   bool
	}{
		"no NBT":              {[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false, true},
		"NBT of version 1":    {[]byte{0xff, 0xff, 1, 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, true, true},
		"NBT without version": {[]byte{5, 0, 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, true, true},
		"NBT of version 2":    {[]byte{0xff, 0xff, 2, 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false, false},
		"NBT of a list root":  {[]byte{0xff, 0xff, 1, 9, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false, false},
		"string of length -1": {[]byte{0, 0, 1, 0, 0, 0, 0xff, 0xff}, false, false},
		"bytes after":         {[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 9}, false, false},
	} {
		pk, err := decodeBounded(IDMobEquipment, mobEquipment(c.data), 1<<20)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if data := pk.(*MobEquipmentPacket).NewItem.Stack.NBTData; (data != nil) != c.nbt {
			t.Errorf("%s: NBT %v", name, data)
		}
	}
}

// TestStackRequestActions decodes the actions of an item stack request,
// each a struct behind an interface that is charged to the budget.
func TestStackRequestActions(t *testing.T) {
	pk := &ItemStackRequestPacket{Requests: []ItemStackRequest{{RequestID: -3, Actions: []StackRequestAction{
		&TakeStackRequestAction{Count: 2}, &CreateStackRequestAction{ResultsSlot: 1},
		&CraftCreativeStackRequestAction{CreativeItemNetworkID: 9, NumberOfCrafts: 1},
	}, FilterStrings: []string{"a"}}}}
	back, err := decodeBounded(IDItemStackRequest, encode(pk), 1<<20)
	if err != nil || !reflect.DeepEqual(back, pk) {
		t.Fatalf("read back %+v, %v", back, err)
	}
	actions := make([]StackRequestAction, 1000)
	for i := range actions {
		actions[i] = &CreateStackRequestAction{}
	}
	b := encode(&ItemStackRequestPacket{Requests: []ItemStackRequest{{Actions: actions}}})
	budget := int(unsafe.Sizeof(ItemStackRequest{})) + 1000*int(unsafe.Sizeof(actions[0])+unsafe.Sizeof(CreateStackRequestAction{}))
	if _, err := decodeBounded(IDItemStackRequest, b, budget); err != nil {
		t.Fatalf("1000 actions in a budget of %d: %v", budget, err)
	}
	if _, err := decodeBounded(IDItemStackRequest, b, budget-1); !errors.Is(err, wire.ErrAlloc) {
		t.Fatalf("1000 actions in a budget of %d: %v", budget-1, err)
	}
}

// TestListsOfStrings decodes the lists of strings of text, pack settings
// and resource pack responses, within MaxList and past it.
func TestListsOfStrings(t *testing.T) {
	long := make([]string, 1025)
	for _, c := range []struct {
		pk, past proto.Packet
	}{
		{&TextPacket{TextType: TextTypeTranslation, Message: "m", Parameters: []string{"a", "b"}, XUID: "1"}, &TextPacket{TextType: TextTypePopup, Parameters: long}},
		{&TextPacket{TextType: TextTypeChat, SourceName: "s", Message: "m"}, nil},
		{&TextPacket{TextType: TextTypeRaw, Message: "m", FilteredMessage: proto.Some("f")}, nil},
		{&ServerBoundPackSettingChangePacket{PackSetting: PackSetting{Name: "n", Value: []string{"a", "b"}}}, &ServerBoundPackSettingChangePacket{PackSetting: PackSetting{Value: long}}},
		{&ServerBoundPackSettingChangePacket{PackSetting: PackSetting{Name: "n", Value: "v"}}, nil},
		{&ResourcePackClientResponsePacket{Response: packResponseSendPacks, PacksToDownload: []string{"a"}}, &ResourcePackClientResponsePacket{Response: packResponseSendPacks, PacksToDownload: long}},
	} {
		back, err := decodeBounded(c.pk.ID(), encode(c.pk), 1<<20)
		if err != nil || !reflect.DeepEqual(back, c.pk) {
			t.Errorf("%T: read back %+v, %v", c.pk, back, err)
		}
		if c.past == nil {
			continue
		}
		if _, err := decodeBounded(c.past.ID(), encode(c.past), 1<<20); !errors.Is(err, wire.ErrTooLarge) {
			t.Errorf("%T of 1025 strings: %v", c.past, err)
		}
	}
}

// TestItemDescriptors codes the items of recipes of each kind, and fails
// for a kind of no name, naming it short.
func TestItemDescriptors(t *testing.T) {
	for _, x := range []ItemDescriptorCount{
		{Descriptor: &InvalidItemDescriptor{}, Count: 1},
		{Descriptor: &DefaultItemDescriptor{Name: "minecraft:stick", MetadataValue: 2}, Count: 3},
		{Descriptor: &MoLangItemDescriptor{Expression: "q.any_tag('a')", Version: 10}, Count: 4},
		{Descriptor: &ItemTagItemDescriptor{Tag: "minecraft:planks"}, Count: 5},
	} {
		var w wire.Writer
		writeItemDescriptorCount(&w, &x)
		var back ItemDescriptorCount
		r := wire.NewReader(w.B)
		readItemDescriptorCount(r, &back)
		if r.Err() != nil || r.Len() != 0 || !reflect.DeepEqual(back, x) {
			t.Errorf("%T: read back %+v, %v", x.Descriptor, back, r.Err())
		}
	}
	var w wire.Writer
	w.Varuint32(1)
	w.String(strings.Repeat("x", 1<<20))
	r := wire.NewReader(w.B)
	readItemDescriptorCount(r, new(ItemDescriptorCount))
	if r.Err() == nil || len(r.Err().Error()) > 512 {
		t.Errorf("an item descriptor of a name of 1 MiB failed with %d bytes", len(fmt.Sprint(r.Err())))
	}
	r = wire.NewReader([]byte{2})
	if readItemDescriptorCount(r, new(ItemDescriptorCount)); r.Err() == nil {
		t.Error("read an item descriptor of variant 2")
	}
}
