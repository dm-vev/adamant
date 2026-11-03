package session_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/dm-vev/adamant/net/nbt"
)

// chunkWireCase is a chunk the encoding of chunks is checked on.
type chunkWireCase struct {
	name string
	c    *chunk.Chunk
}

// chunkWireJavaStateID is the Java state ID the cases attach to blocks. Its
// value does not matter: the network encoding leaves Java state IDs out.
const chunkWireJavaStateID = 4242

// chunkWireCases returns the chunks of the golden world around its spawn and
// chunks made to hold what the encoding has to handle: palettes of each bit
// size, two layers, empty sub-chunks, mixed biomes and equal neighbouring
// biome storages, a biome that is not registered, Java state IDs, and block
// states read from disk with properties missing.
func chunkWireCases(t testing.TB) []chunkWireCase {
	t.Helper()
	br := world.DefaultBlockRegistry
	r := world.Overworld.Range()
	var cases []chunkWireCase

	gen := goldenGenerator()
	for _, pos := range []world.ChunkPos{{0, 0}, {-1, 0}, {0, -1}, {1, 1}} {
		c := chunk.New(br, r)
		gen.GenerateChunk(pos, c, world.GenerationContext{})
		cases = append(cases, chunkWireCase{fmt.Sprintf("golden world column %v", pos), c})
	}

	cases = append(cases, chunkWireCase{"empty", chunk.New(br, r)})

	stone := br.BlockRuntimeID(block.Stone{})
	full := chunk.New(br, r)
	for x := range uint8(16) {
		for z := range uint8(16) {
			for y := range int16(16) {
				full.SetBlock(x, y, z, 0, stone)
			}
		}
	}
	full.Compact()
	cases = append(cases, chunkWireCase{"0 bits: a sub-chunk of stone", full})

	count := uint32(br.BlockCount())
	for _, values := range []int{2, 4, 8, 16, 32, 64, 256, 257, 4096} {
		c := chunk.New(br, r)
		// Air is in the palette already: values-1 other states follow it.
		stride := count / uint32(values)
		for i := 1; i < values; i++ {
			x, z, y := uint8(i>>8), uint8(i>>4&15), int16(i&15)
			c.SetBlock(x, y+16, z, 0, uint32(i)*stride)
		}
		cases = append(cases, chunkWireCase{fmt.Sprintf("%d values in a palette", values), c})
	}

	water := br.BlockRuntimeID(block.Water{Depth: 8, Still: true})
	layers := chunk.New(br, r)
	for i := range 64 {
		x, z := uint8(i&15), uint8(i>>4)
		layers.SetBlock(x, 0, z, 0, stone)
		layers.SetBlock(x, 1, z, 1, water)
	}
	cases = append(cases, chunkWireCase{"two layers", layers})

	biomes := chunk.New(br, r)
	ids := world.BiomeIDs()
	for i := range 48 {
		x, z, y := uint8(i&15), uint8(i>>2&15), int16(i*7%384)+int16(r.Min())
		biomes.SetBiome(x, y, z, uint32(ids[i*5%len(ids)]))
	}
	// Equal storages on neighbouring sub-chunks, not equal to the one below.
	for y := int16(64); y < 96; y++ {
		biomes.SetBiome(3, y, 4, uint32(ids[len(ids)-1]))
		biomes.SetBiome(5, y, 6, 1)
	}
	// An ID no biome is registered with.
	biomes.SetBiome(9, 200, 9, 60000)
	cases = append(cases, chunkWireCase{"mixed biomes", biomes})

	java := chunk.New(br, r)
	for i := range 32 {
		x, z, y := uint8(i&15), uint8(i>>4), int16(i%5)
		java.SetBlock(x, y, z, 0, stone)
		if i%3 == 0 {
			java.SetJavaStateID(x, y, z, chunkWireJavaStateID)
		}
	}
	cases = append(cases, chunkWireCase{"Java state IDs", java})

	cases = append(cases, chunkWireCase{"block states missing properties", missingStatesChunk(t)})
	return cases
}

// missingStatesChunk returns a chunk read from disk whose palette holds a log
// without its axis, which the chunk records as a state missing a property.
func missingStatesChunk(t testing.TB) *chunk.Chunk {
	t.Helper()
	r := world.Overworld.Range()
	ind := 5
	sub := []byte{chunk.SubChunkVersion, 1, uint8(ind + r[0]>>4), 1 << 1}
	words := make([]byte, 128*4)
	for i := range words {
		words[i] = byte(i * 37)
	}
	sub = append(sub, words...)
	sub = binary.LittleEndian.AppendUint32(sub, 2)
	buf := bytes.NewBuffer(sub)
	enc := nbt.NewEncoderWithEncoding(buf, nbt.LittleEndian)
	for _, entry := range []map[string]any{
		{"name": "minecraft:air", "states": map[string]any{}, "version": chunk.CurrentBlockVersion},
		{"name": "minecraft:oak_log", "states": map[string]any{}, "version": chunk.CurrentBlockVersion},
	} {
		if err := enc.Encode(entry); err != nil {
			t.Fatal(err)
		}
	}
	data := chunk.SerialisedData{SubChunks: make([][]byte, ind+1)}
	data.SubChunks[ind] = buf.Bytes()
	c, err := chunk.DiskDecode(world.DefaultBlockRegistry, data, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.MissingBlockStates()) == 0 {
		t.Fatal("the chunk records no block state missing a property")
	}
	return c
}

