package interop

import (
	"bytes"
	"fmt"
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

// fill sets a value to random contents: small numbers often, so that enum
// branches are taken, short strings and lists, NBT of one key.
func fill(rng *rand.Rand, v reflect.Value, depth int) {
	if !v.CanSet() {
		v = reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
	}
	small := rng.IntN(2) == 0
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
			fill(rng, s.Index(i), depth+1)
		}
		v.Set(s)
	case reflect.Array:
		for i := range v.Len() {
			fill(rng, v.Index(i), depth+1)
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
			fill(rng, v.Field(i), depth+1)
		}
	case reflect.Pointer:
		if depth > 5 {
			return
		}
		p := reflect.New(v.Type().Elem())
		fill(rng, p.Elem(), depth+1)
		v.Set(p)
	}
}

// marshal encodes a packet with gophertunnel, reporting a value it refuses.
func marshal(pk packet.Packet) (b []byte, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	var buf bytes.Buffer
	pk.Marshal(protocol.NewWriter(&buf, 0))
	return buf.Bytes(), true
}

// TestGeneratedCodecsMatchGophertunnel decodes what gophertunnel encodes
// for random packets with the generated codecs and encodes it back: the
// bytes must not change.
func TestGeneratedCodecsMatchGophertunnel(t *testing.T) {
	pool := packet.NewClientPool()
	for id, f := range packet.NewServerPool() {
		pool[id] = f
	}
	ids := make([]uint32, 0, len(pool))
	for id := range pool {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	rng := rand.New(rand.NewPCG(1, 2))
	var codecs, raw, untested int
	for _, id := range ids {
		if v2193.New(id) == nil {
			t.Errorf("packet %d (%T) has no generated type", id, pool[id]())
			continue
		}
		if v2193.Raw(id) {
			raw++
		}
		tested := 0
		for try := 0; try < 200 && tested < 30; try++ {
			pk := pool[id]()
			fill(rng, reflect.ValueOf(pk).Elem(), 0)
			b, ok := marshal(pk)
			if !ok {
				continue
			}
			back := pool[id]()
			if !unmarshal(back, b) {
				// gophertunnel writes values its reader refuses.
				continue
			}
			tested++
			ours := v2193.New(id)
			r := wire.NewReader(b)
			ours.Decode(r)
			if r.Err() != nil || r.Len() != 0 {
				t.Errorf("%T: decode of % x: %v, %d bytes left", pk, head(b), r.Err(), r.Len())
				break
			}
			if !v2193.Raw(id) {
				if err := same(reflect.ValueOf(ours).Elem(), reflect.ValueOf(back).Elem(), ""); err != nil {
					t.Errorf("%T: %v\nbytes % x", pk, err, head(b))
					break
				}
			}
			var w wire.Writer
			ours.Encode(&w)
			if !bytes.Equal(w.B, b) {
				t.Errorf("%T: encoded back differently\ngophertunnel % x\nours         % x", pk, head(b), head(w.B))
				break
			}
		}
		if tested == 0 {
			untested++
			t.Logf("%T: gophertunnel refused every random value", pool[id]())
		} else if !v2193.Raw(id) {
			codecs++
		}
	}
	t.Logf("%d packets: %d with codecs checked, %d raw, %d untested", len(ids), codecs, raw, untested)
}

// unmarshal decodes a packet with gophertunnel.
func unmarshal(pk packet.Packet, b []byte) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	pk.Marshal(protocol.NewReader(bytes.NewBuffer(b), 0, false))
	return true
}

// open makes an unexported field readable.
func open(v reflect.Value) reflect.Value {
	if v.CanInterface() || !v.CanAddr() {
		return v
	}
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
}

