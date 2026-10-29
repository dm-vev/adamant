package v2193

import (
	"slices"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// GameRule is a game rule of StartGame and GameRulesChanged. Value is a
// bool, a uint32, a float32, or nil for a rule without a value.
type GameRule struct {
	Name                  string
	CanBeModifiedByPlayer bool
	Value                 any
}

func writeGameRule(w *wire.Writer, x *GameRule) {
	w.String(x.Name)
	w.Bool(x.CanBeModifiedByPlayer)
	switch v := x.Value.(type) {
	case nil:
		w.Varuint32(0)
	case bool:
		w.Varuint32(1)
		w.Bool(v)
	case uint32:
		w.Varuint32(2)
		w.Uint32(v)
	case float32:
		w.Varuint32(3)
		w.Float32(v)
	default:
		panic("v2193: game rule value of no type")
	}
}

func readGameRule(r *wire.Reader, x *GameRule) {
	x.Name = r.String()
	x.CanBeModifiedByPlayer = r.Bool()
	switch t := r.Varuint32(); t {
	case 0:
		x.Value = nil
	case 1:
		x.Value = r.Bool()
	case 2:
		x.Value = r.Uint32()
	case 3:
		x.Value = r.Float32()
	default:
		r.Failf("v2193: game rule type %d", t)
	}
}

// InputFlags holds the IDs of the flags a PlayerAuthInput sets.
type InputFlags []int32

// Has reports whether the flag of an ID is set.
func (x InputFlags) Has(id int) bool { return slices.Contains(x, int32(id)) }

func writeInputFlags(w *wire.Writer, x *InputFlags, count int) {
	w.Varuint32(uint32(len(*x)))
	for _, id := range *x {
		w.Varint32(id)
	}
}

func readInputFlags(r *wire.Reader, x *InputFlags, count int) {
	n := r.Varuint32()
	if n > uint32(count) {
		r.Failf("v2193: %d input flags", n)
		return
	}
	ids := make(InputFlags, n)
	for i := range ids {
		id := r.Varint32()
		if id < 0 || int(id) >= count || slices.Contains(ids[:i], id) {
			r.Failf("v2193: input flag %d", id)
			return
		}
		ids[i] = id
	}
	*x = ids
}

// HeightMap holds the heights of a sub-chunk by z then x, each row
// prefixed with its length of 16.
type HeightMap [16][16]int8

func (x *HeightMap) Encode(w *wire.Writer) {
	for z := range x {
		w.Varuint32(16)
		for i := range x[z] {
			w.Int8(x[z][i])
		}
	}
}

func (x *HeightMap) Decode(r *wire.Reader) {
	for z := range x {
		if n := r.Varuint32(); n != 16 {
			r.Failf("v2193: height map row of %d heights", n)
			return
		}
		for i := range x[z] {
			x[z][i] = r.Int8()
		}
	}
}

// StackRequestItem is an item of the deprecated crafting results of an
// ItemStackRequest, named by identifier.
type StackRequestItem struct {
	Identifier     string
	MetadataValue  uint32
	BlockRuntimeID int32
	Count          uint16
	NBTData        map[string]any
	CanBePlacedOn  []string
	CanBreak       []string
	BlockingTick   int64
}

func writeStackRequestItem(w *wire.Writer, x *StackRequestItem) {
	hasItem := x.Identifier != ""
	variant := uint32(0)
	if hasItem {
		variant = 1
	}
	// The descriptor type goes twice: once as the selector.
	w.Varuint32(variant)
	w.Uint8(uint8(variant))
	if hasItem {
		w.String(x.Identifier)
		w.Varint32(int32(x.MetadataValue))
	}
	w.Int16(int16(x.Count))
	w.Varuint32(uint32(x.BlockRuntimeID))
	writeItemData(w, hasItem, x.Identifier == "minecraft:shield", x.NBTData, x.CanBePlacedOn, x.CanBreak, x.BlockingTick)
}

func readStackRequestItem(r *wire.Reader, x *StackRequestItem) {
	variant := r.Varuint32()
	r.Uint8()
	if variant > 1 {
		r.Failf("v2193: stack request item descriptor %d", variant)
		return
	}
	x.Identifier, x.MetadataValue = "", 0
	if variant == 1 {
		x.Identifier = r.String()
		x.MetadataValue = uint32(r.Varint32())
	}
	x.Count = uint16(r.Int16())
	x.BlockRuntimeID = int32(r.Varuint32())
	x.NBTData, x.CanBePlacedOn, x.CanBreak, x.BlockingTick = readItemData(r, x.Identifier == "minecraft:shield")
}

// StackRequestAction is an action of an ItemStackRequest, one of the
// *StackRequestAction types.
type StackRequestAction interface {
	Encode(w *wire.Writer)
	Decode(r *wire.Reader)
}

// The IDs of the actions. Place in and take out of a container have IDs
// but no variant: the variant of the later ones is their ID less two.
const (
	actionTakeOutContainer = 8
	actionLast             = 19
)

func newStackRequestAction(id uint8) StackRequestAction {
	switch id {
	case 0:
		return &TakeStackRequestAction{}
	case 1:
		return &PlaceStackRequestAction{}
	case 2:
		return &SwapStackRequestAction{}
	case 3:
		return &DropStackRequestAction{}
	case 4:
		return &DestroyStackRequestAction{}
	case 5:
		return &ConsumeStackRequestAction{}
	case 6:
		return &CreateStackRequestAction{}
	case 9:
		return &LabTableCombineStackRequestAction{}
	case 10:
		return &BeaconPaymentStackRequestAction{}
	case 11:
		return &MineBlockStackRequestAction{}
	case 12:
		return &CraftRecipeStackRequestAction{}
	case 13:
		return &AutoCraftRecipeStackRequestAction{}
	case 14:
		return &CraftCreativeStackRequestAction{}
	case 15:
		return &CraftRecipeOptionalStackRequestAction{}
	case 16:
		return &CraftGrindstoneRecipeStackRequestAction{}
	case 17:
		return &CraftLoomRecipeStackRequestAction{}
	case 18:
		return &CraftNonImplementedStackRequestAction{}
	case 19:
		return &CraftResultsDeprecatedStackRequestAction{}
	}
	return nil
}

func stackRequestActionID(a StackRequestAction) uint8 {
	switch a.(type) {
	case *TakeStackRequestAction:
		return 0
	case *PlaceStackRequestAction:
		return 1
	case *SwapStackRequestAction:
		return 2
	case *DropStackRequestAction:
		return 3
	case *DestroyStackRequestAction:
		return 4
	case *ConsumeStackRequestAction:
		return 5
	case *CreateStackRequestAction:
		return 6
	case *LabTableCombineStackRequestAction:
		return 9
	case *BeaconPaymentStackRequestAction:
		return 10
	case *MineBlockStackRequestAction:
		return 11
	case *CraftRecipeStackRequestAction:
		return 12
	case *AutoCraftRecipeStackRequestAction:
		return 13
	case *CraftCreativeStackRequestAction:
		return 14
	case *CraftRecipeOptionalStackRequestAction:
		return 15
	case *CraftGrindstoneRecipeStackRequestAction:
		return 16
	case *CraftLoomRecipeStackRequestAction:
		return 17
	case *CraftNonImplementedStackRequestAction:
		return 18
	case *CraftResultsDeprecatedStackRequestAction:
		return 19
	}
	panic("v2193: stack request action of no type")
}

func writeStackRequestAction(w *wire.Writer, x *StackRequestAction) {
	id := stackRequestActionID(*x)
	variant := uint32(id)
	if id > actionTakeOutContainer {
		variant -= 2
	}
	w.Varuint32(variant)
	w.Uint8(id)
	(*x).Encode(w)
}

func readStackRequestAction(r *wire.Reader, x *StackRequestAction) {
	variant := r.Varuint32()
	r.Uint8()
	if variant > actionLast-2 {
		r.Failf("v2193: stack request action variant %d", variant)
		return
	}
	id := uint8(variant)
	if variant >= actionTakeOutContainer-1 {
		id += 2
	}
	a := newStackRequestAction(id)
	a.Decode(r)
	*x = a
}

// TextPacket carries a message of chat, a popup, a tip or a translation.
type TextPacket struct {
	TextType         uint8
	NeedsTranslation bool
	SourceName       string
	Message          string
	Parameters       []string
	XUID             string
	PlatformChatID   string
	FilteredMessage  proto.Optional[string]
}

func (*TextPacket) ID() uint32 { return 9 }

// Types of text.
const (
	TextTypeRaw uint8 = iota
	TextTypeChat
	TextTypeTranslation
	TextTypePopup
	TextTypeJukeboxPopup
	TextTypeTip
	TextTypeSystem
	TextTypeWhisper
	TextTypeAnnouncement
	TextTypeObjectWhisper
	TextTypeObject
	TextTypeObjectAnnouncement
)

// textCategory returns the category a type of text is sent as.
func textCategory(t uint8) uint8 {
	switch t {
	case TextTypeRaw, TextTypeTip, TextTypeSystem, TextTypeObjectWhisper, TextTypeObjectAnnouncement, TextTypeObject:
		return 0
	case TextTypeChat, TextTypeWhisper, TextTypeAnnouncement:
		return 1
	}
	return 2
}

func (p *TextPacket) Encode(w *wire.Writer) {
	w.Bool(p.NeedsTranslation)
	w.Uint8(textCategory(p.TextType))
	w.Uint8(p.TextType)
	switch p.TextType {
	case TextTypeChat, TextTypeWhisper, TextTypeAnnouncement:
		w.String(p.SourceName)
		w.String(p.Message)
	case TextTypeRaw, TextTypeTip, TextTypeSystem, TextTypeObject, TextTypeObjectWhisper, TextTypeObjectAnnouncement:
		w.String(p.Message)
	case TextTypeTranslation, TextTypePopup, TextTypeJukeboxPopup:
		w.String(p.Message)
		w.Varuint32(uint32(len(p.Parameters)))
		for _, s := range p.Parameters {
			w.String(s)
		}
	}
	w.String(p.XUID)
	w.String(p.PlatformChatID)
	w.Bool(p.FilteredMessage.Set)
	if p.FilteredMessage.Set {
		w.String(p.FilteredMessage.Val)
	}
}

func (p *TextPacket) Decode(r *wire.Reader) {
	p.NeedsTranslation = r.Bool()
	// The category follows from the type.
	r.Uint8()
	p.TextType = r.Uint8()
	switch p.TextType {
	case TextTypeChat, TextTypeWhisper, TextTypeAnnouncement:
		p.SourceName = r.String()
		p.Message = r.String()
	case TextTypeRaw, TextTypeTip, TextTypeSystem, TextTypeObject, TextTypeObjectWhisper, TextTypeObjectAnnouncement:
		p.Message = r.String()
	case TextTypeTranslation, TextTypePopup, TextTypeJukeboxPopup:
		p.Message = r.String()
		n := r.Count(r.Len())
		p.Parameters = make([]string, n)
		for i := range p.Parameters {
			p.Parameters[i] = r.String()
		}
	}
	p.XUID = r.String()
	p.PlatformChatID = r.String()
	if p.FilteredMessage.Set = r.Bool(); p.FilteredMessage.Set {
		p.FilteredMessage.Val = r.String()
	}
}

// ResourcePackClientResponsePacket answers the resource packs a server
// offers.
type ResourcePackClientResponsePacket struct {
	Response        uint32
	PacksToDownload []string
}

func (*ResourcePackClientResponsePacket) ID() uint32 { return 8 }

// packResponses names the responses: the name follows the number on the
// wire.
var packResponses = [...]string{"cancel", "downloading", "downloadingfinished", "resourcepackstackfinished"}

// packResponseSendPacks asks for the packs to download.
const packResponseSendPacks = 1

func (p *ResourcePackClientResponsePacket) Encode(w *wire.Writer) {
	w.Varuint32(p.Response)
	if p.Response >= uint32(len(packResponses)) {
		panic("v2193: unknown resource pack response")
	}
	w.String(packResponses[p.Response])
	if p.Response == packResponseSendPacks {
		w.Varuint32(uint32(len(p.PacksToDownload)))
		for _, s := range p.PacksToDownload {
			w.String(s)
		}
	}
}

func (p *ResourcePackClientResponsePacket) Decode(r *wire.Reader) {
	p.Response = r.Varuint32()
	if p.Response >= uint32(len(packResponses)) {
		r.Failf("v2193: resource pack response %d", p.Response)
		return
	}
	// The name repeats the response.
	_ = r.String()
	if p.Response == packResponseSendPacks {
		n := r.Count(r.Len())
		p.PacksToDownload = make([]string, n)
		for i := range p.PacksToDownload {
			p.PacksToDownload[i] = r.String()
		}
	}
}

// ItemDescriptor describes an item of a recipe: one of the *ItemDescriptor
// types.
type ItemDescriptor interface {
	Encode(w *wire.Writer)
	Decode(r *wire.Reader)
}

// ItemDescriptorCount is an item a recipe takes, with its count.
type ItemDescriptorCount struct {
	Descriptor ItemDescriptor
	Count      int32
}

// itemDescriptorAux is the metadata sent after an invalid or tag
// descriptor: any.
const itemDescriptorAux = 32767

func writeItemDescriptorCount(w *wire.Writer, x *ItemDescriptorCount) {
	switch d := x.Descriptor.(type) {
	case nil, *InvalidItemDescriptor:
		w.Varuint32(0)
		w.Varint32(itemDescriptorAux)
	case *DefaultItemDescriptor:
		w.Varuint32(1)
		w.String("name")
		d.Encode(w)
	case *MoLangItemDescriptor:
		w.Varuint32(1)
		w.String("molang")
		d.Encode(w)
	case *ItemTagItemDescriptor:
		w.Varuint32(1)
		w.String("item_tag")
		d.Encode(w)
		w.Varint32(itemDescriptorAux)
	default:
		panic("v2193: item descriptor of no type")
	}
	w.Varint32(x.Count)
}

func readItemDescriptorCount(r *wire.Reader, x *ItemDescriptorCount) {
	switch variant := r.Varuint32(); variant {
	case 0:
		x.Descriptor = &InvalidItemDescriptor{}
		r.Varint32()
	case 1:
		switch name := r.String(); name {
		case "name":
			d := &DefaultItemDescriptor{}
			d.Decode(r)
			x.Descriptor = d
		case "molang":
			d := &MoLangItemDescriptor{}
			d.Decode(r)
			x.Descriptor = d
		case "item_tag":
			d := &ItemTagItemDescriptor{}
			d.Decode(r)
			r.Varint32()
			x.Descriptor = d
		default:
			r.Failf("v2193: item descriptor %q", name)
			return
		}
	default:
		r.Failf("v2193: item descriptor variant %d", variant)
		return
	}
	x.Count = r.Varint32()
}

func (x *ItemDescriptorCount) Encode(w *wire.Writer) { writeItemDescriptorCount(w, x) }
func (x *ItemDescriptorCount) Decode(r *wire.Reader) { readItemDescriptorCount(r, x) }
