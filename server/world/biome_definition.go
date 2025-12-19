package world

import (
	"encoding/binary"
	"sort"
	"sync"

	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

var (
	// maxVanillaBiomeID is the highest ID used by vanilla biomes.
	maxVanillaBiomeID int

	biomeRuntimeOnce   sync.Once
	biomeIDsSorted     []int
	biomeIDToRuntimeID map[uint32]uint32
	biomeRuntimeToID   []uint32
)

func init() {
	chunk.BiomeIDToRuntimeID = func(id uint32) (uint32, bool) {
		ensureBiomeRuntimeData()
		rid, ok := biomeIDToRuntimeID[id]
		return rid, ok
	}
	chunk.BiomeRuntimeIDToID = func(runtimeID uint32) (uint32, bool) {
		ensureBiomeRuntimeData()
		if runtimeID >= uint32(len(biomeRuntimeToID)) {
			return 0, false
		}
		return biomeRuntimeToID[runtimeID], true
	}
}

func ensureBiomeRuntimeData() {
	biomeRuntimeOnce.Do(func() {
		ids := make([]int, 0, len(biomes))
		for id := range biomes {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		biomeIDsSorted = ids

		biomeIDToRuntimeID = make(map[uint32]uint32, len(ids))
		biomeRuntimeToID = make([]uint32, len(ids))
		for i, id := range ids {
			runtimeID := uint32(i)
			biomeIDToRuntimeID[uint32(id)] = runtimeID
			biomeRuntimeToID[runtimeID] = uint32(id)
		}
	})
}

// finaliseBiomeRegistry is called after all vanilla biomes have been registered.
// It sets maxVanillaBiomeID to the highest ID found among them.
// noinspection GoUnusedFunction
//
//lint:ignore U1000 Function is used through compiler directives.
func finaliseBiomeRegistry() {
	for _, b := range biomes {
		id := b.EncodeBiome()
		if id > maxVanillaBiomeID {
			maxVanillaBiomeID = id
		}
	}
}

// BiomeDefinitions returns the list of biome definitions along with the associated StringList.
func BiomeDefinitions() ([]protocol.BiomeDefinition, []string) {
	ensureBiomeRuntimeData()
	var (
		internedStrings     []string
		internedStringIndex = make(map[string]int)
	)

	intern := func(s string) int {
		if index, exists := internedStringIndex[s]; exists {
			return index
		}
		index := len(internedStrings)
		internedStrings = append(internedStrings, s)
		internedStringIndex[s] = index
		return index
	}

	// The order of biomes in this packet must be deterministic across server runs.
	encodedBiomes := make([]protocol.BiomeDefinition, 0, len(biomeIDsSorted))
	for _, id := range biomeIDsSorted {
		b := biomes[id]
		nameIndex := intern(b.String())

		tags := b.Tags()
		tagIndices := make([]uint16, len(tags))
		for i, tag := range tags {
			tagIndices[i] = uint16(intern(tag))
		}

		biomeID := int16(-1)
		if id > maxVanillaBiomeID {
			biomeID = int16(id)
		}

		def := protocol.BiomeDefinition{
			NameIndex:   int16(nameIndex),
			BiomeID:     biomeID,
			Temperature: float32(b.Temperature()),
			Downfall:    float32(b.Rainfall()),
			Depth:       float32(b.Depth()),
			Scale:       float32(b.Scale()),
			MapWaterColour: int32(binary.BigEndian.Uint32([]byte{
				b.WaterColour().A,
				b.WaterColour().R,
				b.WaterColour().G,
				b.WaterColour().B,
			})),
			Rain: b.Rainfall() > 0,
			Tags: protocol.Option[[]uint16](tagIndices),
		}

		encodedBiomes = append(encodedBiomes, def)
	}

	return encodedBiomes, internedStrings
}
