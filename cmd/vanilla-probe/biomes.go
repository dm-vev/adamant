package main

import (
	"fmt"
	"slices"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

type biomeIdentity struct {
	Identifier     string   `json:"identifier"`
	Index          int      `json:"index"`
	BiomeID        int16    `json:"biome_id"`
	Temperature    float32  `json:"temperature"`
	Downfall       float32  `json:"downfall"`
	FoliageSnow    float32  `json:"foliage_snow"`
	Depth          float32  `json:"depth"`
	Scale          float32  `json:"scale"`
	MapWaterColour uint32   `json:"map_water_colour"`
	Rain           bool     `json:"rain"`
	Tags           []string `json:"tags"`
}

func biomeIdentities(pk *v2193.BiomeDefinitionListPacket) ([]biomeIdentity, error) {
	biomes := make([]biomeIdentity, len(pk.BiomeDefinitions))
	for i := range pk.BiomeDefinitions {
		definition := &pk.BiomeDefinitions[i]
		if definition.NameIndex < 0 || int(definition.NameIndex) >= len(pk.StringList) {
			return nil, fmt.Errorf("biome %d: name index %d outside string list", i, definition.NameIndex)
		}
		var tags []string
		if indices, present := definition.Tags.Value(); present {
			tags = make([]string, len(indices))
			for j, index := range indices {
				if int(index) >= len(pk.StringList) {
					return nil, fmt.Errorf("biome %d: tag index %d outside string list", i, index)
				}
				tags[j] = pk.StringList[index]
			}
			slices.Sort(tags)
		}
		biomes[i] = biomeIdentity{
			Identifier: pk.StringList[definition.NameIndex], Index: i, BiomeID: definition.BiomeID,
			Temperature: definition.Temperature, Downfall: definition.Downfall, FoliageSnow: definition.FoliageSnow,
			Depth: definition.Depth, Scale: definition.Scale, MapWaterColour: uint32(definition.MapWaterColour),
			Rain: definition.Rain, Tags: tags,
		}
	}
	return biomes, nil
}
