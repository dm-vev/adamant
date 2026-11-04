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

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
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

	// The states of the palettes are taken at a stride from those the
	// clients of the default protocol know, by their hashes, so that the
	// states of the game the protocol does not know, and the order the
	// registry holds the states in, do not change them.
	states := session.StatesOfDefault(br)
	count := len(states)
	for _, values := range []int{2, 4, 8, 16, 32, 64, 256, 257, 4096} {
		c := chunk.New(br, r)
		// Air is in the palette already: values-1 other states follow it.
		stride := count / values
		for i := 1; i < values; i++ {
			x, z, y := uint8(i>>8), uint8(i>>4&15), int16(i&15)
			c.SetBlock(x, y+16, z, 0, states[i*stride])
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

// TestChunkWireCasesCoverPaletteSizes checks the chunks chunk_wire.snapshot
// records hold block storages of every size of index the world format has.
func TestChunkWireCasesCoverPaletteSizes(t *testing.T) {
	sizes := map[uint16]bool{}
	for _, tc := range chunkWireCases(t) {
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

// TestChunkWireOmitsJavaMetadata checks the Java state ID of a block does not
// reach Bedrock clients: compacted, a chunk with one set encodes to the bytes
// of the same chunk without it.
func TestChunkWireOmitsJavaMetadata(t *testing.T) {
	br := world.DefaultBlockRegistry
	r := cube.Range{0, 15}
	stone := br.BlockRuntimeID(block.Stone{})
	with, without := chunk.New(br, r), chunk.New(br, r)
	for _, c := range []*chunk.Chunk{with, without} {
		c.SetBlock(1, 2, 3, 0, stone)
	}
	with.SetJavaStateID(1, 2, 3, chunkWireJavaStateID)
	for _, c := range []*chunk.Chunk{with, without} {
		c.Compact()
	}
	if with.StateID(1, 2, 3) != chunkWireJavaStateID {
		t.Fatal("the chunk lost the Java state ID set")
	}
	if got, want := session.EncodeSubChunk(with, 0), session.EncodeSubChunk(without, 0); !bytes.Equal(got, want) {
		t.Fatalf("with a Java state ID, the sub-chunk encodes to\n%x\nwithout to\n%x", got, want)
	}
	if got, want := session.EncodeBiomes(with), session.EncodeBiomes(without); !bytes.Equal(got, want) {
		t.Fatalf("with a Java state ID, the biomes encode to\n%x\nwithout to\n%x", got, want)
	}
}

// TestGoldenChunkWire compares what the session sends of the chunks of
// chunkWireCases with testdata/golden/chunk_wire.snapshot, recorded when
// the bytes were checked against the network encoding the chunk package
// had.
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
