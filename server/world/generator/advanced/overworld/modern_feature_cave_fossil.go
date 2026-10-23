package overworld

import (
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func placeModernFossil(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	fossilNames, ok := config["fossil_structures"].([]any)
	if !ok || len(fossilNames) == 0 {
		panic("overworld: malformed fossil structures")
	}
	overlayNames, ok := config["overlay_structures"].([]any)
	if !ok || len(overlayNames) != len(fossilNames) {
		panic("overworld: malformed fossil overlay structures")
	}
	rotation := structure.Rotation(random.NextInt(4))
	index := int(random.NextInt(int32(len(fossilNames))))
	fossilName, ok := fossilNames[index].(string)
	if !ok {
		panic("overworld: malformed fossil structure name")
	}
	overlayName, ok := overlayNames[index].(string)
	if !ok {
		panic("overworld: malformed fossil overlay name")
	}
	fossil, err := structure.Load(normalizeResource(fossilName))
	if err != nil {
		panic(err)
	}
	overlay, err := structure.Load(normalizeResource(overlayName))
	if err != nil {
		panic(err)
	}
	sizeX, sizeZ := fossil.Size[0], fossil.Size[2]
	if rotation == structure.Clockwise90 || rotation == structure.CounterClockwise90 {
		sizeX, sizeZ = sizeZ, sizeX
	}
	lowX, lowZ := origin.x-sizeX/2, origin.z-sizeZ/2
	lowest := origin.y
	for x := int32(0); x < sizeX; x++ {
		for z := int32(0); z < sizeZ; z++ {
			h := region.Height(lowX+x, lowZ+z, true)
			if h < lowest {
				lowest = h
			}
		}
	}
	targetY := lowest - 15 - random.NextInt(10)
	if min := region.MinY() + 10; targetY < min {
		targetY = min
	}
	sizeX0, sizeZ0 := fossil.Size[0]-1, fossil.Size[2]-1
	baseX, baseZ := lowX, lowZ
	switch rotation {
	case structure.Clockwise90:
		baseX += sizeZ0
	case structure.Clockwise180:
		baseX += sizeX0
		baseZ += sizeZ0
	case structure.CounterClockwise90:
		baseZ += sizeX0
	}
	templateOrigin := [3]int32{baseX, targetY, baseZ}
	maxEmpty := featureNumber(config["max_empty_corners_allowed"])
	rotX, rotZ := fossil.Size[0], fossil.Size[2]
	if rotation == structure.Clockwise90 || rotation == structure.CounterClockwise90 {
		rotX, rotZ = rotZ, rotX
	}
	// The zero-position transform compensates the rotation, leaving the
	// bounding-box minimum at the centred footprint for mirror/pivot NONE/ZERO.
	minBound := [3]int32{lowX, targetY, lowZ}
	maxBound := [3]int32{minBound[0] + rotX - 1, minBound[1] + fossil.Size[1] - 1, minBound[2] + rotZ - 1}
	empty := 0
	for _, x := range []int32{minBound[0], maxBound[0]} {
		for _, y := range []int32{minBound[1], maxBound[1]} {
			for _, z := range []int32{minBound[2], maxBound[2]} {
				name, _ := region.State(featurePos{x, y, z})
				if featureRegionAir(region, featurePos{x, y, z}) || name == "minecraft:water" || name == "minecraft:lava" {
					empty++
				}
			}
		}
	}
	if empty > maxEmpty {
		return false
	}
	chunkX, chunkZ := origin.x>>4, origin.z>>4
	clip := structure.Bounds{Min: [3]int32{chunkX*16 - 16, region.MinY(), chunkZ*16 - 16}, Max: [3]int32{chunkX*16 + 31, region.MaxY(), chunkZ*16 + 31}}
	place := func(template *structure.Template, resource any) {
		if len(template.Palettes) == 0 {
			return
		}
		paletteIndex := int(random.NextInt(int32(len(template.Palettes))))
		name := normalizeResource(resource.(string))
		processors, err := structure.LoadProcessors([]string{name}, nil, structure.ProcessorEnvironment{
			BaseBlock: func(x, y, z int32) world.Block { return region.Block(featurePos{x, y, z}) },
			BiomeAt:   region.f.source.BiomeAt,
			BaseHeight: func(x, z int32, oceanFloor bool) int32 {
				kind := "WORLD_SURFACE_WG"
				if oceanFloor {
					kind = "OCEAN_FLOOR_WG"
				}
				return region.placementHeight(x, z, kind)
			},
			WorldSeed: region.f.seed, StartPos: templateOrigin, BlockCount: len(template.Palettes[paletteIndex]), Random: random, SkipEntities: true,
			SettingsRandom: random,
		})
		if err != nil {
			panic(err)
		}
		placement, err := template.PlaceBlocks(paletteIndex, templateOrigin, rotation, structure.NoMirror, clip, 0, processors...)
		if err != nil {
			panic(err)
		}
		placement.TemplateUpdateFlags = 260
		placement.TemplateKnownShape = false
		region.f.applyStructurePlacement(region, placement)
	}
	place(fossil, config["fossil_processors"])
	place(overlay, config["overlay_processors"])
	return true
}
