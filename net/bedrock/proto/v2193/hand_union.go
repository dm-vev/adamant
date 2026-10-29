package v2193

import (
	"math/bits"

	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// Event is the data of an EventPacket: one of the *Event types.
type Event interface {
	Encode(w *wire.Writer)
	Decode(r *wire.Reader)
}

func newEvent(t int32) Event {
	switch t {
	case 0:
		return &AchievementAwardedEvent{}
	case 1:
		return &EntityInteractEvent{}
	case 2:
		return &PortalBuiltEvent{}
	case 3:
		return &PortalUsedEvent{}
	case 4:
		return &MobKilledEvent{}
	case 5:
		return &CauldronUsedEvent{}
	case 6:
		return &PlayerDiedEvent{}
	case 7:
		return &BossKilledEvent{}
	case 8:
		return &AgentCommandEvent{}
	case 9:
		return &AgentCreatedEvent{}
	case 10:
		return &PatternRemovedEvent{}
	case 11:
		return &SlashCommandExecutedEvent{}
	case 12:
		return &FishBucketedEvent{}
	case 13:
		return &MobBornEvent{}
	case 14:
		return &PetDiedEvent{}
	case 15:
		return &CauldronInteractEvent{}
	case 16:
		return &ComposterInteractEvent{}
	case 17:
		return &BellUsedEvent{}
	case 18:
		return &EntityDefinitionTriggerEvent{}
	case 19:
		return &RaidUpdateEvent{}
	case 20:
		return &MovementAnomalyEvent{}
	case 21:
		return &MovementCorrectedEvent{}
	case 22:
		return &ExtractHoneyEvent{}
	case 23:
		return &TargetBlockHitEvent{}
	case 24:
		return &PiglinBarterEvent{}
	case 25:
		return &WaxedOrUnwaxedCopperEvent{}
	case 26:
		return &CodeBuilderRuntimeActionEvent{}
	case 27:
		return &CodeBuilderScoreboardEvent{}
	case 28:
		return &StriderRiddenInLavaInOverworldEvent{}
	case 29:
		return &SneakCloseToSculkSensorEvent{}
	case 30:
		return &CarefulRestorationEvent{}
	case 31:
		return &ItemUsedEvent{}
	}
	return nil
}

// eventType returns the type of an event and its ordinal, the index of
// the event among those the game still sends; ok is false for an event
// without one, which the game no longer reads.
func eventType(e Event) (t int32, ordinal uint32, ok bool) {
	switch e.(type) {
	case *AchievementAwardedEvent:
		return 0, 0, true
	case *EntityInteractEvent:
		return 1, 1, true
	case *PortalBuiltEvent:
		return 2, 2, true
	case *PortalUsedEvent:
		return 3, 3, true
	case *MobKilledEvent:
		return 4, 4, true
	case *CauldronUsedEvent:
		return 5, 5, true
	case *PlayerDiedEvent:
		return 6, 6, true
	case *BossKilledEvent:
		return 7, 7, true
	case *SlashCommandExecutedEvent:
		return 11, 8, true
	case *MobBornEvent:
		return 13, 9, true
	case *CauldronInteractEvent:
		return 15, 10, true
	case *ComposterInteractEvent:
		return 16, 11, true
	case *BellUsedEvent:
		return 17, 12, true
	case *EntityDefinitionTriggerEvent:
		return 18, 13, true
	case *RaidUpdateEvent:
		return 19, 14, true
	case *TargetBlockHitEvent:
		return 23, 15, true
	case *PiglinBarterEvent:
		return 24, 16, true
	case *WaxedOrUnwaxedCopperEvent:
		return 25, 17, true
	case *CodeBuilderRuntimeActionEvent:
		return 26, 18, true
	case *CodeBuilderScoreboardEvent:
		return 27, 19, true
	case *ItemUsedEvent:
		return 31, 20, true
	case *StriderRiddenInLavaInOverworldEvent:
		return 28, 21, true
	case *SneakCloseToSculkSensorEvent:
		return 29, 21, true
	case *CarefulRestorationEvent:
		return 30, 21, true
	}
	return 0, 0, false
}

// EventPacket tells a client of an event it counts towards achievements.
type EventPacket struct {
	EntityRuntimeID int64
	UsePlayerID     bool
	Event           Event
}

func (*EventPacket) ID() uint32 { return 65 }

func (p *EventPacket) Encode(w *wire.Writer) {
	t, ordinal, ok := eventType(p.Event)
	if !ok {
		panic("v2193: event the game does not read")
	}
	w.Varint64(p.EntityRuntimeID)
	w.Varint32(t)
	w.Bool(p.UsePlayerID)
	w.Varuint32(ordinal)
	p.Event.Encode(w)
}

func (p *EventPacket) Decode(r *wire.Reader) {
	p.EntityRuntimeID = r.Varint64()
	t := r.Varint32()
	p.Event = newEvent(t)
	if _, _, ok := eventType(p.Event); !ok {
		r.Failf("v2193: event type %d", t)
		return
	}
	p.UsePlayerID = r.Bool()
	// The ordinal follows from the type.
	_ = r.Varuint32()
	p.Event.Decode(r)
}

// ShapeData is what is particular to the shape of a PrimitiveShape: one
// of the *Shape types.
type ShapeData interface {
	Encode(w *wire.Writer)
	Decode(r *wire.Reader)
}

func newShapeData(t uint32) ShapeData {
	switch t {
	case 0:
		return &LastShape{}
	case 1:
		return &ArrowShape{}
	case 2:
		return &TextShape{}
	case 3:
		return &BoxShape{}
	case 4:
		return &LineShape{}
	case 5:
		return &SphereShape{}
	case 6:
		return &CylinderShape{}
	case 7:
		return &PyramidShape{}
	case 8:
		return &EllipsoidShape{}
	case 9:
		return &ConeShape{}
	}
	return nil
}

func writeShapeData(w *wire.Writer, x *ShapeData) {
	var t uint32
	switch (*x).(type) {
	case *LastShape:
		t = 0
	case *ArrowShape:
		t = 1
	case *TextShape:
		t = 2
	case *BoxShape:
		t = 3
	case *LineShape:
		t = 4
	case *SphereShape:
		t = 5
	case *CylinderShape:
		t = 6
	case *PyramidShape:
		t = 7
	case *EllipsoidShape:
		t = 8
	case *ConeShape:
		t = 9
	default:
		panic("v2193: shape data of no type")
	}
	w.Varuint32(t)
	(*x).Encode(w)
}

func readShapeData(r *wire.Reader, x *ShapeData) {
	t := r.Varuint32()
	if *x = newShapeData(t); *x == nil {
		r.Failf("v2193: shape data type %d", t)
		return
	}
	(*x).Decode(r)
}

// AbilityValue is the value of an ability: a bool or a float32.
type AbilityValue any

// writeAbilityValue writes the type of the value, then a bool and a
// float32, the one of the other type zero.
func writeAbilityValue(w *wire.Writer, x *AbilityValue) {
	switch v := (*x).(type) {
	case bool:
		w.Uint8(1)
		w.Bool(v)
		w.Float32(0)
	case float32:
		w.Uint8(2)
		w.Bool(false)
		w.Float32(v)
	default:
		panic("v2193: ability value neither bool nor float32")
	}
}

func readAbilityValue(r *wire.Reader, x *AbilityValue) {
	t := r.Uint8()
	b := r.Bool()
	f := r.Float32()
	switch t {
	case 1:
		*x = b
	case 2:
		*x = f
	default:
		r.Failf("v2193: ability value type %d", t)
	}
}

// PackSetting is a setting of a resource pack, whose Value is a float32,
// a bool, a string or a []string.
type PackSetting struct {
	Name  string
	Value any
}

func writePackSetting(w *wire.Writer, x *PackSetting) {
	w.String(x.Name)
	switch v := x.Value.(type) {
	case float32:
		w.Varuint32(0)
		w.Float32(v)
	case bool:
		w.Varuint32(1)
		w.Bool(v)
	case string:
		w.Varuint32(2)
		w.String(v)
	case []string:
		w.Varuint32(3)
		w.Varuint32(uint32(len(v)))
		for _, s := range v {
			w.String(s)
		}
	default:
		panic("v2193: pack setting of no type")
	}
}

func readPackSetting(r *wire.Reader, x *PackSetting) {
	x.Name = r.String()
	switch t := r.Varuint32(); t {
	case 0:
		x.Value = r.Float32()
	case 1:
		x.Value = r.Bool()
	case 2:
		x.Value = r.String()
	case 3:
		v := make([]string, r.Count(r.Len()))
		for i := range v {
			v[i] = r.String()
		}
		x.Value = v
	default:
		r.Failf("v2193: pack setting type %d", t)
	}
}

// Bitset is a set of bits: bit i is bit i%64 of word i/64. It goes as
// groups of seven bits from the lowest, each in a byte whose high bit says
// another follows, as an unsigned varint goes.
type Bitset []uint64

// Has reports whether bit i is set.
func (b Bitset) Has(i int) bool {
	return i >= 0 && i/64 < len(b) && b[i/64]&(1<<(i%64)) != 0
}

// Set sets bit i.
func (b *Bitset) Set(i int) {
	for len(*b) <= i/64 {
		*b = append(*b, 0)
	}
	(*b)[i/64] |= 1 << (i % 64)
}

// bitLen returns the index of the highest bit set, plus one.
func (b Bitset) bitLen() int {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] != 0 {
			return i*64 + bits.Len64(b[i])
		}
	}
	return 0
}