// compareChunkWire compares what the session sends of a chunk with what
// chunk.NetworkEncoding encodes of it.
func compareChunkWire(t *testing.T, name string, c *chunk.Chunk) {
	t.Helper()
	for i := range c.Sub() {
		got, want := session.EncodeSubChunk(c, i), chunk.EncodeSubChunk(c, chunk.NetworkEncoding, i)
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: sub-chunk %d encodes to\n%x\nchunk.NetworkEncoding to\n%x", name, i, got, want)
		}
	}
	got, want := session.EncodeBiomes(c), chunk.EncodeBiomes(c, chunk.NetworkEncoding)
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: biomes encode to\n%x\nchunk.NetworkEncoding to\n%x", name, got, want)
	}
}

// TestChunkWireMatchesNetworkEncoding checks the session encodes chunks to
// the bytes chunk.NetworkEncoding encodes them to.
func TestChunkWireMatchesNetworkEncoding(t *testing.T) {
	sizes := map[uint16]bool{}
	for _, tc := range chunkWireCases(t) {
		compareChunkWire(t, tc.name, tc.c)
		for _, sub := range tc.c.Sub() {
			for _, storage := range sub.Layers() {
				_, bits := storage.Indices()
				sizes[bits] = true
			}
		}
	}
	for _, bits := range []uint16{0, 1, 2, 3, 4, 5, 6, 8, 16} {
		if !sizes[bits] {
			t.Errorf("no case has a block storage of %d bits an index", bits)
		}
	}
}

// FuzzChunkWire compares the encoding of the session with
// chunk.NetworkEncoding on chunks the fuzzed bytes place blocks, biomes and
// Java state IDs in. Every five bytes are an operation: its kind, the X and
// Z of a column, a Y and a value of two bytes.
func FuzzChunkWire(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0x12, 40, 1, 0, 1, 0x34, 41, 2, 0, 2, 0x56, 42, 3, 0, 3, 0x12, 40, 9, 0})
	f.Add([]byte{2, 0, 0, 0, 0, 2, 0xff, 255, 255, 0, 2, 0x11, 100, 7, 0, 4, 0, 0, 0, 0})
	var many []byte
	for i := range 300 {
		many = append(many, 0, byte(i), byte(i>>8), byte(i*13), byte(i*13>>8))
	}
	f.Add(many)

	br := world.DefaultBlockRegistry
	r := world.Overworld.Range()
	count := br.BlockCount()
	ids := world.BiomeIDs()
	f.Fuzz(func(t *testing.T, ops []byte) {
		c := chunk.New(br, r)
		for ; len(ops) >= 5; ops = ops[5:] {
			x, z := ops[1]&15, ops[1]>>4
			y := int16(int(ops[2])*3/2 + r.Min())
			v := uint32(ops[3]) | uint32(ops[4])<<8
			switch ops[0] % 6 {
			case 0:
				c.SetBlock(x, y, z, 0, v%uint32(count))
			case 1:
				c.SetBlock(x, y, z, 1, v%uint32(count))
			case 2:
				if ops[3] == 0xff {
					// An ID no biome is registered with.
					c.SetBiome(x, y, z, 60000+v)
					continue
				}
				c.SetBiome(x, y, z, uint32(ids[int(v)%len(ids)]))
			case 3:
				c.SetJavaStateID(x, y, z, v+1)
			case 4:
				c.Compact()
			case 5:
				y = min(y, int16(r.Max())-15)
				for dy := range int16(16) {
					c.SetBiome(x, y+dy, z, uint32(ids[int(v)%len(ids)]))
				}
			}
		}
		compareChunkWire(t, "fuzzed chunk", c)
	})
}

// TestGoldenChunkWire records what the session sends of the chunks of
// chunkWireCases in testdata/golden/chunk_wire.snapshot, so that the bytes
// stay held once nothing else encodes chunks for the network.
func TestGoldenChunkWire(t *testing.T) {
	if isolated(t) {
		return
	}
	var b strings.Builder
	for _, tc := range chunkWireCases(t) {
		fmt.Fprintf(&b, "== %s\n", tc.name)
		for i := range tc.c.Sub() {
			fmt.Fprintf(&b, "sub-chunk %d: %s\n", i, hex.EncodeToString(session.EncodeSubChunk(tc.c, i)))
		}
		fmt.Fprintf(&b, "biomes: %s\n", hex.EncodeToString(session.EncodeBiomes(tc.c)))
	}
	path := filepath.Join("testdata", "golden", "chunk_wire.snapshot")
	got := b.String()
	if *updateSnapshots {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v: run with -update-snapshots to write it", err)
	}
	if got != string(want) {
		t.Errorf("the chunks encode differently from %s (run with -update-snapshots if the change is meant):\n%s", path, snapshotDiff(string(want), got))
	}
}
