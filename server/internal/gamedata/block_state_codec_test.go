package gamedata

import (
	"math"
	"testing"
)

func TestBlockStateCodecRoundTripsEveryState(t *testing.T) {
	for blockIndex, domains := range blockPropertyDomains {
		codec := blockStateCodecs[blockIndex]
		for state := uint32(0); state < codec.count; state++ {
			id := codec.offset + state + 1
			name, properties, ok := BlockStateFromID(id)
			if !ok || name != "minecraft:"+solidBlockNames[blockIndex] {
				t.Fatalf("decode ID %d = (%q, %t), want block %q", id, name, ok, "minecraft:"+solidBlockNames[blockIndex])
			}
			got, err := BlockStateID(name, properties)
			if err != nil || got != id {
				t.Fatalf("round trip ID %d = (%d, %v)", id, got, err)
			}
			if len(properties) != len(domains) {
				t.Fatalf("ID %d decoded %d properties, want %d", id, len(properties), len(domains))
			}
		}
	}
}

func TestBlockStateIDDefaultsAndSourceValues(t *testing.T) {
	id, err := BlockStateID("minecraft:oak_log", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	name, properties, ok := BlockStateFromID(id)
	if !ok || name != "minecraft:oak_log" || properties["axis"] != "y" {
		t.Fatalf("decoded default oak_log state = (%q, %#v, %t)", name, properties, ok)
	}
	enumID, err := BlockStateID("oak_log", map[string]any{"axis": "x"})
	if err != nil {
		t.Fatal(err)
	}
	enumRoundTrip, err := BlockStateID("oak_log", map[string]any{"axis": "x"})
	if err != nil || enumRoundTrip != enumID {
		t.Fatalf("canonical enum ID = %d, %v; want %d", enumRoundTrip, err, enumID)
	}
	boolID, err := BlockStateID("oak_door", map[string]any{"facing": "east", "half": "upper", "hinge": "right", "open": "true", "powered": true})
	if err != nil || boolID == 0 {
		t.Fatalf("valid bool/enum source values: ID %d, %v", boolID, err)
	}
	intID, err := BlockStateID("snow", map[string]any{"layers": "8"})
	if err != nil || intID == 0 {
		t.Fatalf("valid integer source value at upper boundary: ID %d, %v", intID, err)
	}
	canonicalIntID, err := BlockStateID("snow", map[string]any{"layers": int32(8)})
	if err != nil || canonicalIntID != intID {
		t.Fatalf("canonical integer ID = %d, %v; want %d", canonicalIntID, err, intID)
	}
}

func TestBlockStateIDNormalizesAdapterScalars(t *testing.T) {
	base, err := BlockStateID("snow", map[string]any{"layers": int32(4)})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{int(4), int8(4), int16(4), int64(4), uint(4), uint8(4), uint16(4), uint32(4), uint64(4), float64(4), "4"} {
		id, err := BlockStateID("snow", map[string]any{"layers": value})
		if err != nil || id != base {
			t.Errorf("layers %#v = (%d, %v), want canonical ID %d", value, id, err, base)
		}
	}
	boolBase, err := BlockStateID("oak_door", map[string]any{"powered": false})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{uint8(0), "false"} {
		id, err := BlockStateID("oak_door", map[string]any{"powered": value})
		if err != nil || id != boolBase {
			t.Errorf("powered %#v = (%d, %v), want canonical ID %d", value, id, err, boolBase)
		}
	}
	for _, value := range []any{float64(4.5), math.NaN(), math.Inf(1), uint64(^uint64(0)), int64(^uint64(0) >> 1)} {
		id, err := BlockStateID("snow", map[string]any{"layers": value})
		if err == nil || id != 0 {
			t.Errorf("layers %#v = (%d, %v), want error", value, id, err)
		}
	}
	id, err := BlockStateID("oak_door", map[string]any{"powered": uint8(2)})
	if err == nil || id != 0 {
		t.Errorf("powered uint8(2) = (%d, %v), want error", id, err)
	}
}
func TestBlockStateCodecRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props map[string]any
	}{
		{"not_a_block", nil},
		{"stone", map[string]any{"unknown": "x"}},
		{"oak_log", map[string]any{"axis": "bad"}},
		{"oak_door", map[string]any{"powered": "yes"}},
		{"oak_log", map[string]any{"axis": 1}},
	} {
		if id, err := BlockStateID(tc.name, tc.props); err == nil || id != 0 {
			t.Errorf("BlockStateID(%q, %#v) = (%d, %v), want error", tc.name, tc.props, id, err)
		}
	}
	for _, id := range []uint32{0, blockStateCount + 1} {
		if name, properties, ok := BlockStateFromID(id); ok || name != "" || properties != nil {
			t.Errorf("BlockStateFromID(%d) = (%q, %#v, %t), want absent", id, name, properties, ok)
		}
	}
}

func TestBlockStateIDsAreDenseAndDistinct(t *testing.T) {
	if blockStateCount == 0 {
		t.Fatal("empty block state catalog")
	}
	for id := uint32(1); id <= blockStateCount; id++ {
		if _, _, ok := BlockStateFromID(id); !ok {
			t.Fatalf("missing dense ID %d", id)
		}
	}
}