func writeBitset(w *wire.Writer, x *Bitset, size int) {
	n := x.bitLen()
	if n > size {
		panic("v2193: bitset larger than its size")
	}
	groups := max(1, (n+6)/7)
	for g := range groups {
		var v byte
		for j := range 7 {
			if x.Has(g*7 + j) {
				v |= 1 << j
			}
		}
		if g < groups-1 {
			v |= 0x80
		}
		w.Uint8(v)
	}
}

func readBitset(r *wire.Reader, x *Bitset, size int) {
	*x = nil
	for i := 0; i < size; i += 7 {
		v := r.Uint8()
		if r.Err() != nil {
			return
		}
		// No bit, nor the bit saying another group follows, past the size.
		if i+bits.Len8(v) > size {
			break
		}
		for j := range 7 {
			if v&(1<<j) != 0 {
				x.Set(i + j)
			}
		}
		if v&0x80 == 0 {
			return
		}
	}
	r.Failf("v2193: bitset over %d bits", size)
}

// PlayerUpdateEntityOverridesPacket overrides a property of an entity for
// the player only, or clears the overrides.
type PlayerUpdateEntityOverridesPacket struct {
	EntityUniqueID int64
	PropertyIndex  uint32
	// Type is 0 to clear every override, 1 to remove one, 2 to set an int
	// and 3 to set a float.
	Type       uint32
	IntValue   int32
	FloatValue float32
}

