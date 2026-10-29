package v2193

import (
	"slices"
	"sync/atomic"

	"github.com/dm-vev/adamant/net/bedrock/nbt"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// The codecs written by hand: values whose encoding the schema cannot say.

// shieldID is the network ID of the shield, whose items carry the tick they
// started blocking at.
var shieldID atomic.Int32

// SetShieldID sets the network ID of the shield, from the item registry the
// server sends. Every connection of the process shares it.
func SetShieldID(id int32) { shieldID.Store(id) }

// ItemStack is a stack of items as most packets carry it.
type ItemStack struct {
	NetworkID      int32
	MetadataValue  uint32
	BlockRuntimeID int32
	Count          uint16
	NBTData        map[string]any
	CanBePlacedOn  []string
	CanBreak       []string
	BlockingTick   int64
}

// ItemInstance is an item stack with the network ID of the stack, as
// inventories carry it.
type ItemInstance struct {
	StackNetworkID int32
	Stack          ItemStack
}

func writeItemStack(w *wire.Writer, x *ItemStack) {
	w.Varint32(x.NetworkID)
	w.Uint16(x.Count)
	w.Varuint32(x.MetadataValue)
	w.Varint32(x.BlockRuntimeID)
	writeUserData(w, x)
}

func readItemStack(r *wire.Reader, x *ItemStack) {
	x.NetworkID = r.Varint32()
	x.Count = r.Uint16()
	x.MetadataValue = r.Varuint32()
	x.BlockRuntimeID = r.Varint32()
	readUserData(r, x)
}

func writeItemInstance(w *wire.Writer, x *ItemInstance) {
	s := &x.Stack
	w.Int16(int16(s.NetworkID))
	w.Uint16(s.Count)
	w.Varuint32(s.MetadataValue)
	w.Bool(x.StackNetworkID != 0)
	if x.StackNetworkID != 0 {
		w.Varint32(x.StackNetworkID)
	}
	w.Varuint32(uint32(s.BlockRuntimeID))
	writeUserData(w, s)
}

func readItemInstance(r *wire.Reader, x *ItemInstance) {
	s := &x.Stack
	s.NetworkID = int32(r.Int16())
	s.Count = r.Uint16()
	s.MetadataValue = r.Varuint32()
	x.StackNetworkID = 0
	if r.Bool() {
		x.StackNetworkID = r.Varint32()
	}
	s.BlockRuntimeID = int32(r.Varuint32())
	readUserData(r, s)
}

// writeUserData writes the user data of an item stack.
func writeUserData(w *wire.Writer, x *ItemStack) {
	writeItemData(w, x.NetworkID != 0, x.NetworkID == shieldID.Load(), x.NBTData, x.CanBePlacedOn, x.CanBreak, x.BlockingTick)
}

func readUserData(r *wire.Reader, x *ItemStack) {
	x.NBTData, x.CanBePlacedOn, x.CanBreak, x.BlockingTick = readItemData(r, x.NetworkID == shieldID.Load())
}

// writeItemData writes the extra data of an item: its NBT in the form of
// worlds, the blocks it may be placed on and break, and for a shield its
// blocking tick, all prefixed with their length. An empty item has none.
func writeItemData(w *wire.Writer, present, shield bool, data map[string]any, placeOn, canBreak []string, tick int64) {
	if !present {
		w.Varuint32(0)
		return
	}
	var b wire.Writer
	if len(data) > 0 {
		b.Int16(-1)
		b.Uint8(1)
		if err := nbt.WriteEncoded(&b, data, nbt.LittleEndian); err != nil {
			panic(err)
		}
	} else {
		b.Int16(0)
	}
	writeStrings16(&b, placeOn)
	writeStrings16(&b, canBreak)
	if shield {
		b.Int64(tick)
	}
	w.ByteSlice(b.B)
}

func readItemData(r *wire.Reader, shield bool) (data map[string]any, placeOn, canBreak []string, tick int64) {
	b := r.ByteSlice()
	if len(b) == 0 || r.Err() != nil {
		return
	}
	d := wire.NewReader(b)
	switch d.Int16() {
	case 0:
	case -1:
		if v := d.Uint8(); v != 1 {
			r.Failf("v2193: item user data of version %d", v)
			return
		}
		fallthrough
	default:
		m, err := nbt.ReadEncoded(d, nbt.LittleEndian)
		if err != nil {
			r.Fail(err)
			return
		}
		data = m
	}
	placeOn = readStrings16(d)
	canBreak = readStrings16(d)
	if shield {
		tick = d.Int64()
	}
	if d.Err() != nil {
		r.Fail(d.Err())
	}
	return
}

// writeStrings16 writes strings prefixed with an int16 length, the list
// with a uint32 count.
func writeStrings16(w *wire.Writer, s []string) {
	w.Uint32(uint32(len(s)))
	for _, v := range s {
		w.Int16(int16(len(v)))
		w.Bytes([]byte(v))
	}
}

func readStrings16(r *wire.Reader) []string {
	n := int(r.Uint32())
	if n > r.Len() {
		r.Fail(wire.ErrShort)
		return nil
	}
	var s []string
	for range n {
		l := int(r.Int16())
		if l < 0 {
			r.Fail(wire.ErrShort)
			return nil
		}
		s = append(s, string(r.Next(l)))
	}
	return s
}

// Metadata holds the data of an entity by key. Values are uint8, int16,
// int32, float32, string, map[string]any (NBT), [3]int32 (a block
// position), int64 or [3]float32 (a vector).
type Metadata map[uint32]any

// Types of metadata values.
const (
	metadataByte uint32 = iota
	metadataInt16
	metadataInt32
	metadataFloat32
	metadataString
	metadataCompound
	metadataBlockPos
	metadataInt64
	metadataVec3
)

// writeMetadata writes metadata in order of keys: some, such as the
// buttons of an NPC, only show in that order. A value of another type is
// a bug of the caller.
func writeMetadata(w *wire.Writer, x *Metadata) {
	keys := make([]uint32, 0, len(*x))
	for k := range *x {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	w.Varuint32(uint32(len(keys)))
	for _, k := range keys {
		w.Varuint32(k)
		typ := func(t uint32) {
			// The type goes twice: once as the selector of the value.
			w.Varuint32(t)
			w.Uint8(uint8(t))
		}
		switch v := (*x)[k].(type) {
		case uint8:
			typ(metadataByte)
			w.Uint8(v)
		case int16:
			typ(metadataInt16)
			w.Int16(v)
		case int32:
			typ(metadataInt32)
			w.Varint32(v)
		case float32:
			typ(metadataFloat32)
			w.Float32(v)
		case string:
			typ(metadataString)
			w.String(v)
		case map[string]any:
			typ(metadataCompound)
			if err := nbt.Write(w, v); err != nil {
				panic(err)
			}
		case [3]int32:
			typ(metadataBlockPos)
			w.Varint32(v[0])
			w.Varint32(v[1])
			w.Varint32(v[2])
		case int64:
			typ(metadataInt64)
			w.Varint64(v)
		case [3]float32:
			typ(metadataVec3)
			w.Float32(v[0])
			w.Float32(v[1])
			w.Float32(v[2])
		default:
			panic("v2193: metadata value of no type")
		}
	}
}

func readMetadata(r *wire.Reader, x *Metadata) {
	n := int(r.Varuint32())
	if n > r.Len() {
		r.Fail(wire.ErrShort)
		return
	}
	m := make(Metadata, n)
	for range n {
		k, t := r.Varuint32(), r.Varuint32()
		if legacy := r.Uint8(); uint32(legacy) != t {
			r.Failf("v2193: metadata type %d named again as %d", t, legacy)
			return
		}
		switch t {
		case metadataByte:
			m[k] = r.Uint8()
		case metadataInt16:
			m[k] = r.Int16()
		case metadataInt32:
			m[k] = r.Varint32()
		case metadataFloat32:
			m[k] = r.Float32()
		case metadataString:
			m[k] = r.String()
		case metadataCompound:
			v, err := nbt.Read(r)
			if err != nil {
				r.Fail(err)
				return
			}
			m[k] = v
		case metadataBlockPos:
			m[k] = [3]int32{r.Varint32(), r.Varint32(), r.Varint32()}
		case metadataInt64:
			m[k] = r.Varint64()
		case metadataVec3:
			m[k] = [3]float32{r.Float32(), r.Float32(), r.Float32()}
		default:
			r.Failf("v2193: metadata type %d", t)
			return
		}
		if r.Err() != nil {
			return
		}
	}
	*x = m
}
