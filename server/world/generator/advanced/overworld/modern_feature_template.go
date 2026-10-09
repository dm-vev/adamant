package overworld

import (
	"fmt"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type modernTemplateEntry struct {
	name      string
	rotations []javastructure.Rotation
	weight    int32
}

// placeModernTemplateFeature ports TemplateFeature.place, including its two
// sequential random selections before template placement.
func placeModernTemplateFeature(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	if random == nil {
		panic("overworld: Java template feature has no random source")
	}
	rawTemplates, ok := config["templates"].([]any)
	if !ok {
		panic("overworld: malformed Java template feature templates")
	}
	entries := make([]modernTemplateEntry, len(rawTemplates))
	var totalWeight int64
	for i, raw := range rawTemplates {
		weighted, ok := raw.(map[string]any)
		if !ok {
			panic("overworld: malformed Java weighted template entry")
		}
		weight, ok := weighted["weight"].(float64)
		if !ok || weight < 0 || weight > float64(^uint32(0)>>1) || weight != float64(int64(weight)) {
			panic("overworld: malformed Java template weight")
		}
		data, ok := weighted["data"].(map[string]any)
		if !ok {
			panic("overworld: malformed Java template entry data")
		}
		name, ok := data["id"].(string)
		if !ok || name == "" {
			panic("overworld: malformed Java template identifier")
		}
		rotations := []javastructure.Rotation{javastructure.None, javastructure.Clockwise90, javastructure.Clockwise180, javastructure.CounterClockwise90}
		if rawRotations, exists := data["rotations"]; exists {
			values, ok := rawRotations.([]any)
			if !ok {
				panic("overworld: malformed Java template rotations")
			}
			rotations = make([]javastructure.Rotation, len(values))
			for j, value := range values {
				rotation, ok := value.(string)
				if !ok {
					panic("overworld: malformed Java template rotation")
				}
				switch rotation {
				case "none":
					rotations[j] = javastructure.None
				case "clockwise_90":
					rotations[j] = javastructure.Clockwise90
				case "180":
					rotations[j] = javastructure.Clockwise180
				case "counterclockwise_90":
					rotations[j] = javastructure.CounterClockwise90
				default:
					panic("overworld: unsupported Java template rotation " + rotation)
				}
			}
		}
		entries[i] = modernTemplateEntry{name: normalizeJavaResource(name), rotations: rotations, weight: int32(weight)}
		totalWeight += int64(weight)
		if totalWeight > int64(^uint32(0)>>1) {
			panic("overworld: Java template total weight overflows int")
		}
	}
	if totalWeight == 0 {
		panic("overworld: Java template weighted list has no positive-weight entries")
	}
	choice := random.NextInt(int32(totalWeight))
	var entry modernTemplateEntry
	for _, candidate := range entries {
		if choice < candidate.weight {
			entry = candidate
			break
		}
		choice -= candidate.weight
	}
	if len(entry.rotations) == 0 {
		panic("overworld: Java template rotation list is empty")
	}
	rotation := entry.rotations[random.NextInt(int32(len(entry.rotations)))]

	template, err := javastructure.LoadOrCreate(entry.name)
	if err != nil {
		panic(fmt.Errorf("overworld: loading Java template %q: %w", entry.name, err))
	}
	if len(template.Palettes) == 0 {
		return false
	}
	palette := int(random.NextInt(int32(len(template.Palettes))))

	// Java offsets each negative template axis independently, using integer
	// division before rotation (so odd dimensions bias toward the negative edge).
	offsetX := javastructure.TransformPosition([3]int32{-template.Size[0] / 2, 0, 0}, javastructure.NoMirror, rotation)
	offsetZ := javastructure.TransformPosition([3]int32{0, 0, -template.Size[2] / 2}, javastructure.NoMirror, rotation)
	placeOrigin := [3]int32{origin.x + offsetX[0] + offsetZ[0], origin.y, origin.z + offsetX[2] + offsetZ[2]}

	var processorNames []string
	var inlineProcessorSpecs []javastructure.ProcessorSpec
	if rawProcessors, exists := config["processors"]; exists {
		switch value := rawProcessors.(type) {
		case string:
			processorNames = []string{normalizeJavaResource(value)}
		case []any:
			inlineProcessorSpecs = modernTemplateProcessorSpecs(value)
		case map[string]any:
			if id, ok := value["id"].(string); ok {
				processorNames = []string{normalizeJavaResource(id)}
			} else if specs, ok := value["processors"].([]any); ok {
				inlineProcessorSpecs = modernTemplateProcessorSpecs(specs)
			} else {
				panic("overworld: malformed inline Java template processor-list holder")
			}
		default:
			panic("overworld: malformed Java template processor-list holder")
		}
	}
	processors, err := javastructure.LoadProcessors(processorNames, inlineProcessorSpecs, javastructure.ProcessorEnvironment{
		BaseBlock:   func(x, y, z int32) world.Block { return region.Block(featurePos{x, y, z}) },
		JavaBiomeAt: region.f.source.JavaBiomeAt,
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
		PlaceFeature: func(feature any, pos [3]int32, rng *java26.Random) bool {
			return region.PlacePlacedFeature(feature, pos, rng)
		},
	})
	if err != nil {
		panic(fmt.Errorf("overworld: loading Java template processors: %w", err))
	}
	// Palette selection and processors using StructurePlaceSettings.getRandom
	// share the placement stream. RuleProcessor owns a position-seeded stream.
	chunkX, chunkZ := region.writeOriginX, region.writeOriginZ
	clip := javastructure.Bounds{
		Min: [3]int32{(chunkX - 1) * 16, region.MinY(), (chunkZ - 1) * 16},
		Max: [3]int32{(chunkX+2)*16 - 1, region.MaxY(), (chunkZ+2)*16 - 1},
	}
	placement, err := template.Place(palette, placeOrigin, rotation, javastructure.NoMirror, clip, region.f.seed, processors...)
	if err != nil {
		panic(fmt.Errorf("overworld: placing Java template %q: %w", entry.name, err))
	}
	placement.TemplateUpdateFlags = 3
	placement.TemplateKnownShape = false
	for i := range placement.Blocks {
		javastructure.SeedRandomizableContainerLoot(&placement.Blocks[i], random)
	}
	region.f.applyStructurePlacement(region, placement)

	// TemplateFeature returns StructureTemplate.placeInWorld's success value:
	// it is based on the chosen palette, entity list, and positive template size,
	// not on whether clipping or processors yielded a write.
	return template.Size[0] >= 1 && template.Size[1] >= 1 && template.Size[2] >= 1 &&
		(len(template.Palettes[palette]) != 0 || len(template.Entities) != 0)
}
func modernTemplateProcessorSpecs(values []any) []javastructure.ProcessorSpec {
	specs := make([]javastructure.ProcessorSpec, len(values))
	for i, raw := range values {
		processor, ok := raw.(map[string]any)
		if !ok {
			panic("overworld: malformed inline Java template processor")
		}
		typeName, ok := processor["processor_type"].(string)
		if !ok || typeName == "" {
			panic("overworld: inline Java template processor has no processor_type")
		}
		specs[i] = javastructure.ProcessorSpec{
			Type:   normalizeJavaResource(typeName),
			Config: processor,
		}
	}
	return specs
}
