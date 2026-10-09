package mcdb

import (
	"errors"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
)

func TestPostProcessingPersistsAcrossProviderRestart(t *testing.T) {
	dim := postProcessingTestDimension{}
	if err := world.RegisterDimension(32701, "post-processing-test", dim); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pos := world.ChunkPos{-3, 8}
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	many := make([]uint16, 65_536)
	for i := range many {
		many[i] = 0x0012
	}
	many[len(many)-1] = 0x0043
	want := [][]uint16{many, {}, {0x0fff}, {0x04ab}}
	col := &chunk.Column{
		Chunk:          chunk.New(world.DefaultBlockRegistry, dim.Range()),
		PostProcessing: want,
	}
	if err := db.StoreColumn(pos, dim, col); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	loaded, err := db.LoadColumn(pos, dim)
	if err != nil {
		t.Fatal(err)
	}
	if !equalPostProcessing(loaded.PostProcessing, want) {
		t.Fatalf("post-processing after restart = %#v, want %#v", loaded.PostProcessing, want)
	}
}

func TestStoreColumnClearsEmptyPostProcessingMetadata(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pos := world.ChunkPos{-3, 8}
	col := &chunk.Column{
		Chunk:          chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()),
		PostProcessing: [][]uint16{{0x123}},
	}
	if err := db.StoreColumn(pos, world.Overworld, col); err != nil {
		t.Fatal(err)
	}
	col.PostProcessing = nil
	if err := db.StoreColumn(pos, world.Overworld, col); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ldb.Get((dbKey{pos: pos, dim: world.Overworld}).Sum([]byte(keyAdamantPostProcessing)...), nil); !errors.Is(err, leveldb.ErrNotFound) {
		t.Fatalf("post-processing metadata lookup error = %v, want not found", err)
	}
	loaded, err := db.LoadColumn(pos, world.Overworld)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.PostProcessing) != 0 {
		t.Fatalf("post-processing after empty save = %#v, want empty", loaded.PostProcessing)
	}
}

func TestLoadColumnRejectsMalformedPostProcessingData(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pos := world.ChunkPos{4, 5}
	if err := db.StoreColumn(pos, world.Overworld, &chunk.Column{Chunk: chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())}); err != nil {
		t.Fatal(err)
	}
	key := (dbKey{pos: pos, dim: world.Overworld}).Sum([]byte(keyAdamantPostProcessing)...)
	cases := []struct {
		name string
		data []byte
	}{
		{name: "truncated header", data: []byte{postProcessingVersion, 1}},
		{name: "truncated section count", data: []byte{postProcessingVersion, 1, 0}},
		{name: "truncated marker", data: []byte{postProcessingVersion, 1, 0, 1, 0, 0, 0, 0x34}},
		{name: "invalid coordinate bits", data: []byte{postProcessingVersion, 1, 0, 1, 0, 0, 0, 0, 0xf0}},
		{name: "too many section rows", data: []byte{postProcessingVersion, 0xff, 0xff}},
		{name: "trailing bytes", data: []byte{postProcessingVersion, 0, 0, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := db.ldb.Put(key, tc.data, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := db.LoadColumn(pos, world.Overworld); err == nil {
				t.Fatal("LoadColumn succeeded for malformed post-processing data")
			}
		})
	}
}

func TestStoreColumnRejectsInvalidPostProcessingCoordinates(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	col := &chunk.Column{
		Chunk:          chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()),
		PostProcessing: [][]uint16{{0xf000}},
	}
	if err := db.StoreColumn(world.ChunkPos{}, world.Overworld, col); err == nil {
		t.Fatal("StoreColumn succeeded for reserved packed coordinate bits")
	}
}

func equalPostProcessing(a, b [][]uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

type postProcessingTestDimension struct{}

func (postProcessingTestDimension) Range() cube.Range                 { return cube.Range{-128, 127} }
func (postProcessingTestDimension) WaterEvaporates() bool             { return false }
func (postProcessingTestDimension) LavaSpreadDuration() time.Duration { return time.Second }
func (postProcessingTestDimension) WeatherCycle() bool                { return true }
func (postProcessingTestDimension) TimeCycle() bool                   { return true }
