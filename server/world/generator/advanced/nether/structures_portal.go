package nether

import (
	"fmt"
	"sort"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func (s *netherFeatureSource) placeRuinedPortalStep(pos world.ChunkPos, env javastructure.ProcessorEnvironment, emit func(javastructure.Placement)) {
	if env.Random == nil {
		panic("nether: Java ruined portal placement has no step random source")
	}
	const salt, spacing, separation = int32(34222645), int32(40), int32(15)
	targetX, targetZ := int32(pos[0]), int32(pos[1])
	clip := javastructure.Bounds{Min: [3]int32{targetX * 16, 0, targetZ * 16}, Max: [3]int32{targetX*16 + 15, 127, targetZ*16 + 15}}
	for sourceZ := targetZ - 2; sourceZ <= targetZ+2; sourceZ++ {
		for sourceX := targetX - 2; sourceX <= targetX+2; sourceX++ {
			candidate, err := javastructure.PotentialChunk(s.generator.seed, sourceX, sourceZ, salt, spacing, separation, javastructure.Triangular)
			if err != nil {
				panic(err)
			}
			if candidate[0] != sourceX || candidate[1] != sourceZ || env.Random.NextInt(7) != 6 {
				continue
			}
			placement, ok := s.buildNetherPortal([2]int32{sourceX, sourceZ}, clip, env)
			if ok {
				emit(placement)
			}
		}
	}
}

func (s *netherFeatureSource) buildNetherPortal(chunkPos [2]int32, clip javastructure.Bounds, env javastructure.ProcessorEnvironment) (javastructure.Placement, bool) {
	r := env.Random
	var templateName string
	if r.NextFloat() < 0.05 {
		templateName = fmt.Sprintf("ruined_portal/giant_portal_%d", r.NextInt(3)+1)
	} else {
		templateName = fmt.Sprintf("ruined_portal/portal_%d", r.NextInt(10)+1)
	}
	template := loadNetherTemplate(templateName)
	rotation := javastructure.Rotation(r.NextInt(4))
	mirror := javastructure.NoMirror
	if r.NextFloat() >= 0.5 {
		mirror = javastructure.FrontBack
	}
	pivot := [3]int32{template.Size[0] / 2, 0, template.Size[2] / 2}
	baseX, baseZ := chunkPos[0]*16, chunkPos[1]*16
	baseY := int32(32)
	basePosition := [3]int32{baseX, baseY, baseZ}
	adjustedOrigin := portalTemplateOrigin(basePosition, pivot, rotation, mirror)
	min, max := portalTemplateBounds(template, adjustedOrigin, pivot, rotation, mirror)
	centerX, centerZ := (min[0]+max[0])/2, (min[2]+max[2])/2
	surfaceY := env.BaseHeight(centerX, centerZ, false) - 1
	y := portalSuitableY(r, env, min, max, surfaceY)
	basePosition[1] = y
	adjustedOrigin = portalTemplateOrigin(basePosition, pivot, rotation, mirror)
	min, max = portalTemplateBounds(template, adjustedOrigin, pivot, rotation, mirror)
	center := [3]int32{(min[0] + max[0]) / 2, (min[1] + max[1]) / 2, (min[2] + max[2]) / 2}
	if center[0]>>4 != clip.Min[0]>>4 || center[2]>>4 != clip.Min[2]>>4 {
		return javastructure.Placement{}, false
	}
	clip = javastructure.Bounds{Min: min, Max: max}
	processors, err := javastructure.LoadProcessors(nil, netherPortalProcessorSpecs(), env)
	if err != nil {
		panic(fmt.Errorf("load Java Nether ruined portal processors: %w", err))
	}
	placement, err := template.Place(0, adjustedOrigin, rotation, mirror, clip, s.generator.seed, processors...)
	if err != nil {
		panic(fmt.Errorf("place Java Nether ruined portal %s: %w", templateName, err))
	}
	placed := make(map[[3]int32]javastructure.Block, len(placement.Blocks))
	for _, b := range placement.Blocks {
		javastructure.SeedRandomizableContainerLoot(&b, r)
		placed[b.Pos] = b
	}
	add := func(p [3]int32, name string) {
		placed[p] = javastructure.Block{Pos: p, State: world.BlockState{Name: name}}
	}
	nameAt := func(p [3]int32) string {
		if b, ok := placed[p]; ok {
			return b.State.Name
		}
		name, _ := env.BaseBlock(p[0], p[1], p[2]).EncodeBlock()
		return name
	}
	canReplace := func(p [3]int32) bool {
		name := nameAt(p)
		if name == "minecraft:air" || name == "minecraft:obsidian" {
			return false
		}
		protected, err := javadata.BlockTagContains("minecraft:features_cannot_replace", name)
		if err != nil {
			panic(fmt.Errorf("resolve Java portal protected-block tag: %w", err))
		}
		return !protected
	}
	placeNetherrackOrMagma := func(p [3]int32) {
		if canReplace(p) {
			if r.NextFloat() < 0.07 {
				add(p, "minecraft:magma_block")
			} else {
				add(p, "minecraft:netherrack")
			}
		}
	}
	drip := func(p [3]int32) {
		placeNetherrackOrMagma(p)
		for cap := 8; cap > 0 && r.NextFloat() < 0.5; cap-- {
			p[1]--
			placeNetherrackOrMagma(p)
		}
	}
	probabilities := [...]float64{1, 1, 1, 1, 1, 1, 1, .9, .9, .8, .7, .6, .4, .2}
	averageWidth := (max[0] - min[0] + max[2] - min[2] + 2) / 2
	distanceAdjustment := int32(r.NextInt(max32(1, 8-averageWidth/2)))
	for x := center[0] - 14; x <= center[0]+14; x++ {
		for z := center[2] - 14; z <= center[2]+14; z++ {
			distance := abs32(x-center[0]) + abs32(z-center[2]) + distanceAdjustment
			if distance < 0 {
				distance = 0
			}
			if distance >= int32(len(probabilities)) || r.NextDouble() >= probabilities[distance] {
				continue
			}
			surface := env.BaseHeight(x, z, false) - 1
			y := min32(min[1], surface)
			p := [3]int32{x, y, z}
			if abs32(y-min[1]) <= 3 && canReplace(p) {
				placeNetherrackOrMagma(p)
				drip([3]int32{x, y - 1, z})
			}
		}
	}
	for x := min[0] + 1; x < max[0]; x++ {
		for z := min[2] + 1; z < max[2]; z++ {
			p := [3]int32{x, min[1], z}
			if nameAt(p) == "minecraft:netherrack" {
				drip([3]int32{x, min[1] - 1, z})
			}
		}
	}
	placement.Blocks = placement.Blocks[:0]
	for _, b := range placed {
		placement.Blocks = append(placement.Blocks, b)
	}
	sort.Slice(placement.Blocks, func(i, j int) bool {
		a, b := placement.Blocks[i].Pos, placement.Blocks[j].Pos
		if a[0] != b[0] { return a[0] < b[0] }
		if a[1] != b[1] { return a[1] < b[1] }
		return a[2] < b[2]
	})
	placement, err = placement.AdaptItemFrameEntities()
	if err != nil {
		panic(fmt.Errorf("adapt Java Nether ruined portal: %w", err))
	}
	return placement, true
}

func portalTemplateOrigin(base, pivot [3]int32, rotation javastructure.Rotation, mirror javastructure.Mirror) [3]int32 {
	transformedPivot := javastructure.TransformPosition(pivot, mirror, rotation)
	return [3]int32{base[0] + pivot[0] - transformedPivot[0], base[1] + pivot[1] - transformedPivot[1], base[2] + pivot[2] - transformedPivot[2]}
}

func portalTemplateBounds(template *javastructure.Template, origin, pivot [3]int32, rotation javastructure.Rotation, mirror javastructure.Mirror) ([3]int32, [3]int32) {
	var min, max [3]int32
	for axis := range 3 { min[axis] = 1 << 30; max[axis] = -1 << 30 }
	for x := int32(0); x < 2; x++ {
		for y := int32(0); y < 2; y++ {
			for z := int32(0); z < 2; z++ {
				corner := [3]int32{x * (template.Size[0] - 1), y * (template.Size[1] - 1), z * (template.Size[2] - 1)}
				worldPos := javastructure.TransformPosition(corner, mirror, rotation)
				for axis := range 3 {
					worldPos[axis] += origin[axis]
					min[axis] = min32(min[axis], worldPos[axis])
					max[axis] = max32(max[axis], worldPos[axis])
				}
			}
		}
	}
	_ = pivot
	return min, max
}

func portalSuitableY(random *java26.Random, env javastructure.ProcessorEnvironment, min, max [3]int32, surfaceY int32) int32 {
	newY := int32(0)
	if random.NextFloat() < .5 {
		newY = 27 + random.NextInt(3)
	} else {
		newY = 29 + random.NextInt(72)
	}
	corners := [4][2]int32{{min[0], min[2]}, {max[0], min[2]}, {min[0], max[2]}, {max[0], max[2]}}
	for y := newY; y > 15; y-- {
		solid := 0
		for _, corner := range corners {
			name, _ := env.BaseBlock(corner[0], y, corner[1]).EncodeBlock()
			if name != "minecraft:air" && name != "minecraft:lava" && name != "minecraft:flowing_lava" {
				solid++
			}
		}
		if solid >= 3 { return y }
	}
	_ = surfaceY
	_ = max
	return 15
}

func netherPortalProcessorSpecs() []javastructure.ProcessorSpec {
	always := map[string]any{"predicate_type": "minecraft:always_true"}
	rule := func(block string, probability float64, output string) javastructure.ProcessorSpec {
		return javastructure.ProcessorSpec{Type: "rule", Config: map[string]any{"rules": []any{map[string]any{
			"input_predicate": map[string]any{"predicate_type": "minecraft:random_block_match", "block": block, "probability": probability},
			"location_predicate": always, "output_state": output,
		}}}}
	}
	return []javastructure.ProcessorSpec{
		{Type: "block_ignore", Config: map[string]any{"blocks": []any{"minecraft:air", "minecraft:structure_block"}}},
		rule("minecraft:gold_block", .3, "minecraft:air"),
		rule("minecraft:lava", .2, "minecraft:magma_block"),
		rule("minecraft:netherrack", .07, "minecraft:magma_block"),
		{Type: "block_age", Config: map[string]any{"mossiness": float64(0)}},
		{Type: "protected_blocks", Config: map[string]any{"value": "minecraft:features_cannot_replace"}},
		{Type: "lava_submerged_block", Config: map[string]any{}},
		{Type: "blackstone_replace", Config: map[string]any{}},
	}
}

func abs32(v int32) int32 { if v < 0 { return -v }; return v }

