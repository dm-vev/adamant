package world

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

const migrationTestBlockName = "test:state_migration_connection"

// migrationTestBlock is a minimal opted-in block whose east property is derived from its east neighbour.
type migrationTestBlock struct{ east bool }

func (b migrationTestBlock) EncodeBlock() (string, map[string]any) {
	return migrationTestBlockName, map[string]any{"minecraft:connection_east": b.east}
}
func (b migrationTestBlock) Hash() (uint64, uint64) {
	if b.east {
		return 1 << 60, 1
	}
	return 1 << 60, 0
}
func (migrationTestBlock) Model() BlockModel        { return nil }
func (migrationTestBlock) ReconstructionOrder() int { return 0 }
func (b migrationTestBlock) ReconstructMissingState(pos cube.Pos, missing []string, blocks BlockSource) Block {
	for _, property := range missing {
		if property == "minecraft:connection_east" {
			eastName, _ := blocks.Block(pos.Side(cube.FaceEast)).EncodeBlock()
			return migrationTestBlock{east: eastName == "minecraft:stone"}
		}
	}
	return b
}
func (migrationTestBlock) DerivedStateProperties() []string {
	return []string{"minecraft:connection_east"}
}

type migrationTestGenerator struct {
	blockRID uint32
	stoneRID uint32
}

func (g migrationTestGenerator) GenerateChunk(pos ChunkPos, c *chunk.Chunk) {
	if pos == (ChunkPos{0, 0}) {
		c.SetBlock(15, 0, 8, 0, g.blockRID)
	} else if pos == (ChunkPos{1, 0}) {
		c.SetBlock(0, 0, 8, 0, g.stoneRID)
	}
}
func (migrationTestGenerator) DefaultSpawn(Dimension) cube.Pos { return cube.Pos{} }

func TestGeneratedDerivedBlockStatesAreReconstructedAfterNeighbourReadiness(t *testing.T) {
	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: migrationTestBlockName, Properties: map[string]any{"minecraft:connection_east": false}})
	registry.RegisterBlockState(BlockState{Name: migrationTestBlockName, Properties: map[string]any{"minecraft:connection_east": true}})
	registry.RegisterBlock(migrationTestBlock{})
	registry.RegisterBlock(migrationTestBlock{east: true})
	registry.Finalize()
	blockRID, _ := registry.StateToRuntimeID(migrationTestBlockName, nil)
	stoneRID, _ := registry.StateToRuntimeID("minecraft:stone", nil)
	w := Config{
		Blocks: registry, Generator: migrationTestGenerator{blockRID: blockRID, stoneRID: stoneRID},
		Synchronous: true,
	}.New()
	t.Cleanup(func() { _ = w.Close() })

	<-w.Exec(func(tx *Tx) {
		tx.chunk(ChunkPos{0, 0})
		if got := tx.Block(cube.Pos{15, 0, 8}).(migrationTestBlock); got.east {
			t.Fatal("generated derived state was reconstructed before its east chunk was ready")
		}
		col := w.chunks[ChunkPos{0, 0}]
		if pending := col.missingBlockStates; len(pending) != 1 {
			t.Fatalf("generated pending states = %d, want one opted-in state", len(pending))
		}
		reloaded, err := chunk.DiskDecode(registry, chunk.Encode(col.Chunk, chunk.DiskEncoding), w.ra)
		if err != nil {
			t.Fatal(err)
		}
		if pending := reloaded.MissingBlockStates(); len(pending) != 1 {
			t.Fatalf("generated unresolved state did not persist for reconstruction: %#v", pending)
		}
	})
	<-w.Exec(func(tx *Tx) { tx.chunk(ChunkPos{1, 0}) })
	<-w.Exec(func(tx *Tx) {
		if got := tx.Block(cube.Pos{15, 0, 8}).(migrationTestBlock); !got.east {
			t.Fatal("generated derived state was not reconstructed after its east chunk became ready")
		}
	})
}

type migrationDependentBlock struct{ migrationTestBlock }

func (b migrationDependentBlock) EncodeBlock() (string, map[string]any) {
	return "test:state_migration_dependency", map[string]any{"minecraft:connection_east": b.east}
}
func (b migrationDependentBlock) Hash() (uint64, uint64) {
	_, state := b.migrationTestBlock.Hash()
	return 1<<60 + 1, state
}
func (migrationDependentBlock) ReconstructionOrder() int { return 1 }
func (b migrationDependentBlock) ReconstructMissingState(pos cube.Pos, missing []string, blocks BlockSource) Block {
	for _, property := range missing {
		if property == "minecraft:connection_east" {
			neighbour, ok := blocks.Block(pos.Side(cube.FaceEast)).(migrationTestBlock)
			b.east = ok && neighbour.east
		}
	}
	return b
}

