package main

import (
	"encoding/binary"
	"testing"
)

func TestSubChunkDimensionBedrockKeyLayouts(t *testing.T) {
	for _, test := range []struct {
		name string
		key  []byte
		want string
	}{
		{name: "overworld", key: []byte{1, 0, 0, 0, 2, 0, 0, 0, '/', 0x80}, want: "overworld"},
		{name: "nether", key: dimensionKey(1), want: "nether"},
		{name: "end", key: dimensionKey(2), want: "end"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := subChunkDimension(test.key)
			if !ok || got != test.want {
				t.Fatalf("subChunkDimension(%x) = (%v, %v), want (%v, true)", test.key, got, ok, test.want)
			}
		})
	}
	if _, ok := subChunkDimension([]byte{1, 0, 0, 0, 2, 0, 0, 0, 'x', 0}); ok {
		t.Fatal("accepted a non-subchunk tag")
	}
}

func dimensionKey(id uint32) []byte {
	key := make([]byte, 14)
	binary.LittleEndian.PutUint32(key, 1)
	binary.LittleEndian.PutUint32(key[4:], 2)
	binary.LittleEndian.PutUint32(key[8:], id)
	key[12] = '/'
	key[13] = 0x80
	return key
}
