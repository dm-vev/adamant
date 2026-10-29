package main

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/dm-vev/adamant/net/nbt"
)

func TestDecodeSubChunk9PaletteWidthsAndNegativeY(t *testing.T) {
	data := subChunkFixture(t, 0x80, []paletteFixture{{bits: 0, names: []string{"minecraft:air"}}, {bits: 3, names: []string{"minecraft:stone", "minecraft:dirt"}}})
	got, err := decodeSubChunk9(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d palette entries, want 3", len(got))
	}
	if got[0].Name != "minecraft:air" || got[1].Name != "minecraft:stone" || got[2].Name != "minecraft:dirt" {
		t.Fatalf("unexpected palette order: %#v", got)
	}
	for _, state := range got {
		if state.Version != 18040335 {
			t.Fatalf("version = %d, want persisted value 18040335", state.Version)
		}
	}
}

func TestDecodeSubChunk9RejectsTruncatedPackedWords(t *testing.T) {
	data := []byte{9, 1, 0xfe, 2}
	_, err := decodeSubChunk9(data)
	if err == nil {
		t.Fatalf("error = %v, want packed-word truncation", err)
	}
}

func TestDecodeSubChunk9RejectsTruncatedPalette(t *testing.T) {
	data := []byte{9, 1, 0xfe, 0}
	_, err := decodeSubChunk9(data)
	if err == nil {
		t.Fatalf("error = %v, want NBT truncation", err)
	}
}

type paletteFixture struct {
	bits  byte
	names []string
}

func subChunkFixture(t *testing.T, y byte, palettes []paletteFixture) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteByte(9)
	b.WriteByte(byte(len(palettes)))
	b.WriteByte(y)
	for _, palette := range palettes {
		b.WriteByte(palette.bits << 1)
		if palette.bits != 0 {
			wordsPerWord := 32 / int(palette.bits)
			wordCount := (4096 + wordsPerWord - 1) / wordsPerWord
			for range wordCount {
				_ = binary.Write(&b, binary.LittleEndian, uint32(0))
			}
			_ = binary.Write(&b, binary.LittleEndian, uint32(len(palette.names)))
		}
		for _, name := range palette.names {
			entry := map[string]any{
				"name":    name,
				"states":  map[string]any{"active": uint8(1)},
				"version": int32(18040335),
			}
			if err := nbt.NewEncoderWithEncoding(&b, nbt.LittleEndian).Encode(entry); err != nil {
				t.Fatal(err)
			}
		}
	}
	return b.Bytes()
}
