package nbt

import (
	"bytes"
	"testing"
)

func benchEntityMap() map[string]any {
	return map[string]any{
		"identifier":     "minecraft:villager_v2",
		"UniqueID":       int64(-4294967291),
		"CustomName":     "Librarian",
		"Pos":            []float32{128.5, 64.0, -312.25},
		"Motion":         []float32{0.01, -0.0784, -0.02},
		"Rotation":       []float32{135.5, -12.25},
		"FallDistance":   float32(0.0),
		"Fire":           int16(0),
		"Air":            int16(300),
		"HurtTime":       int16(0),
		"DeathTime":      int16(0),
		"Age":            int32(24000),
		"PortalCooldown": int32(0),
		"Variant":        int32(5),
		"MarkVariant":    int32(2),
		"SkinID":         int32(0),
		"OnGround":       byte(1),
		"Persistent":     byte(1),
		"IsAutonomous":   byte(0),
		"Invulnerable":   byte(0),
		"NaturalSpawn":   byte(1),
		"Tags":           []string{"village_center", "traded", "chunk_saved"},
		"UUIDBits":       [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		"ChunkRefs":      [4]int32{12, -3, 13, -3},
		"SeedHistory":    [2]int64{1234567890123, -9876543210987},
		"EmptyList":      []any{},
		"Attributes": []any{
			map[string]any{
				"Name":       "minecraft:health",
				"Base":       float32(20),
				"Current":    float32(20),
				"Max":        float32(20),
				"Min":        float32(0),
				"DefaultMax": float32(20),
				"DefaultMin": float32(0),
			},
			map[string]any{
				"Name":       "minecraft:movement",
				"Base":       float32(0.5),
				"Current":    float32(0.5),
				"Max":        float32(3.4028235e+38),
				"Min":        float32(0),
				"DefaultMax": float32(3.4028235e+38),
				"DefaultMin": float32(0),
			},
			map[string]any{
				"Name":       "minecraft:follow_range",
				"Base":       float32(128),
				"Current":    float32(128),
				"Max":        float32(2048),
				"Min":        float32(0),
				"DefaultMax": float32(2048),
				"DefaultMin": float32(0),
			},
		},
		"Armor": []any{
			map[string]any{"Name": "", "Count": byte(0), "Damage": int16(0), "WasPickedUp": byte(0)},
			map[string]any{
				"Name":        "minecraft:diamond_chestplate",
				"Count":       byte(1),
				"Damage":      int16(14),
				"WasPickedUp": byte(0),
				"tag": map[string]any{
					"Damage": int32(14),
					"display": map[string]any{
						"Name": "Guard Plate",
					},
					"ench": []any{
						map[string]any{"id": int16(0), "lvl": int16(4)},
						map[string]any{"id": int16(17), "lvl": int16(3)},
					},
				},
			},
		},
	}
}

func benchBlockEntityMap() map[string]any {
	items := make([]any, 0, 12)
	for i := range 12 {
		item := map[string]any{
			"Slot":        byte(i),
			"Name":        "minecraft:iron_ingot",
			"Count":       byte(16 + i),
			"Damage":      int16(0),
			"WasPickedUp": byte(0),
		}
		if i%3 == 0 {
			item["Name"] = "minecraft:enchanted_book"
			item["Count"] = byte(1)
			item["tag"] = map[string]any{
				"ench": []any{
					map[string]any{"id": int16(9), "lvl": int16(5)},
					map[string]any{"id": int16(17), "lvl": int16(3)},
				},
			}
		}
		items = append(items, item)
	}
	return map[string]any{
		"id":         "Chest",
		"x":          int32(128),
		"y":          int32(64),
		"z":          int32(-312),
		"isMovable":  byte(1),
		"Findable":   byte(0),
		"CustomName": "Storage #4",
		"Items":      items,
	}
}

type BenchBaseMeta struct {
	ID      string `nbt:"id"`
	Version int32  `nbt:"version"`
}

type BenchPosMeta struct {
	BenchBaseMeta
	X int32 `nbt:"x"`
	Y int32 `nbt:"y"`
	Z int32 `nbt:"z"`
}

type benchEnchant struct {
	ID    int16 `nbt:"id"`
	Level int16 `nbt:"lvl"`
}

type benchItemStruct struct {
	Slot       byte           `nbt:"Slot"`
	Name       string         `nbt:"Name"`
	Count      byte           `nbt:"Count"`
	Damage     int16          `nbt:"Damage,omitempty"`
	CustomName string         `nbt:"CustomName,omitempty"`
	Enchants   []benchEnchant `nbt:"ench,omitempty"`
	Extra      map[string]any `nbt:"extra,omitempty"`
	ignored    int32
	SkippedTag string `nbt:"-"`
}

type benchBlockEntityStruct struct {
	BenchPosMeta
	Movable    bool              `nbt:"isMovable"`
	Findable   bool              `nbt:"Findable,omitempty"`
	CustomName string            `nbt:"CustomName,omitempty"`
	EmptyNote  string            `nbt:"EmptyNote,omitempty"`
	EmptyCount int32             `nbt:"EmptyCount,omitempty"`
	Pos        []float32         `nbt:"Pos"`
	ChunkRefs  [4]int32          `nbt:"ChunkRefs"`
	Hash       [8]byte           `nbt:"Hash"`
	Items      []benchItemStruct `nbt:"Items"`
	ExtraData  map[string]any    `nbt:"ExtraData,omitempty"`
}

func benchStructSample() benchBlockEntityStruct {
	return benchBlockEntityStruct{
		BenchPosMeta: BenchPosMeta{
			BenchBaseMeta: BenchBaseMeta{ID: "Chest", Version: 18163713},
			X:             128,
			Y:             64,
			Z:             -312,
		},
		Movable:    true,
		CustomName: "Storage #4",
		Pos:        []float32{128.5, 64.0, -312.5},
		ChunkRefs:  [4]int32{8, -20, 9, -20},
		Hash:       [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		Items: []benchItemStruct{
			{Slot: 0, Name: "minecraft:diamond_sword", Count: 1, Damage: 12, CustomName: "Edge", Enchants: []benchEnchant{{ID: 9, Level: 5}, {ID: 17, Level: 3}}},
			{Slot: 1, Name: "minecraft:cobblestone", Count: 64},
			{Slot: 2, Name: "minecraft:torch", Count: 32},
			{Slot: 3, Name: "minecraft:cooked_beef", Count: 16},
			{Slot: 4, Name: "minecraft:oak_planks", Count: 64, Extra: map[string]any{"source": "craft", "tier": int32(1)}},
		},
		ExtraData: map[string]any{
			"owner": "player-1",
			"locks": []int32{1, 2, 3, 4},
		},
	}
}

var benchEncodings = []struct {
	name string
	enc  Encoding
}{
	{"LittleEndian", LittleEndian},
	{"NetworkLittleEndian", NetworkLittleEndian},
	{"BigEndian", BigEndian},
}

func BenchmarkMarshalEntityMap(b *testing.B) {
	entity := benchEntityMap()
	for _, tc := range benchEncodings {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				out, err := MarshalEncoding(entity, tc.enc)
				if err != nil {
					b.Fatal(err)
				}
				if len(out) == 0 {
					b.Fatal("empty")
				}
			}
		})
	}
}

