package block

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/dm-vev/adamant/net/nbt"
)

// oldWorldProvider holds the columns of a world saved before 1.26.60, as
// disk data, and loads nothing else.
type oldWorldProvider struct {
	world.NopProvider
	columns map[world.ChunkPos]chunk.SerialisedData
}

func (p *oldWorldProvider) LoadColumn(pos world.ChunkPos, dim world.Dimension) (*chunk.Column, error) {
	data, ok := p.columns[pos]
	if !ok {
		return nil, leveldb.ErrNotFound
	}
	c, err := chunk.DiskDecode(world.DefaultBlockRegistry, data, dim.Range())
	if err != nil {
		return nil, err
	}
	return &chunk.Column{Chunk: c}, nil
}

func (p *oldWorldProvider) StoreColumn(world.ChunkPos, world.Dimension, *chunk.Column) error {
	return nil
}

// oldWorldOrigin is the block the scene of oldWorldSubChunk is placed
// around, as cmd/vanilla-probe placed it around the player.
var oldWorldOrigin = cube.Pos{4, 65, 4}

// oldWorldSubChunk returns the sub-chunk from y 64 of chunk (0, 0) of a
// world saved by 1.26.52, as BDS 1.26.52.3 saved the scene of the capture
// of the connection states (session's testdata/connection_states.commands)
// placed around oldWorldOrigin: on a floor of stone, a line of redstone
// wire with a corner and a step up onto a block, wire alone, fire beside a
// log, and a chorus plant with a branch on end stone, each of the states of
// 1.26.50, without their connections. With them are two wires of worlds
// adamant saved: one of 1.26.50 whose Java state, connected north and
// south, the palette entry retains, and one saved with the connections of
// 1.26.60, to no side.
func oldWorldSubChunk(t *testing.T) []byte {
	t.Helper()
	javaWire, err := gamedata.BlockStateID("minecraft:redstone_wire", map[string]any{"north": "side", "south": "side", "east": "none", "west": "none", "power": int32(0)})
	if err != nil {
		t.Fatal(err)
	}
	javaName, javaProps, _ := gamedata.BlockStateFromID(javaWire)
	v := chunk.CurrentBlockVersion
	entries := []map[string]any{
		{"name": "minecraft:air", "states": map[string]any{}, "version": v},
		{"name": "minecraft:stone", "states": map[string]any{}, "version": v},
		{"name": "minecraft:redstone_wire", "states": map[string]any{"redstone_signal": int32(0)}, "version": v},
		{"name": "minecraft:oak_log", "states": map[string]any{"pillar_axis": "y"}, "version": v},
		{"name": "minecraft:fire", "states": map[string]any{"age": int32(0)}, "version": v},
		{"name": "minecraft:end_stone", "states": map[string]any{}, "version": v},
		{"name": "minecraft:chorus_plant", "states": map[string]any{}, "version": v},
		{"name": "minecraft:redstone_wire", "states": map[string]any{"redstone_signal": int32(0)}, "version": v,
			"adamant:java": map[string]any{"name": javaName, "properties": javaProps}},
		{"name": "minecraft:redstone_wire", "states": map[string]any{"redstone_signal": int32(0), "redstone_north": "none",
			"redstone_east": "none", "redstone_south": "none", "redstone_west": "none"}, "version": v},
	}
	cells := map[cube.Pos]uint32{
		{2, 0, 0}: 2, {3, 0, 0}: 2, {4, 0, 0}: 2, {4, 0, 1}: 2, {5, 0, 0}: 1, {5, 1, 0}: 2, {2, 0, 6}: 2,
		{0, 0, 4}: 3, {1, 0, 4}: 4,
		{4, -1, 4}: 5, {4, 0, 4}: 6, {4, 1, 4}: 6, {5, 1, 4}: 6,
		{10, 0, 10}: 7, {10, 0, 6}: 8,
	}
	for x := -4; x <= 8; x++ {
		for z := -4; z <= 8; z++ {
			if _, set := cells[cube.Pos{x, -1, z}]; !set {
				cells[cube.Pos{x, -1, z}] = 1
			}
		}
	}
	// Four bits an index: eight indices to a word.
	words := make([]uint32, 512)
	for rel, index := range cells {
		pos := oldWorldOrigin.Add(rel)
		i := (pos[0]&15)<<8 | (pos[2]&15)<<4 | pos[1]&15
		words[i/8] |= index << ((i % 8) * 4)
	}
	var b bytes.Buffer
	b.Write([]byte{chunk.SubChunkVersion, 1, 4, 4 << 1})
	for _, word := range words {
		_ = binary.Write(&b, binary.LittleEndian, word)
	}
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(entries)))
	for _, entry := range entries {
		if err := nbt.NewEncoderWithEncoding(&b, nbt.LittleEndian).Encode(entry); err != nil {
			t.Fatal(err)
		}
	}
	return b.Bytes()
}

// TestOldWorldStatesConnectOnLoad loads a world saved by 1.26.52, whose
// redstone wire, fire and chorus plants have no connections, and finds them
// connected once loaded, as Java places them, without a neighbour changing:
// BDS 1.26.61.1, given the same world saved by 1.26.52.3, saves the same
// connections of the wire and chorus plants once it loaded it (measured
// with copies of both, cmd/vanilla-probe, and the saved worlds read back).
// BDS also gives the fire beside the log the side of the log, where Java's
// FireBlock, on a floor of stone, gives none: the game follows Java. A
// retained Java state of a world adamant saved is taken as it was, and wire
// saved with the connections of 1.26.60 is left as it was, as BDS leaves
// unconnected wire of a world it saved itself.
func TestOldWorldStatesConnectOnLoad(t *testing.T) {
	t.Parallel()
	r := world.Overworld.Range()
	data := chunk.SerialisedData{SubChunks: make([][]byte, (r.Height()>>4)+1)}
	data.SubChunks[(oldWorldOrigin[1]-r.Min())>>4] = oldWorldSubChunk(t)
	provider := &oldWorldProvider{NopProvider: world.NopProvider{WorldData: &world.WorldDataMemory{}},
		columns: map[world.ChunkPos]chunk.SerialisedData{{0, 0}: data}}
	w := world.Config{Provider: provider, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })

	runWorld(w, func(tx *world.Tx) {
		for rel, want := range map[cube.Pos]world.Block{
			{2, 0, 0}:   wire("nsns"),
			{3, 0, 0}:   wire("nsns"),
			{4, 0, 0}:   wire("nuss"),
			{4, 0, 1}:   wire("snsn"),
			{5, 1, 0}:   wire("nsns"),
			{2, 0, 6}:   wire("ssss"),
			{1, 0, 4}:   Fire{},
			{4, 0, 4}:   ChorusPlant{Down: true, Up: true},
			{4, 1, 4}:   ChorusPlant{Down: true, East: true},
			{5, 1, 4}:   ChorusPlant{West: true},
			{10, 0, 10}: wire("snsn"),
			{10, 0, 6}:  wire("nnnn"),
		} {
			if got := tx.Block(oldWorldOrigin.Add(rel)); got != want {
				t.Errorf("at %v of the scene, the world loaded %#v, want %#v", rel, got, want)
			}
		}
	})
}
