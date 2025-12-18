package world

import (
	"encoding/binary"
	"math"
	"sort"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

var (
	// maxVanillaBiomeID is the highest ID used by vanilla biomes.
	maxVanillaBiomeID int
)

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
	// Clients use biome IDs in chunk data for things like grass/foliage colouring. If biome definitions are
	// generated in a random order (as iterating maps does), clients may interpret biome IDs incorrectly.
	ids := make([]int, 0, len(biomes))
	for id := range biomes {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	encodedBiomes := make([]protocol.BiomeDefinition, 0, len(ids))
	for _, id := range ids {
		b := biomes[id]
		nameIndex := intern(b.String())

		tags := b.Tags()
		tagIndices := make([]uint16, len(tags))
		for i, tag := range tags {
			tagIndices[i] = uint16(intern(tag))
		}

		// Always set BiomeID explicitly. The ID is what is referenced in chunk biome data and must match what
		// the client expects, otherwise biome-dependent colouring won't apply correctly.
		biomeID := int16(-1)
		if id >= 0 && id <= math.MaxInt16 {
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