// entityOverrideTypes names the types of overrides: the name follows the
// type.
var entityOverrideTypes = [...]string{"clearoverrides", "removeoverride", "setintoverride", "setfloatoverride"}

func (*PlayerUpdateEntityOverridesPacket) ID() uint32 { return 325 }

func (p *PlayerUpdateEntityOverridesPacket) Encode(w *wire.Writer) {
	if p.Type >= uint32(len(entityOverrideTypes)) {
		panic("v2193: unknown entity override type")
	}
	w.Varint64(p.EntityUniqueID)
	w.Varuint32(p.PropertyIndex)
	w.Varuint32(p.Type)
	w.String(entityOverrideTypes[p.Type])
	switch p.Type {
	case 2:
		w.Int32(p.IntValue)
	case 3:
		w.Float32(p.FloatValue)
	}
}

func (p *PlayerUpdateEntityOverridesPacket) Decode(r *wire.Reader) {
	p.EntityUniqueID = r.Varint64()
	p.PropertyIndex = r.Varuint32()
	p.Type = r.Varuint32()
	if p.Type >= uint32(len(entityOverrideTypes)) {
		r.Failf("v2193: entity override type %d", p.Type)
		return
	}
	_ = r.String()
	switch p.Type {
	case 2:
		p.IntValue = r.Int32()
	case 3:
		p.FloatValue = r.Float32()
	}
}
