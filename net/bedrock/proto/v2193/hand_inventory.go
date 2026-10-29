package v2193

import (
	"strings"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// InventoryTransactionData is the data of an InventoryTransaction: one of
// the *TransactionData types.
type InventoryTransactionData interface {
	Encode(w *wire.Writer)
	Decode(r *wire.Reader)
}

func newTransactionData(t uint32) InventoryTransactionData {
	switch t {
	case 0:
		return &NormalTransactionData{}
	case 1:
		return &MismatchTransactionData{}
	case 2:
		return &UseItemTransactionData{}
	case 3:
		return &UseItemOnEntityTransactionData{}
	case 4:
		return &ReleaseItemTransactionData{}
	}
	return nil
}

func transactionType(d InventoryTransactionData) uint32 {
	switch d.(type) {
	case *NormalTransactionData:
		return 0
	case *MismatchTransactionData:
		return 1
	case *UseItemTransactionData:
		return 2
	case *UseItemOnEntityTransactionData:
		return 3
	case *ReleaseItemTransactionData:
		return 4
	}
	panic("v2193: inventory transaction data of no type")
}

// InventoryTransactionPacket carries a transaction of a client's
// inventory.
type InventoryTransactionPacket struct {
	LegacyRequestID    int32
	LegacySetItemSlots []LegacySetItemSlot
	Actions            []InventoryAction
	TransactionData    InventoryTransactionData
}

func (*InventoryTransactionPacket) ID() uint32 { return 30 }

func (p *InventoryTransactionPacket) Encode(w *wire.Writer) {
	w.Varint32(p.LegacyRequestID)
	// Legacy slots go with an even request ID under -1.
	hasLegacy := p.LegacyRequestID < -1 && p.LegacyRequestID&1 == 0
	w.Bool(hasLegacy)
	if hasLegacy {
		w.Varuint32(uint32(len(p.LegacySetItemSlots)))
		for i := range p.LegacySetItemSlots {
			p.LegacySetItemSlots[i].Encode(w)
		}
	}
	// The type of the data goes before the actions, the data after.
	w.Varuint32(transactionType(p.TransactionData))
	w.Varuint32(uint32(len(p.Actions)))
	for i := range p.Actions {
		p.Actions[i].Encode(w)
	}
	p.TransactionData.Encode(w)
}

func (p *InventoryTransactionPacket) Decode(r *wire.Reader) {
	p.LegacyRequestID = r.Varint32()
	p.LegacySetItemSlots = nil
	if r.Bool() {
		p.LegacySetItemSlots = make([]LegacySetItemSlot, r.Count(r.Len()))
		for i := range p.LegacySetItemSlots {
			p.LegacySetItemSlots[i].Decode(r)
		}
	}
	t := r.Varuint32()
	p.TransactionData = newTransactionData(t)
	if p.TransactionData == nil {
		r.Failf("v2193: inventory transaction type %d", t)
		return
	}
	p.Actions = make([]InventoryAction, r.Count(r.Len()))
	for i := range p.Actions {
		p.Actions[i].Decode(r)
	}
	p.TransactionData.Decode(r)
}

// PlayerListEntry adds a player to the player list, or removes one.
type PlayerListEntry struct {
	// ActionType is 0 to add, 1 to remove.
	ActionType     uint8
	UUID           [16]byte
	EntityUniqueID int64
	Username       string
	XUID           string
	PlatformChatID string
	BuildPlatform  int32
	Skin           Skin
	Teacher        bool
	Host           bool
	SubClient      bool
	PlayerColour   proto.Color
}

const playerListRemove = 1

func (x *PlayerListEntry) Encode(w *wire.Writer) {
	// The selector is 1 to add; the action follows as a byte.
	variant := uint32(0)
	if x.ActionType != playerListRemove {
		variant = 1
	}
	w.Varuint32(variant)
	w.Uint8(x.ActionType)
	w.UUID(x.UUID)
	if x.ActionType == playerListRemove {
		return
	}
	w.Varint64(x.EntityUniqueID)
	w.String(x.Username)
	w.String(x.XUID)
	w.String(x.PlatformChatID)
	w.Int32(x.BuildPlatform)
	x.Skin.Encode(w)
	w.Bool(x.Teacher)
	w.Bool(x.Host)
	w.Bool(x.SubClient)
	proto.WriteARGBBE(w, &x.PlayerColour)
}

func (x *PlayerListEntry) Decode(r *wire.Reader) {
	variant := r.Varuint32()
	r.Uint8()
	switch variant {
	case 0:
		x.ActionType = playerListRemove
	case 1:
		x.ActionType = 0
	default:
		r.Failf("v2193: player list entry variant %d", variant)
		return
	}
	x.UUID = r.UUID()
	if x.ActionType == playerListRemove {
		return
	}
	x.EntityUniqueID = r.Varint64()
	x.Username = r.String()
	x.XUID = r.String()
	x.PlatformChatID = r.String()
	x.BuildPlatform = r.Int32()
	x.Skin.Decode(r)
	x.Teacher = r.Bool()
	x.Host = r.Bool()
	x.SubClient = r.Bool()
	proto.ReadARGBBE(r, &x.PlayerColour)
}

// Skin is the skin of a player.
type Skin struct {
	SkinID                    string
	PlayFabID                 string
	SkinResourcePatch         []byte
	SkinImageWidth            uint32
	SkinImageHeight           uint32
	SkinData                  []byte
	Animations                []SkinAnimation
	CapeImageWidth            uint32
	CapeImageHeight           uint32
	CapeData                  []byte
	SkinGeometry              []byte
	GeometryDataEngineVersion []byte
	AnimationData             []byte
	CapeID                    string
	FullID                    string
	ArmSize                   uint8
	SkinColour                proto.Color
	PersonaPieces             []PersonaPiece
	PieceTintColours          []PersonaPieceTintColour
	PremiumSkin               bool
	PersonaSkin               bool
	PersonaCapeOnClassicSkin  bool
	PrimaryUser               bool
	OverrideAppearance        bool
	Trusted                   bool
	ProfileHash               string
}

func (x *Skin) Encode(w *wire.Writer) {
	w.String(x.SkinID)
	w.String(x.PlayFabID)
	w.ByteSlice(x.SkinResourcePatch)
	w.Uint32(x.SkinImageWidth)
	w.Uint32(x.SkinImageHeight)
	w.ByteSlice(x.SkinData)
	w.Varuint32(uint32(len(x.Animations)))
	for i := range x.Animations {
		x.Animations[i].Encode(w)
	}
	w.Uint32(x.CapeImageWidth)
	w.Uint32(x.CapeImageHeight)
	w.ByteSlice(x.CapeData)
	w.ByteSlice(x.SkinGeometry)
	w.ByteSlice(x.GeometryDataEngineVersion)
	w.ByteSlice(x.AnimationData)
	w.String(x.CapeID)
	w.String(x.FullID)
	w.Uint8(x.ArmSize)
	proto.WriteARGBBE(w, &x.SkinColour)
	w.Varuint32(uint32(len(x.PersonaPieces)))
	for i := range x.PersonaPieces {
		x.PersonaPieces[i].Encode(w)
	}
	w.Varuint32(uint32(len(x.PieceTintColours)))
	for i := range x.PieceTintColours {
		x.PieceTintColours[i].Encode(w)
	}
	w.Bool(x.PremiumSkin)
	w.Bool(x.PersonaSkin)
	w.Bool(x.PersonaCapeOnClassicSkin)
	w.Bool(x.PrimaryUser)
	w.Bool(x.OverrideAppearance)
	// Trust goes as the string "true" or "false".
	if x.Trusted {
		w.String("true")
	} else {
		w.String("false")
	}
	w.String(x.ProfileHash)
}

func (x *Skin) Decode(r *wire.Reader) {
	x.SkinID = r.String()
	x.PlayFabID = r.String()
	x.SkinResourcePatch = r.ByteSlice()
	x.SkinImageWidth = r.Uint32()
	x.SkinImageHeight = r.Uint32()
	x.SkinData = r.ByteSlice()
	x.Animations = make([]SkinAnimation, r.Count(r.Len()))
	for i := range x.Animations {
		x.Animations[i].Decode(r)
	}
	x.CapeImageWidth = r.Uint32()
	x.CapeImageHeight = r.Uint32()
	x.CapeData = r.ByteSlice()
	x.SkinGeometry = r.ByteSlice()
	x.GeometryDataEngineVersion = r.ByteSlice()
	x.AnimationData = r.ByteSlice()
	x.CapeID = r.String()
	x.FullID = r.String()
	x.ArmSize = r.Uint8()
	proto.ReadARGBBE(r, &x.SkinColour)
	x.PersonaPieces = make([]PersonaPiece, r.Count(r.Len()))
	for i := range x.PersonaPieces {
		x.PersonaPieces[i].Decode(r)
	}
	x.PieceTintColours = make([]PersonaPieceTintColour, r.Count(r.Len()))
	for i := range x.PieceTintColours {
		x.PieceTintColours[i].Decode(r)
	}
	x.PremiumSkin = r.Bool()
	x.PersonaSkin = r.Bool()
	x.PersonaCapeOnClassicSkin = r.Bool()
	x.PrimaryUser = r.Bool()
	x.OverrideAppearance = r.Bool()
	x.Trusted = strings.EqualFold(r.String(), "true")
	x.ProfileHash = r.String()
}

// PersonaPieceTintColour holds the colours of a piece of a persona skin.
// The piece goes on the wire without its persona_ prefix.
type PersonaPieceTintColour struct {
	PieceType string
	Colours   [4]proto.Color
}

func (x *PersonaPieceTintColour) Encode(w *wire.Writer) {
	w.String(tintWireType(x.PieceType))
	for i := range x.Colours {
		proto.WriteARGBBE(w, &x.Colours[i])
	}
}

func (x *PersonaPieceTintColour) Decode(r *wire.Reader) {
	x.PieceType = tintLoginType(r.String())
	for i := range x.Colours {
		proto.ReadARGBBE(r, &x.Colours[i])
	}
}

func tintWireType(t string) string {
	if t == "persona_hand" {
		return "hands"
	}
	return strings.TrimPrefix(t, "persona_")
}

func tintLoginType(t string) string {
	switch t {
	case "", "unsupported":
		return t
	case "hands":
		return "persona_hand"
	}
	return "persona_" + t
}

// ScoreboardEntry changes or removes an entry of a scoreboard.
type ScoreboardEntry struct {
	// IdentityType is 0 to remove, 1 for a player, 2 for an entity, 3 for
	// a fake player.
	IdentityType   uint8
	EntryID        int64
	ObjectiveName  string
	Score          int32
	EntityUniqueID int64
	DisplayName    string
}

// scoreboardTypes names the identity types: the name follows the type.
var scoreboardTypes = [...]string{"remove", "changeplayer", "changeentity", "changefakeplayer"}

func (x *ScoreboardEntry) Encode(w *wire.Writer) {
	if int(x.IdentityType) >= len(scoreboardTypes) {
		panic("v2193: unknown scoreboard identity type")
	}
	w.Varuint32(uint32(x.IdentityType))
	w.String(scoreboardTypes[x.IdentityType])
	w.Varint64(x.EntryID)
	switch x.IdentityType {
	case 0:
		w.Bool(x.ObjectiveName != "")
		if x.ObjectiveName != "" {
			w.String(x.ObjectiveName)
		}
	case 1, 2:
		w.String(x.ObjectiveName)
		w.Int32(x.Score)
		w.Varint64(x.EntityUniqueID)
	case 3:
		w.String(x.ObjectiveName)
		w.Int32(x.Score)
		w.String(x.DisplayName)
	}
}

func (x *ScoreboardEntry) Decode(r *wire.Reader) {
	t := r.Varuint32()
	if t >= uint32(len(scoreboardTypes)) {
		r.Failf("v2193: scoreboard identity type %d", t)
		return
	}
	x.IdentityType = uint8(t)
	_ = r.String()
	x.EntryID = r.Varint64()
	switch x.IdentityType {
	case 0:
		x.ObjectiveName = ""
		if r.Bool() {
			x.ObjectiveName = r.String()
		}
	case 1, 2:
		x.ObjectiveName = r.String()
		x.Score = r.Int32()
		x.EntityUniqueID = r.Varint64()
	case 3:
		x.ObjectiveName = r.String()
		x.Score = r.Int32()
		x.DisplayName = r.String()
	}
}

// ItemType is the network ID and metadata of an item.
type ItemType struct {
	NetworkID     int32
	MetadataValue uint32
}

// MaterialReducer is what the material reducer gives for an item.
type MaterialReducer struct {
	InputItem ItemType
	Outputs   []MaterialReducerOutput
}

// writeMaterialReducer writes the input as one varint, the network ID in
// the high 16 bits and the metadata in the low 15.
func writeMaterialReducer(w *wire.Writer, x *MaterialReducer) {
	w.Varint32(x.InputItem.NetworkID<<16 | int32(x.InputItem.MetadataValue&0x7fff))
	w.Varuint32(uint32(len(x.Outputs)))
	for i := range x.Outputs {
		x.Outputs[i].Encode(w)
	}
}

func readMaterialReducer(r *wire.Reader, x *MaterialReducer) {
	mix := r.Varint32()
	x.InputItem = ItemType{NetworkID: mix >> 16, MetadataValue: uint32(mix & 0x7fff)}
	x.Outputs = make([]MaterialReducerOutput, r.Count(r.Len()))
	for i := range x.Outputs {
		x.Outputs[i].Decode(r)
	}
}