type migrationDependencyGenerator struct {
	migrationTestGenerator
	dependentRID uint32
}

func (g migrationDependencyGenerator) GenerateChunk(pos ChunkPos, c *chunk.Chunk) {
	g.migrationTestGenerator.GenerateChunk(pos, c)
	if pos == (ChunkPos{0, 0}) {
		c.SetBlock(14, 0, 8, 0, g.dependentRID)
	}
}

func TestMissingStateDependencyWaitsForBoundaryReconstruction(t *testing.T) {
	registry := NewBlockRegistry()
	for _, east := range []bool{false, true} {
		for _, b := range []Block{migrationTestBlock{east: east}, migrationDependentBlock{migrationTestBlock{east: east}}} {
			name, properties := b.EncodeBlock()
			registry.RegisterBlockState(BlockState{Name: name, Properties: properties})
			registry.RegisterBlock(b)
		}
	}
	registry.Finalize()
	blockRID, _ := registry.StateToRuntimeID(migrationTestBlockName, nil)
	dependentRID, _ := registry.StateToRuntimeID("test:state_migration_dependency", nil)
	stoneRID, _ := registry.StateToRuntimeID("minecraft:stone", nil)
	provider := &migrationTestProvider{registry: registry, columns: make(map[ChunkPos]chunk.SerialisedData)}
	w := Config{
		Blocks: registry, Provider: provider, Synchronous: true,
		Generator: migrationDependencyGenerator{migrationTestGenerator{blockRID: blockRID, stoneRID: stoneRID}, dependentRID},
	}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *Tx) {
		pos := ChunkPos{0, 0}
		tx.chunk(pos)
		col := w.chunks[pos]
		if len(col.missingBlockStates) != 2 {
			t.Errorf("dependent state was committed before boundary geometry: %#v", col.missingBlockStates)
		}
		w.closeChunk(tx, pos, col)
		tx.chunk(pos)
		if len(w.chunks[pos].missingBlockStates) != 2 {
			t.Errorf("unresolved dependency was lost across save/reload: %#v", w.chunks[pos].missingBlockStates)
		}
		tx.chunk(ChunkPos{1, 0})
		if !tx.Block(cube.Pos{15, 0, 8}).(migrationTestBlock).east {
			t.Error("boundary geometry was not reconstructed")
		}
		if !tx.Block(cube.Pos{14, 0, 8}).(migrationDependentBlock).east {
			t.Error("dependent geometry used the boundary block's unreconstructed state")
		}
		if len(w.chunks[pos].missingBlockStates) != 0 {
			t.Errorf("resolved dependency remains pending: %#v", w.chunks[pos].missingBlockStates)
		}
	})
}

type migrationTestProvider struct {
	NopProvider
	registry BlockRegistry
	columns  map[ChunkPos]chunk.SerialisedData
}

func (p *migrationTestProvider) LoadColumn(pos ChunkPos, dim Dimension) (*chunk.Column, error) {
	data, ok := p.columns[pos]
	if !ok {
		return nil, leveldb.ErrNotFound
	}
	decoded, err := chunk.DiskDecode(p.registry, data, dim.Range())
	if err != nil {
		return nil, err
	}
	return &chunk.Column{Chunk: decoded}, nil
}

func (p *migrationTestProvider) StoreColumn(pos ChunkPos, _ Dimension, col *chunk.Column) error {
	p.columns[pos] = chunk.Encode(col.Chunk, chunk.DiskEncoding)
	return nil
}