// same compares a value of a generated codec with the value gophertunnel
// decoded from the same bytes, field by field by name.
func same(ours, theirs reflect.Value, path string) error {
	ours, theirs = open(ours), open(theirs)
	if theirs.Kind() == reflect.Interface || theirs.Kind() == reflect.Pointer {
		if theirs.IsNil() {
			return nil
		}
		theirs = theirs.Elem()
	}
	switch ours.Kind() {
	case reflect.Bool:
		if theirs.Kind() == reflect.Bool && ours.Bool() == theirs.Bool() {
			return nil
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if a, b := integer(ours), integer(theirs); a != nil && b != nil && *a == *b {
			return nil
		}
	case reflect.Float32, reflect.Float64:
		if theirs.CanFloat() && float32(ours.Float()) == float32(theirs.Float()) {
			return nil
		}
	case reflect.String:
		if theirs.Kind() == reflect.String && ours.String() == theirs.String() {
			return nil
		}
	case reflect.Map:
		if ours.Type().Key().Kind() != reflect.String && theirs.Kind() == reflect.Map {
			if ours.Len() != theirs.Len() {
				break
			}
			for _, k := range ours.MapKeys() {
				tv := theirs.MapIndex(k.Convert(theirs.Type().Key()))
				if !tv.IsValid() {
					return fmt.Errorf("%s[%v]: gophertunnel has no such key", path, k)
				}
				if err := same(reflect.ValueOf(ours.MapIndex(k).Interface()), reflect.ValueOf(tv.Interface()), fmt.Sprintf("%s[%v]", path, k)); err != nil {
					return err
				}
			}
			return nil
		}
		if reflect.DeepEqual(normalise(ours.Interface()), normalise(theirs.Interface())) ||
			ours.Len() == 0 && theirs.Len() == 0 {
			return nil
		}
	case reflect.Slice, reflect.Array:
		if theirs.Kind() == reflect.Struct && theirs.Type().Name() == "RGBA" {
			// A colour of gophertunnel: compare its channels.
			if ours.Len() == 3 {
				for i, ch := range []string{"R", "G", "B"} {
					if uint8(ours.Index(i).Float()*255) != uint8(theirs.FieldByName(ch).Uint()) {
						return fmt.Errorf("%s: colour %v, gophertunnel %v", path, ours.Interface(), theirs.Interface())
					}
				}
				return nil
			}
			for i, ch := range []string{"R", "G", "B", "A"} {
				if ours.Index(i).Uint() != theirs.FieldByName(ch).Uint() {
					return fmt.Errorf("%s: colour %v, gophertunnel %v", path, ours.Interface(), theirs.Interface())
				}
			}
			return nil
		}
		if theirs.Kind() != reflect.Slice && theirs.Kind() != reflect.Array || ours.Len() != theirs.Len() {
			break
		}
		for i := range ours.Len() {
			if err := same(ours.Index(i), theirs.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		if ours.Type().Name() == "" || len(ours.Type().Name()) >= 8 && ours.Type().Name()[:8] == "Optional" {
			// proto.Optional against protocol.Optional.
			if ours.NumField() == 2 && theirs.Kind() == reflect.Struct && theirs.NumField() == 2 {
				set := open(theirs.Field(0)).Bool()
				if ours.Field(0).Bool() != set {
					return fmt.Errorf("%s: set %v, gophertunnel %v", path, ours.Field(0).Bool(), set)
				}
				if !set {
					return nil
				}
				return same(ours.Field(1), theirs.Field(1), path+".Val")
			}
		}
		if theirs.Kind() != reflect.Struct {
			break
		}
		for i := range ours.NumField() {
			name := ours.Type().Field(i).Name
			tf := theirs.FieldByName(name)
			if !tf.IsValid() {
				return fmt.Errorf("%s.%s: gophertunnel has no such field", path, name)
			}
			if err := same(ours.Field(i), tf, path+"."+name); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
	return fmt.Errorf("%s: %v (%s), gophertunnel %v (%s)", path, ours.Interface(), ours.Type(), theirs.Interface(), theirs.Type())
}

// integer returns an integer value as a decimal string, nil for another
// kind.
func integer(v reflect.Value) *string {
	var s string
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		s = fmt.Sprint(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		s = fmt.Sprint(v.Uint())
	default:
		return nil
	}
	return &s
}

func head(b []byte) []byte {
	if len(b) > 64 {
		return b[:64]
	}
	return b
}

// TestShieldItemsMatchGophertunnel codes a shield, whose user data carries
// the tick it started blocking at, with the shield ID set on both sides.
func TestShieldItemsMatchGophertunnel(t *testing.T) {
	const shield = 355
	v2193.SetShieldID(shield)
	defer v2193.SetShieldID(0)
	pk := &packet.InventorySlot{WindowID: 1, Slot: 4, NewItem: protocol.ItemInstance{
		StackNetworkID: 9,
		Stack: protocol.ItemStack{
			ItemType:      protocol.ItemType{NetworkID: shield, MetadataValue: 2},
			Count:         1,
			NBTData:       map[string]any{"Damage": int32(3)},
			CanBePlacedOn: []string{"minecraft:stone"},
			BlockingTick:  123456789,
		},
	}}
	var buf bytes.Buffer
	pk.Marshal(protocol.NewWriter(&buf, shield))
	b := buf.Bytes()
	var back packet.InventorySlot
	back.Marshal(protocol.NewReader(bytes.NewBuffer(b), shield, false))

	var ours v2193.InventorySlotPacket
	r := wire.NewReader(b)
	ours.Decode(r)
	if r.Err() != nil || r.Len() != 0 {
		t.Fatalf("decode: %v, %d bytes left", r.Err(), r.Len())
	}
	if ours.NewItem.Stack.BlockingTick != 123456789 {
		t.Fatalf("blocking tick %d", ours.NewItem.Stack.BlockingTick)
	}
	if err := same(reflect.ValueOf(&ours).Elem(), reflect.ValueOf(&back).Elem(), ""); err != nil {
		t.Fatal(err)
	}
	var w wire.Writer
	ours.Encode(&w)
	if !bytes.Equal(w.B, b) {
		t.Fatalf("encoded back differently\ngophertunnel % x\nours         % x", b, w.B)
	}
}
