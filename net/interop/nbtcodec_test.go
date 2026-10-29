package interop

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/dm-vev/adamant/net/nbt"
	gtnbt "github.com/sandertv/gophertunnel/minecraft/nbt"
)

// nbtSample is a struct of the kinds of fields the server saves.
type nbtSample struct {
	Name     string           `nbt:"name"`
	Count    int32            `nbt:"count"`
	Big      int64            `nbt:"big,omitempty"`
	Ratio    float32          `nbt:"ratio"`
	Flag     bool             `nbt:"flag"`
	Bytes    [4]byte          `nbt:"bytes"`
	Ints     [3]int32         `nbt:"ints"`
	List     []string         `nbt:"list"`
	Children []map[string]any `nbt:"children"`
	Extra    map[string]any   `nbt:"extra"`
	Skipped  string           `nbt:"-"`
}

// TestNBTCodecMatchesGophertunnel encodes random values with net/nbt and
// with gophertunnel's nbt, which the worlds of the server were saved with,
// in every encoding: each decodes the other's bytes to the value encoded,
// and the bytes are the same where no map leaves the order to chance.
func TestNBTCodecMatchesGophertunnel(t *testing.T) {
	r := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	encodings := []struct {
		ours nbt.Encoding
		gt   gtnbt.Encoding
	}{{nbt.LittleEndian, gtnbt.LittleEndian}, {nbt.NetworkLittleEndian, gtnbt.NetworkLittleEndian}, {nbt.BigEndian, gtnbt.BigEndian}}
	for i := range 1000 {
		v := nbtSample{
			Name: string(rune('a' + r.IntN(26))), Count: r.Int32(), Big: r.Int64N(1 << 40), Ratio: r.Float32(), Flag: r.IntN(2) == 0,
			Bytes: [4]byte{byte(r.IntN(256))}, Ints: [3]int32{r.Int32()}, List: []string{"x", "y"},
		}
		// Both decode empty lists and compounds as empty, not nil, and
		// lists of numbers as slices of them.
		v.Children, v.Extra = []map[string]any{}, map[string]any{}
		if i%2 == 0 {
			v.Children = []map[string]any{{"id": "minecraft:stone", "Count": byte(r.IntN(64))}}
			v.Extra = map[string]any{"a": int16(r.IntN(1000)), "b": []int32{1, 2}, "c": map[string]any{"d": r.Float64()}}
		}
		for _, e := range encodings {
			ours, err := nbt.MarshalEncoding(v, e.ours)
			if err != nil {
				t.Fatal(err)
			}
			theirs, err := gtnbt.MarshalEncoding(v, e.gt)
			if err != nil {
				t.Fatal(err)
			}
			if len(v.Extra) == 0 && !bytes.Equal(ours, theirs) {
				t.Fatalf("bytes differ: % x and % x", ours, theirs)
			}
			var a, b nbtSample
			if err := nbt.UnmarshalEncoding(theirs, &a, e.ours); err != nil {
				t.Fatal(err)
			}
			if err := gtnbt.UnmarshalEncoding(ours, &b, e.gt); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, v) || !reflect.DeepEqual(b, v) {
				t.Fatalf("decoded %#v and %#v, want %#v", a, b, v)
			}
		}
	}
}

// FuzzNBTDecodeMatchesGophertunnel decodes any bytes with both: what
// gophertunnel decodes, net/nbt decodes alike, and what it refuses, net/nbt
// refuses too. Where gophertunnel panics or refuses over the depth it left
// behind, net/nbt was made to differ. Data that net/nbt refuses for a
// length past its end or a nesting past the bound is not given to
// gophertunnel, which runs out of memory or stack over it and takes the
// process down.
func FuzzNBTDecodeMatchesGophertunnel(f *testing.F) {
	for _, v := range []any{nbtSample{Name: "a", List: []string{"b"}}, map[string]any{"x": []int32{1}, "y": map[string]any{"z": "w"}}} {
		for _, e := range []nbt.Encoding{nbt.LittleEndian, nbt.NetworkLittleEndian, nbt.BigEndian} {
			b, err := nbt.MarshalEncoding(v, e)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for i, e := range []nbt.Encoding{nbt.LittleEndian, nbt.NetworkLittleEndian, nbt.BigEndian} {
			ge := []gtnbt.Encoding{gtnbt.LittleEndian, gtnbt.NetworkLittleEndian, gtnbt.BigEndian}[i]
			var a, b map[string]any
			errA := nbt.UnmarshalEncoding(data, &a, e)
			if errors.As(errA, new(nbt.BufferOverrunError)) || errors.As(errA, new(nbt.MaximumDepthReachedError)) {
				continue
			}
			errB, panicked := func() (err error, panicked bool) {
				defer func() {
					if recover() != nil {
						panicked = true
					}
				}()
				return gtnbt.UnmarshalEncoding(data, &b, ge), false
			}()
			if panicked || errors.As(errB, new(gtnbt.MaximumDepthReachedError)) {
				continue
			}
			if (errA == nil) != (errB == nil) {
				t.Fatalf("errors %v and %v", errA, errB)
			}
			// fmt sorts the keys of maps, and prints NaN alike.
			if errA == nil && fmt.Sprintf("%#v", a) != fmt.Sprintf("%#v", b) {
				t.Fatalf("decoded %#v and %#v", a, b)
			}
		}
	})
}