func TestDiskMissingStateMigrationTracksPaletteEntriesAndChunkBoundaries(t *testing.T) {
	for _, markerFirst := range []bool{true, false} {
		name := "neighbour loads after marker"
		if !markerFirst {
			name = "marker loads after neighbour"
		}
		t.Run(name, func(t *testing.T) {
			registry := NewBlockRegistry()
			registry.RegisterBlockState(BlockState{Name: migrationTestBlockName, Properties: map[string]any{"minecraft:connection_east": false}})
			registry.RegisterBlockState(BlockState{Name: migrationTestBlockName, Properties: map[string]any{"minecraft:connection_east": true}})
			registry.RegisterBlock(migrationTestBlock{})
			registry.RegisterBlock(migrationTestBlock{east: true})
			registry.Finalize()

			primary, err := chunk.DiskDecode(registry, migrationTestDiskData(t), Overworld.Range())
			if err != nil {
				t.Fatal(err)
			}
			if explicit, partial := primary.Block(14, 0, 8, 0), primary.Block(15, 0, 8, 0); explicit != partial {
				t.Fatalf("complete and partial palette entries did not resolve to the same original state: %d != %d", explicit, partial)
			}
			missing := primary.MissingBlockStates()
			if len(missing) != 1 || missing[0].Position != (cube.Pos{15, 0, 8}) || len(missing[0].Properties) != 1 || missing[0].Properties[0] != "minecraft:connection_east" {
				t.Fatalf("missing-state markers = %#v, want only the partial palette position and omitted east property", missing)
			}

			stoneRID, ok := registry.StateToRuntimeID("minecraft:stone", nil)
			if !ok {
				t.Fatal("stone runtime ID not found")
			}
			neighbour := chunk.New(registry, Overworld.Range())
			neighbour.SetBlock(0, 0, 8, 0, stoneRID)
			provider := &migrationTestProvider{registry: registry, columns: map[ChunkPos]chunk.SerialisedData{
				{0, 0}: chunk.Encode(primary, chunk.DiskEncoding),
				{1, 0}: chunk.Encode(neighbour, chunk.DiskEncoding),
			}}
			w := Config{Blocks: registry, Provider: provider, Synchronous: true}.New()
			t.Cleanup(func() { _ = w.Close() })

			markerPos, neighbourPos := ChunkPos{0, 0}, ChunkPos{1, 0}
			if !markerFirst {
				markerPos, neighbourPos = neighbourPos, markerPos
			}
			<-w.Exec(func(tx *Tx) { tx.chunk(markerPos) })
			if markerFirst {
				<-w.Exec(func(tx *Tx) {
					if got := tx.Block(cube.Pos{15, 0, 8}).(migrationTestBlock); got.east {
						t.Fatal("missing boundary state was reconstructed before the east chunk was ready")
					}
					col := w.chunks[markerPos]
					tx.SetBlock(cube.Pos{3, 0, 3}, registry.Air(), &SetOpts{
						DisableBlockUpdates: true, DisableRedstoneUpdates: true, DisableLiquidDisplacement: true,
					})
					w.closeChunk(tx, markerPos, col)
					tx.chunk(markerPos)
				})
				reloaded, err := chunk.DiskDecode(registry, provider.columns[markerPos], Overworld.Range())
				if err != nil {
					t.Fatal(err)
				}
				if missing := reloaded.MissingBlockStates(); len(missing) != 1 || missing[0].Position != (cube.Pos{15, 0, 8}) {
					t.Fatalf("saved unresolved state markers = %#v, want the same boundary cell after reload", missing)
				}
				<-w.Exec(func(tx *Tx) {
					tx.SetBlock(cube.Pos{15, 0, 8}, migrationTestBlock{}, &SetOpts{
						DisableBlockUpdates: true, DisableRedstoneUpdates: true, DisableLiquidDisplacement: true,
					})
					if got := tx.Block(cube.Pos{15, 0, 8}).(migrationTestBlock); got.east {
						t.Fatal("same-runtime-ID user write did not retain the explicit false state")
					}
				})
			}
			<-w.Exec(func(tx *Tx) { tx.chunk(neighbourPos) })
			<-w.Exec(func(tx *Tx) {
				gotEast := tx.Block(cube.Pos{15, 0, 8}).(migrationTestBlock).east
				if gotEast != !markerFirst {
					t.Fatalf("boundary east state = %v, want %v after neighbour load", gotEast, !markerFirst)
				}
				if got := tx.Block(cube.Pos{14, 0, 8}).(migrationTestBlock); got.east {
					t.Fatal("explicitly encoded false property was overwritten during migration")
				}
			})
		})
	}
}

func migrationTestDiskData(t *testing.T) chunk.SerialisedData {
	t.Helper()
	entries := []map[string]any{
		{"name": "minecraft:air", "states": map[string]any{}, "version": chunk.CurrentBlockVersion},
		{"name": migrationTestBlockName, "states": map[string]any{}, "version": chunk.CurrentBlockVersion},
		{"name": migrationTestBlockName, "states": map[string]any{"minecraft:connection_east": false}, "version": chunk.CurrentBlockVersion},
	}
	indices := make([]uint32, 256)
	setPaletteIndex := func(x, y, z byte, index uint32) {
		blockIndex := (uint16(x) << 8) | (uint16(z) << 4) | uint16(y)
		word, shift := blockIndex/16, (blockIndex%16)*2
		indices[word] |= index << shift
	}
	setPaletteIndex(15, 0, 8, 1)
	setPaletteIndex(14, 0, 8, 2)

	var data bytes.Buffer
	data.Write([]byte{chunk.SubChunkVersion, 1, 0, 4})
	for _, word := range indices {
		if err := binary.Write(&data, binary.LittleEndian, word); err != nil {
			t.Fatal(err)
		}
	}
	if err := binary.Write(&data, binary.LittleEndian, uint32(len(entries))); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := nbt.NewEncoderWithEncoding(&data, nbt.LittleEndian).Encode(entry); err != nil {
			t.Fatal(err)
		}
	}
	return chunk.SerialisedData{SubChunks: [][]byte{data.Bytes()}}
}
