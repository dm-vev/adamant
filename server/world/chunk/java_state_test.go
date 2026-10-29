package chunk_test

import (
	"bytes"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/dm-vev/adamant/net/nbt"
)

func TestJavaStateMetadataDiskRoundTripAndCompaction(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 15})
	stone, ok := world.DefaultBlockRegistry.StateToRuntimeID("minecraft:stone", nil)
	if !ok {
		t.Fatal("stone runtime ID missing")
	}
	states := []map[string]any{
		gamedata.BlockPropertiesWithDefaults("minecraft:oak_log", map[string]any{"axis": "x"}),
		gamedata.BlockPropertiesWithDefaults("minecraft:oak_log", map[string]any{"axis": "z"}),
	}
	ids := make([]uint32, len(states))
	for i, properties := range states {
		var err error
		ids[i], err = gamedata.BlockStateID("minecraft:oak_log", properties)
		if err != nil {
			t.Fatal(err)
		}
	}
	c.SetBlock(1, 2, 3, 0, stone)
	c.SetBlock(2, 2, 3, 0, stone)
	c.SetJavaStateID(1, 2, 3, ids[0])
	c.SetJavaStateID(2, 2, 3, ids[1])
	c.Compact()

	decoded, err := chunk.DiskDecode(world.DefaultBlockRegistry, chunk.Encode(c, chunk.DiskEncoding), cube.Range{0, 15})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Block(1, 2, 3, 0) != stone || decoded.Block(2, 2, 3, 0) != stone {
		t.Fatal("disk round trip changed native runtime IDs")
	}
	if decoded.StateID(1, 2, 3) != ids[0] || decoded.StateID(2, 2, 3) != ids[1] {
		t.Fatalf("disk round trip lost distinct source states: %d, %d", decoded.StateID(1, 2, 3), decoded.StateID(2, 2, 3))
	}
	clone := decoded.Clone()
	if !clone.Equals(decoded) {
		t.Fatal("cloned chunk differs from source metadata chunk")
	}
	clone.SetJavaStateID(1, 2, 3, ids[1])
	if clone.Equals(decoded) {
		t.Fatal("chunk equality ignored different source Java metadata")
	}
	if decoded.StateID(1, 2, 3) != ids[0] {
		t.Fatal("changing cloned source metadata changed the original chunk")
	}
}

func TestNativeWritesClearOnlyAffectedJavaMetadata(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 15})
	stone, ok := world.DefaultBlockRegistry.StateToRuntimeID("minecraft:stone", nil)
	if !ok {
		t.Fatal("stone runtime ID missing")
	}
	id, err := gamedata.BlockStateID("minecraft:oak_log", gamedata.BlockPropertiesWithDefaults("minecraft:oak_log", map[string]any{"axis": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	c.SetBlock(1, 2, 3, 0, stone)
	c.SetBlock(2, 2, 3, 0, stone)
	c.SetJavaStateID(1, 2, 3, id)
	c.SetJavaStateID(2, 2, 3, id)

	c.SetBlock(1, 2, 3, 0, stone)
	if c.StateID(1, 2, 3) != 0 || c.StateID(2, 2, 3) != id {
		t.Fatal("native primary write did not clear only the affected cell metadata")
	}
	c.SetJavaStateID(1, 2, 3, id)
	// Clearing an absent liquid layer must invalidate metadata even though the
	// native write otherwise takes the existing-air fast path.
	c.SetBlock(1, 2, 3, 1, world.DefaultBlockRegistry.AirRuntimeID())
	if c.StateID(1, 2, 3) != 0 {
		t.Fatal("liquid-layer native write retained primary source metadata")
	}
}

func TestMissingNativePropertyCanonicalizationPreservesJavaMetadata(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 15})
	native, ok := world.DefaultBlockRegistry.StateToRuntimeID("minecraft:oak_log", map[string]any{"pillar_axis": "y"})
	if !ok {
		t.Fatal("oak log runtime ID missing")
	}
	id, err := gamedata.BlockStateID("minecraft:oak_log", gamedata.BlockPropertiesWithDefaults("minecraft:oak_log", map[string]any{"axis": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	c.SetBlock(4, 2, 5, 0, native)
	c.SetJavaStateID(4, 2, 5, id)
	data := chunk.Encode(c, chunk.DiskEncoding)
	var nativePalette bytes.Buffer
	nativePalette.Write([]byte{chunk.SubChunkVersion, 1, 0, 0})
	if err := nbt.NewEncoderWithEncoding(&nativePalette, nbt.LittleEndian).Encode(map[string]any{
		"name": "minecraft:oak_log", "states": map[string]any{}, "version": chunk.CurrentBlockVersion,
		"adamant:java": map[string]any{"name": "minecraft:oak_log", "properties": map[string]any{"axis": "x"}},
	}); err != nil {
		t.Fatal(err)
	}
	data.SubChunks[0] = nativePalette.Bytes()
	c, err = chunk.DiskDecode(world.DefaultBlockRegistry, data, cube.Range{0, 15})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.MissingBlockStates()) == 0 {
		t.Fatal("test setup did not produce a missing native property")
	}
	if !c.CanonicalizeMissingBlockState(cube.Pos{4, 2, 5}, 0) {
		t.Fatal("failed to canonicalize missing native property")
	}
	if c.StateID(4, 2, 5) != id {
		t.Fatal("canonicalization discarded source Java metadata")
	}
}

func TestNetworkRoundTripOmitsJavaMetadata(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 15})
	stone, ok := world.DefaultBlockRegistry.StateToRuntimeID("minecraft:stone", nil)
	if !ok {
		t.Fatal("stone runtime ID missing")
	}
	id, err := gamedata.BlockStateID("minecraft:oak_log", gamedata.BlockPropertiesWithDefaults("minecraft:oak_log", map[string]any{"axis": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	c.SetBlock(1, 2, 3, 0, stone)
	c.SetJavaStateID(1, 2, 3, id)
	data := chunk.Encode(c, chunk.NetworkEncoding)
	var payload []byte
	for _, sub := range data.SubChunks {
		payload = append(payload, sub...)
	}
	payload = append(payload, data.Biomes...)
	decoded, err := chunk.NetworkDecode(world.DefaultBlockRegistry, payload, len(data.SubChunks), cube.Range{0, 15})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Block(1, 2, 3, 0) != stone {
		t.Fatal("network round trip changed native runtime ID")
	}
	if decoded.StateID(1, 2, 3) != 0 {
		t.Fatal("network decoding produced source Java metadata")
	}
}
