package session

import (
	"strings"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// biomeDefinitions returns the definitions of the biomes registered, in the
// order of world.BiomeIDs, and the strings their names and tags index.
// A biome that is not one of the game's own carries its ID, a vanilla one
// -1.
func biomeDefinitions() ([]mc.BiomeDefinition, []string) {
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
	ids := world.BiomeIDs()
	encodedBiomes := make([]mc.BiomeDefinition, 0, len(ids))
	for _, id := range ids {
		b, _ := world.BiomeByID(id)
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
		if !world.VanillaBiome(id) {
			biomeID = int16(id)
		}
		water := b.WaterColour()

		def := mc.BiomeDefinition{
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
