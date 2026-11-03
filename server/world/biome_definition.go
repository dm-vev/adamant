package world

import (
	"strings"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// BiomeDefinitions returns the list of biome definitions along with the associated StringList.
func BiomeDefinitions() ([]v2193.BiomeDefinition, []string) {
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
	ids := BiomeIDs()
	encodedBiomes := make([]v2193.BiomeDefinition, 0, len(ids))
	for _, id := range ids {
		b, _ := BiomeByID(id)
		name := b.String()
		if !strings.Contains(name, ":") {
			name = "minecraft:" + name
		}
		nameIndex := intern(name)

		tags := b.Tags()
		tagIndices := make([]uint16, len(tags))
		for i, tag := range tags {
			tagIndices[i] = uint16(intern(tag))
		}

		biomeID := int16(-1)
		if !VanillaBiome(id) {
			biomeID = int16(id)
		}
		water := b.WaterColour()

		def := v2193.BiomeDefinition{
			NameIndex:      int16(nameIndex),
			BiomeID:        biomeID,
			Temperature:    float32(b.Temperature()),
			Downfall:       float32(b.Rainfall()),
			Depth:          float32(b.Depth()),
			Scale:          float32(b.Scale()),
			MapWaterColour: int32(uint32(water.A)<<24 | uint32(water.R)<<16 | uint32(water.G)<<8 | uint32(water.B)),
			Rain:           b.HasPrecipitation(),
			Tags:           proto.Some(tagIndices),
		}

		encodedBiomes = append(encodedBiomes, def)
	}

	return encodedBiomes, internedStrings
}