func BenchmarkUnmarshalEntityMap(b *testing.B) {
	entity := benchEntityMap()
	for _, tc := range benchEncodings {
		data, err := MarshalEncoding(entity, tc.enc)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				var out map[string]any
				if err := UnmarshalEncoding(data, &out, tc.enc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMarshalBlockEntityMap(b *testing.B) {
	be := benchBlockEntityMap()
	for _, tc := range benchEncodings {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				out, err := MarshalEncoding(be, tc.enc)
				if err != nil {
					b.Fatal(err)
				}
				if len(out) == 0 {
					b.Fatal("empty")
				}
			}
		})
	}
}

func BenchmarkUnmarshalBlockEntityMap(b *testing.B) {
	be := benchBlockEntityMap()
	for _, tc := range benchEncodings {
		data, err := MarshalEncoding(be, tc.enc)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				var out map[string]any
				if err := UnmarshalEncoding(data, &out, tc.enc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMarshalStruct(b *testing.B) {
	sample := benchStructSample()
	for _, tc := range benchEncodings {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				out, err := MarshalEncoding(&sample, tc.enc)
				if err != nil {
					b.Fatal(err)
				}
				if len(out) == 0 {
					b.Fatal("empty")
				}
			}
		})
	}
}

func BenchmarkUnmarshalStruct(b *testing.B) {
	sample := benchStructSample()
	for _, tc := range benchEncodings {
		data, err := MarshalEncoding(&sample, tc.enc)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				var out benchBlockEntityStruct
				if err := UnmarshalEncoding(data, &out, tc.enc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkEncoderDecoderBuffer(b *testing.B) {
	entity := benchEntityMap()
	for _, tc := range benchEncodings {
		var encoded bytes.Buffer
		if err := NewEncoderWithEncoding(&encoded, tc.enc).Encode(entity); err != nil {
			b.Fatal(err)
		}
		raw := encoded.Bytes()

		b.Run("Encode/"+tc.name, func(b *testing.B) {
			var buf bytes.Buffer
			buf.Grow(len(raw))
			b.ReportAllocs()
			for b.Loop() {
				buf.Reset()
				if err := NewEncoderWithEncoding(&buf, tc.enc).Encode(entity); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("Decode/"+tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for b.Loop() {
				var out map[string]any
				if err := NewDecoderWithEncoding(bytes.NewBuffer(raw), tc.enc).Decode(&out); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
