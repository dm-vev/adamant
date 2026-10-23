package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

type modernTemplateEntry struct {
	name      string
	rotations []structure.Rotation
	weight    int32
}

// placeModernTemplateFeature ports TemplateFeature.place, including its two
// sequential random selections before template placement.
func placeModernTemplateFeature(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	if random == nil {
		panic("overworld: template feature has no random source")
	}
	rawTemplates, ok := config["templates"].([]any)
	if !ok {
		panic("overworld: malformed template feature templates")
	}
	entries := make([]modernTemplateEntry, len(rawTemplates))
	var totalWeight int64
	for i, raw := range rawTemplates {
		weighted, ok := raw.(map[string]any)
		if !ok {
			panic("overworld: malformed weighted template entry")
		}
		weight, ok := weighted["weight"].(float64)
		if !ok || weight < 0 || weight > float64(^uint32(0)>>1) || weight != float64(int64(weight)) {
			panic("overworld: malformed template weight")
		}
		data, ok := weighted["data"].(map[string]any)
		if !ok {
			panic("overworld: malformed template entry data")
		}
		name, ok := data["id"].(string)
		if !ok || name == "" {
			panic("overworld: malformed template identifier")
		}
		rotations := []structure.Rotation{structure.None, structure.Clockwise90, structure.Clockwise180, structure.CounterClockwise90}
		if rawRotations, exists := data["rotations"]; exists {
			values, ok := rawRotations.([]any)
			if !ok {
				panic("overworld: malformed template rotations")
			}
			rotations = make([]structure.Rotation, len(values))
			for j, value := range values {
				rotation, ok := value.(string)
				if !ok {
					panic("overworld: malformed template rotation")
				}
				switch rotation {
				case "none":
					rotations[j] = structure.None
				case "clockwise_90":
					rotations[j] = structure.Clockwise90
				case "180":
					rotations[j] = structure.Clockwise180
				case "counterclockwise_90":
					rotations[j] = structure.CounterClockwise90
				default:
					panic("overworld: unsupported template rotation " + rotation)
				}
			}
		}
		entries[i] = modernTemplateEntry{name: normalizeResource(name), rotations: rotations, weight: int32(weight)}
		totalWeight += int64(weight)
		if totalWeight > int64(^uint32(0)>>1) {
			panic("overworld: template total weight overflows int")
		}
	}
	if totalWeight == 0 {
		panic("overworld: template weighted list has no positive-weight entries")
	}
	entry := entries[0]
	choice := random.NextInt(int32(totalWeight))
	for _, candidate := range entries {
		if choice < candidate.weight {
			entry = candidate
			break
		}
		choice -= candidate.weight
	}
	if len(entry.rotations) == 0 {
		panic("overworld: template rotation list is empty")
	}
	rotation := entry.rotations[random.NextInt(int32(len(entry.rotations)))]

	template, err := structure.LoadOrCreate(entry.name)
	if err != nil {
		panic(fmt.Errorf("overworld: loading template %q: %w", entry.name, err))
	}
	if len(template.Palettes) == 0 {
		return false
	}
	palette := int(random.NextInt(int32(len(template.Palettes))))

	// Java offsets each negative template axis independently, using integer
	// division before rotation (so odd dimensions bias toward the negative edge).
	offsetX := structure.TransformPosition([3]int32{-template.Size[0] / 2, 0, 0}, structure.NoMirror, rotation)
	offsetZ := structure.TransformPosition([3]int32{0, 0, -template.Size[2] / 2}, structure.NoMirror, rotation)
	placeOrigin := [3]int32{origin.x + offsetX[0] + offsetZ[0], origin.y, origin.z + offsetX[2] + offsetZ[2]}

	var processorNames []string
	var inlineProcessorSpecs []structure.ProcessorSpec
	if rawProcessors, exists := config["processors"]; exists {
		switch value := rawProcessors.(type) {
		case string:
			processorNames = []string{normalizeResource(value)}
		case []any:
			inlineProcessorSpecs = modernTemplateProcessorSpecs(value)
		case map[string]any:
			if id, ok := value["id"].(string); ok {
				processorNames = []string{normalizeResource(id)}
			} else if specs, ok := value["processors"].([]any); ok {
				inlineProcessorSpecs = modernTemplateProcessorSpecs(specs)
			} else {
				panic("overworld: malformed inline template processor-list holder")
			}
		default:
			panic("overworld: malformed template processor-list holder")
		}
	}
	processors, err := structure.LoadProcessors(processorNames, inlineProcessorSpecs, structure.ProcessorEnvironment{
		BaseBlock: func(x, y, z int32) world.Block { return region.Block(featurePos{x, y, z}) },
		BiomeAt:   region.f.source.BiomeAt,
		BaseHeight: func(x, z int32, oceanFloor bool) int32 {
			kind := "WORLD_SURFACE_WG"
			if oceanFloor {
				kind = "OCEAN_FLOOR_WG"
			}
			return region.placementHeight(x, z, kind)
		},
		WorldSeed:      region.f.seed,
		StartPos:       placeOrigin,
		BlockCount:     len(template.Palettes[palette]),
		Random:         random,
		SettingsRandom: random,
		PlaceFeature: func(feature any, pos [3]int32, rng *vanilla.Random) bool {
			return region.PlacePlacedFeature(feature, pos, rng)
		},
	})
	if err != nil {
		panic(fmt.Errorf("overworld: loading template processors: %w", err))
	}
	// Palette selection and processors using StructurePlaceSettings.getRandom
	// share the placement stream. RuleProcessor owns a position-seeded stream.
	// TemplateFeature does not set a bounding box. Process all descriptors and
	// entities; the live region independently rejects out-of-build/window writes.
	clip := structure.Bounds{Min: [3]int32{-1 << 31, -1 << 31, -1 << 31}, Max: [3]int32{1<<31 - 1, 1<<31 - 1, 1<<31 - 1}}
	placement, err := template.Place(palette, placeOrigin, rotation, structure.NoMirror, clip, region.f.seed, processors...)
	if err != nil {
		panic(fmt.Errorf("overworld: placing template %q: %w", entry.name, err))
	}
	placement.TemplateUpdateFlags = 3
	placement.TemplateKnownShape = false
	for i := range placement.Blocks {
		p := placement.Blocks[i].Pos
		if p[1] >= region.MinY() && p[1] <= region.MaxY() && region.withinWriteArea(featurePos{p[0], p[1], p[2]}) {
			structure.SeedRandomizableContainerLoot(&placement.Blocks[i], random)
		}
	}
	region.f.applyStructurePlacement(region, placement)

	// TemplateFeature returns StructureTemplate.placeInWorld's success value:
	// it is based on the chosen palette, entity list, and positive template size,
	// not on whether clipping or processors yielded a write.
	return template.Size[0] >= 1 && template.Size[1] >= 1 && template.Size[2] >= 1 &&
		(len(template.Palettes[palette]) != 0 || len(template.Entities) != 0)
}
func modernTemplateProcessorSpecs(values []any) []structure.ProcessorSpec {
	specs := make([]structure.ProcessorSpec, len(values))
	for i, raw := range values {
		processor, ok := raw.(map[string]any)
		if !ok {
			panic("overworld: malformed inline template processor")
		}
		typeName, ok := processor["processor_type"].(string)
		if !ok || typeName == "" {
			panic("overworld: inline template processor has no processor_type")
		}
		specs[i] = structure.ProcessorSpec{
			Type:   normalizeResource(typeName),
			Config: processor,
		}
	}
	return specs
}
