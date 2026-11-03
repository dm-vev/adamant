package world

import (
	"slices"
	"sort"
)

var (
	// maxVanillaBiomeID is the highest ID used by vanilla biomes.
	maxVanillaBiomeID int

	// biomeIndexReady reports whether the biome index reflects the current
	// biome registry. RegisterBiome clears it.
	biomeIndexReady bool
	// biomeIDsSorted holds the IDs of the registered biomes in ascending
	// order, and biomeIDToIndex the position of each in it.
	biomeIDsSorted []int
	biomeIDToIndex map[uint32]uint32
)

// BiomeIDs returns the IDs of the biomes registered, in ascending order.
func BiomeIDs() []int {
	ensureBiomeIndex()
	biomeMu.RLock()
	defer biomeMu.RUnlock()
	return slices.Clone(biomeIDsSorted)
}

// BiomeIndex returns the position of the biome ID passed in BiomeIDs, and
// false if no biome of that ID is registered.
func BiomeIndex(id uint32) (index uint32, ok bool) {
	ensureBiomeIndex()
	biomeMu.RLock()
	defer biomeMu.RUnlock()
	index, ok = biomeIDToIndex[id]
	return index, ok
}

// VanillaBiome reports whether the biome of the ID passed is one of the
// game's own: its ID is at most the highest ID of the biomes the game
// registers before any other.
func VanillaBiome(id int) bool {
	biomeMu.RLock()
	defer biomeMu.RUnlock()
	return id <= maxVanillaBiomeID
}

// ensureBiomeIndex builds the biome index from the biome registry if a biome
// was registered since it was last built.
func ensureBiomeIndex() {
	biomeMu.RLock()
	if biomeIndexReady {
		biomeMu.RUnlock()
		return
	}
	biomeMu.RUnlock()

	biomeMu.Lock()
	defer biomeMu.Unlock()
	if biomeIndexReady {
		return
	}
	ids := make([]int, 0, len(biomes))
	for id := range biomes {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	biomeIDsSorted = ids

	biomeIDToIndex = make(map[uint32]uint32, len(ids))
	for i, id := range ids {
		biomeIDToIndex[uint32(id)] = uint32(i)
	}
	biomeIndexReady = true
}

// finaliseBiomeRegistry is called after all vanilla biomes have been registered.
// It sets maxVanillaBiomeID to the highest ID found among them.
// noinspection GoUnusedFunction
//
//lint:ignore U1000 Function is used through compiler directives.
func finaliseBiomeRegistry() {
	biomeMu.Lock()
	defer biomeMu.Unlock()

	for _, b := range biomes {
		id := b.EncodeBiome()
		if id > maxVanillaBiomeID {
			maxVanillaBiomeID = id
		}
	}
}
