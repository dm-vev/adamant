package world

import (
	"encoding/binary"
	"slices"
	"testing"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world/chunk"
)

type biomeSearchGenerator struct {
	target  cube.Pos
	samples int
}

func (g *biomeSearchGenerator) GenerateChunk(ChunkPos, *chunk.Chunk, GenerationContext) GenerationResult {
	return GenerationResult{}
}
func (*biomeSearchGenerator) DefaultSpawn(Dimension) cube.Pos { return cube.Pos{0, 64, 0} }
func (g *biomeSearchGenerator) BiomeAt(x, y, z int32) string {
	g.samples++
	pos := cube.Pos{int(x), int(y), int(z)}
	if pos == g.target {
		return "minecraft:target"
	}
	return "minecraft:plains"
}

func TestFindNearestBiomeSamplesGeneratorGrid(t *testing.T) {
	t.Parallel()
	generator := &biomeSearchGenerator{target: cube.Pos{32, 64, 0}}
	w := Config{Generator: generator, Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	pos, found, err := w.FindNearestBiome("target", cube.Pos{0, 64, 0})
	if err != nil {
		t.Fatal(err)
	}
	if !found || pos != generator.target {
		t.Fatalf("nearest biome position=(%v,%t), want %v", pos, found, generator.target)
	}
	if generator.samples == 0 {
		t.Fatal("biome search did not sample its source")
	}
}

type legacyStructRefProvider struct {
	NopProvider
	data map[string][]byte
}

func (p *legacyStructRefProvider) LoadWorldData(ns string) ([]byte, error) {
	if b, ok := p.data[ns]; ok {
		return slices.Clone(b), nil
	}
	return nil, leveldb.ErrNotFound
}

func (p *legacyStructRefProvider) SaveWorldData(ns string, data []byte) error {
	if data == nil {
		delete(p.data, ns)
		return nil
	}
	p.data[ns] = slices.Clone(data)
	return nil
}

func TestLegacyStructureReferencesFallback(t *testing.T) {
	t.Parallel()
	structID := "minecraft:fortress"
	payload := make([]byte, 12+14+len(structID))
	copy(payload, "JSTRREF1")
	binary.LittleEndian.PutUint32(payload[8:], 1)
	binary.LittleEndian.PutUint32(payload[12:], 1)
	binary.LittleEndian.PutUint16(payload[16:], uint16(len(structID)))
	copy(payload[18:], structID)
	off := 18 + len(structID)
	var chunkZ int32 = -2
	binary.LittleEndian.PutUint32(payload[off:], uint32(3))
	binary.LittleEndian.PutUint32(payload[off+4:], uint32(chunkZ))

	refs, err := decodeStructureReferences(payload)
	if err != nil {
		t.Fatal(err)
	}
	wantRef := StructureReference{DimensionID: 1, StructureID: structID, StartChunk: ChunkPos{3, -2}}
	if len(refs) != 1 || refs[0] != wantRef {
		t.Fatalf("decoded legacy JSTRREF1 = %+v, want %+v", refs, wantRef)
	}

	p := &legacyStructRefProvider{
		data: map[string][]byte{legacyStructureReferenceNamespace(1): payload},
	}
	w := Config{Provider: p, Dim: Nether, Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	if err := w.loadStructureReferences(); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.structureReferences[wantRef]; !ok {
		t.Fatal("failed to load structure references from legacy namespace")
	}
}
