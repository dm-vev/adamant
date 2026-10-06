package main

import (
	"fmt"
	"slices"
	"sort"
)

type biome struct {
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

type biomeDifference struct {
	Identifier string   `json:"identifier"`
	Reference  *biome   `json:"reference,omitempty"`
	Candidate  *biome   `json:"candidate,omitempty"`
	Changed    []string `json:"changed,omitempty"`
}

func validateBiomes(biomes []biome) error {
	if biomes == nil {
		return fmt.Errorf("invalid snapshot: biomes is required")
	}
	identifiers := make(map[string]struct{}, len(biomes))
	indices := make([]bool, len(biomes))
	for i := range biomes {
		b := &biomes[i]
		if b.Identifier == "" {
			return fmt.Errorf("invalid snapshot: empty biome identifier")
		}
		if _, duplicate := identifiers[b.Identifier]; duplicate {
			return fmt.Errorf("invalid snapshot: duplicate biome identifier %q", b.Identifier)
		}
		if b.Index < 0 || b.Index >= len(biomes) || indices[b.Index] {
			return fmt.Errorf("invalid snapshot: invalid or duplicate biome index %d for %q", b.Index, b.Identifier)
		}
		identifiers[b.Identifier] = struct{}{}
		indices[b.Index] = true
		slices.Sort(b.Tags)
	}
	return nil
}

func compareBiomes(a, b []biome) []biomeDifference {
	am, bm := make(map[string]*biome, len(a)), make(map[string]*biome, len(b))
	for i := range a {
		am[a[i].Identifier] = &a[i]
	}
	for i := range b {
		bm[b[i].Identifier] = &b[i]
	}
	keys := make([]string, 0, len(am)+len(bm))
	for name := range am {
		keys = append(keys, name)
	}
	for name := range bm {
		if _, exists := am[name]; !exists {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	var differences []biomeDifference
	for _, name := range keys {
		x, y := am[name], bm[name]
		if x == nil || y == nil {
			differences = append(differences, biomeDifference{Identifier: name, Reference: x, Candidate: y})
			continue
		}
		var changed []string
		if x.BiomeID != y.BiomeID {
			changed = append(changed, "biome_id")
		}
		if x.Temperature != y.Temperature {
			changed = append(changed, "temperature")
		}
		if x.Downfall != y.Downfall {
			changed = append(changed, "downfall")
		}
		if x.FoliageSnow != y.FoliageSnow {
			changed = append(changed, "foliage_snow")
		}
		if x.Depth != y.Depth {
			changed = append(changed, "depth")
		}
		if x.Scale != y.Scale {
			changed = append(changed, "scale")
		}
		if x.MapWaterColour != y.MapWaterColour {
			changed = append(changed, "map_water_colour")
		}
		if x.Rain != y.Rain {
			changed = append(changed, "rain")
		}
		if (x.Tags == nil) != (y.Tags == nil) || !slices.Equal(x.Tags, y.Tags) {
			changed = append(changed, "tags")
		}
		if len(changed) != 0 {
			differences = append(differences, biomeDifference{Identifier: name, Reference: x, Candidate: y, Changed: changed})
		}
	}
	return differences
}
