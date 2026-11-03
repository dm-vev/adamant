package world

import (
	"slices"
	"testing"
)

// indexTestBiome is a biome registered by the biome index tests only.
type indexTestBiome struct {
	unknownBiome
	name string
}

func (b indexTestBiome) String() string { return b.name }

// registerIndexTestBiome registers a biome of the ID passed for the rest of
// the test, and removes it from the registry after.
func registerIndexTestBiome(t *testing.T, id int, name string) {
	t.Helper()
	RegisterBiome(indexTestBiome{unknownBiome: unknownBiome{id: id}, name: name})
	t.Cleanup(func() {
		biomeMu.Lock()
		defer biomeMu.Unlock()
		delete(biomes, id)
		delete(biomeByName, name)
		biomeIndexReady = false
	})
}

// TestBiomeIndexFollowsRegistration checks the index orders the biomes by
// their IDs and is built again when a biome is registered after it was
// read.
func TestBiomeIndexFollowsRegistration(t *testing.T) {
	before := BiomeIDs()
	if !slices.IsSorted(before) {
		t.Fatalf("biome IDs %v are not in ascending order", before)
	}
	const low, high = 47000, 47002
	registerIndexTestBiome(t, high, "index_test_high")
	registerIndexTestBiome(t, low, "index_test_low")

	ids := BiomeIDs()
	if len(ids) != len(before)+2 || !slices.IsSorted(ids) {
		t.Fatalf("after registering two biomes, biome IDs are %v (had %d)", ids, len(before))
	}
	for i, id := range ids {
		index, ok := BiomeIndex(uint32(id))
		if !ok || index != uint32(i) {
			t.Fatalf("BiomeIndex(%d) = %d, %v; want %d, true", id, index, ok, i)
		}
		if back, ok := biomeIDAt(uint32(i)); !ok || back != uint32(id) {
			t.Fatalf("biomeIDAt(%d) = %d, %v; want %d, true", i, back, ok, id)
		}
	}
	if index, _ := BiomeIndex(low); index+1 != uint32(len(ids)-1) {
		t.Fatalf("biome %d is at %d, %d at %d", low, index, high, len(ids)-1)
	}
	if _, ok := BiomeIndex(47001); ok {
		t.Fatal("an unregistered biome ID has an index")
	}
	if _, ok := biomeIDAt(uint32(len(ids))); ok {
		t.Fatal("a position past the last biome has an ID")
	}
	ids[0] = -1
	if BiomeIDs()[0] == -1 {
		t.Fatal("BiomeIDs returned the index itself")
	}
}

// TestVanillaBiome checks the biomes past the highest vanilla ID are not the
// game's own.
func TestVanillaBiome(t *testing.T) {
	if !VanillaBiome(0) || !VanillaBiome(maxVanillaBiomeID) {
		t.Fatalf("biomes 0 and %d are not vanilla", maxVanillaBiomeID)
	}
	if VanillaBiome(maxVanillaBiomeID + 1) {
		t.Fatalf("biome %d past the highest vanilla ID is vanilla", maxVanillaBiomeID+1)
	}
}
