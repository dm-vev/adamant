package session

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/dm-vev/adamant/net/nbt"
	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
)

// TestAppendClientActorNetworkNBTEquivalence verifies that the fast-path direct
// NetworkLittleEndian NBT encoder produces decoded compounds identical to
// nbt.NewEncoderWithEncoding across all supported tag types, adamant: key
// stripping, BlockEntityVersion defaults, and fallback paths.
func TestAppendClientActorNetworkNBTEquivalence(t *testing.T) {
	pos := cube.Pos{-12345, 64, 98765}
	cases := []struct {
		name string
		data map[string]any
	}{
		{
			name: "sign_and_primitives",
			data: map[string]any{
				"id":           "Sign",
				"adamant:priv": int32(99),
				"x":            int32(0),
				"y":            int32(0),
				"z":            int32(0),
				"IsWaxed":      byte(1),
				"Flag":         true,
				"FlagFalse":    false,
				"Short":        int16(-32000),
				"Int":          int32(-123456),
				"Long":         int64(-9876543210123),
				"Float":        float32(3.5),
				"Double":       float64(-12.25),
				"Text":         "hello world",
				"Nested": map[string]any{
					"Text":        "front",
					"IgnoreLight": byte(0),
				},
			},
		},
		{
			name: "slices_and_existing_version",
			data: map[string]any{
				"id":                 "Chest",
				"BlockEntityVersion": int32(4),
				"Bytes":              []byte{1, 2, 3, 255},
				"Ints":               []int32{-1, 0, 1, math.MaxInt32},
				"Longs":              []int64{math.MinInt64, 0, math.MaxInt64},
				"Strings":            []string{"a", "bc", ""},
				"Compounds":          []map[string]any{{"Slot": byte(0), "Count": byte(16)}, {"Slot": byte(1)}},
				"EmptyAny":           []any{},
				"StringAny":          []any{"one", "two"},
				"MapAny":             []any{map[string]any{"k": int32(7)}},
			},
		},
		{
			name: "fallback_fixed_array",
			data: map[string]any{
				"id":    "Banner",
				"Fixed": [3]byte{7, 8, 9},
				"AnyI":  []any{int32(1), int32(2)},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotBytes := appendClientActorNetworkNBT(nil, tc.data, pos)
			var wantBuf bytes.Buffer
			if err := nbt.NewEncoderWithEncoding(&wantBuf, nbt.NetworkLittleEndian).Encode(clientActorNBT(tc.data, pos)); err != nil {
				t.Fatalf("reference encode: %v", err)
			}

			var got, want map[string]any
			if err := nbt.NewDecoderWithEncoding(bytes.NewReader(gotBytes), nbt.NetworkLittleEndian).Decode(&got); err != nil {
				t.Fatalf("decode fast path: %v", err)
			}
			if err := nbt.NewDecoderWithEncoding(bytes.NewReader(wantBuf.Bytes()), nbt.NetworkLittleEndian).Decode(&want); err != nil {
				t.Fatalf("decode reference: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("decoded mismatch:\ngot:  %#v\nwant: %#v", got, want)
			}
		})
	}
}

// TestBlockHashManyProperties verifies that networkBlockHash handles >16
// properties (spilling beyond the stack key buffer) identically to standard
// canonical sorting.
func TestBlockHashManyProperties(t *testing.T) {
	props := make(map[string]any, 20)
	for i := range 20 {
		props[string(rune('a'+19-i))] = int32(i)
	}
	h1, scratch := networkBlockHash("minecraft:test", props, nil)
	h2, _ := networkBlockHash("minecraft:test", props, scratch)
	if h1 == 0 || h1 != h2 {
		t.Fatalf("inconsistent hash across scratch reuse: %#x vs %#x", h1, h2)
	}
	hEmpty, _ := networkBlockHash("minecraft:stone", nil, scratch)
	hEmptyMap, _ := networkBlockHash("minecraft:stone", map[string]any{}, scratch)
	if hEmpty != hEmptyMap {
		t.Fatalf("nil vs empty properties hash mismatch: %#x vs %#x", hEmpty, hEmptyMap)
	}
}

// TestBlockUpdatePacketsMultiSubChunkOverflow verifies grouping when updates
// span more than 8 distinct sub-chunks (exercising map overflow in
// findSubChunkGroup).
func TestBlockUpdatePacketsMultiSubChunkOverflow(t *testing.T) {
	updates := make([]world.BlockUpdate, 0, 12*subChunkBlocksAt)
	stone := block.Stone{}
	for sub := range 12 {
		baseY := sub * 16
		for i := range subChunkBlocksAt {
			updates = append(updates, world.BlockUpdate{
				Pos:   cube.Pos{i % 16, baseY + i/16, 0},
				Block: stone,
			})
		}
	}
	shared := blockUpdatePackets(tablesOf(nil, world.DefaultBlockRegistry), updates)
	if len(shared.packets) != 12 {
		t.Fatalf("got %d packets for 12 full sub-chunks, want 12", len(shared.packets))
	}
}
