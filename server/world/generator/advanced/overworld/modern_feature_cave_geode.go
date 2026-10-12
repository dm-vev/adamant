package overworld

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

// placeModernGeode ports GeodeFeature.place. Candidate points and voxel traversal
// retain Java's iteration order because provider sampling is part of the RNG contract.
var geodeDirections = [...][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
var geodeDirectionNames = [...]string{"down", "up", "north", "south", "west", "east"}

func placeModernGeode(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	blocks, ok := config["blocks"].(map[string]any)
	if !ok {
		panic("overworld: malformed Java geode blocks")
	}
	layers, _ := config["layers"].(map[string]any)
	crack, _ := config["crack"].(map[string]any)
	noiseRandom := java26.NewLegacyRandom(region.f.seed)
	noise := java26.NewNormalNoise(&noiseRandom, -4, 1)
	outerMax := geodeProviderMax(config["outer_wall_distance"], 5)
	numPoints := geodeIntProvider(config["distribution_points"], 3, 4, random)
	pointOffset := config["point_offset"]
	minOffset, maxOffset := geodeIntDefault(config["min_gen_offset"], -16), geodeIntDefault(config["max_gen_offset"], 16)
	crackAdjustment := float64(numPoints) / float64(outerMax)
	noiseMultiplier := geodeFloatDefault(config["noise_multiplier"], .05)
	invalidThreshold := geodeIntDefault(config["invalid_blocks_threshold"], 1)
	filling := geodeFloatDefault(layers["filling"], 1.7)
	inner := geodeFloatDefault(layers["inner_layer"], 2.2)
	middle := geodeFloatDefault(layers["middle_layer"], 3.2)
	outerLayer := geodeFloatDefault(layers["outer_layer"], 4.2)
	crackChance := geodeFloatDefault(crack["generate_crack_chance"], 1)
	baseCrackSize := geodeFloatDefault(crack["base_crack_size"], 2)
	crackOffset := geodeIntDefault(crack["crack_point_offset"], 2)
	placementChance := geodeFloatDefault(config["use_potential_placements_chance"], .35)
	requireAlternate := config["placements_require_layer0_alternate"] != false
	innerAir := 1 / math.Sqrt(filling)
	innermost := 1 / math.Sqrt(inner+crackAdjustment)
	innerCrust := 1 / math.Sqrt(middle+crackAdjustment)
	outerCrust := 1 / math.Sqrt(outerLayer+crackAdjustment)
	crackExtra := 0.0
	if numPoints > 3 {
		crackExtra = crackAdjustment
	}
	crackSize := 1 / math.Sqrt(baseCrackSize+random.NextDouble()/2+crackExtra)
	shouldCrack := float64(random.NextFloat()) < crackChance
	type geodePoint struct {
		p      featurePos
		offset int32
	}
	points := make([]geodePoint, 0, numPoints)
	invalid := 0
	invalidBlocks := geodeBlockSet(blocks["invalid_blocks"])
	for range numPoints {
		x, y, z := geodeIntProvider(config["outer_wall_distance"], 4, 5, random), geodeIntProvider(config["outer_wall_distance"], 4, 5, random), geodeIntProvider(config["outer_wall_distance"], 4, 5, random)
		p := featurePos{origin.x + x, origin.y + y, origin.z + z}
		name := geodeBlockName(region, p)
		if name == "minecraft:air" || name == "minecraft:cave_air" || name == "minecraft:void_air" || invalidBlocks.contains(name) {
			invalid++
			if invalid > invalidThreshold {
				return false
			}
		}
		points = append(points, geodePoint{p, geodeIntProvider(pointOffset, 1, 2, random)})
	}
	type point3 struct{ x, y, z int32 }
	crackPoints := make([]point3, 0, 3)
	if shouldCrack {
		v := random.NextInt(4)
		d := int32(numPoints*2 + 1)
		switch v {
		case 0:
			crackPoints = append(crackPoints, point3{origin.x + d, origin.y + 7, origin.z}, point3{origin.x + d, origin.y + 5, origin.z}, point3{origin.x + d, origin.y + 1, origin.z})
		case 1:
			crackPoints = append(crackPoints, point3{origin.x, origin.y + 7, origin.z + d}, point3{origin.x, origin.y + 5, origin.z + d}, point3{origin.x, origin.y + 1, origin.z + d})
		case 2:
			crackPoints = append(crackPoints, point3{origin.x + d, origin.y + 7, origin.z + d}, point3{origin.x + d, origin.y + 5, origin.z + d}, point3{origin.x + d, origin.y + 1, origin.z + d})
		default:
			crackPoints = append(crackPoints, point3{origin.x, origin.y + 7, origin.z}, point3{origin.x, origin.y + 5, origin.z}, point3{origin.x, origin.y + 1, origin.z})
		}
	}
	cannotReplace := geodeBlockSet(blocks["cannot_replace"])
	altChance := geodeFloatDefault(config["use_alternate_layer0_chance"], 0)
	potential := make([]featurePos, 0)
	airState, ok := featureStateRID(region.c, map[string]any{"id": "minecraft:air"})
	if !ok {
		panic("overworld: unregistered Java geode air state")
	}
	for z := origin.z + int32(minOffset); z <= origin.z+int32(maxOffset); z++ {
		for y := origin.y + int32(minOffset); y <= origin.y+int32(maxOffset); y++ {
			for x := origin.x + int32(minOffset); x <= origin.x+int32(maxOffset); x++ {
				p := featurePos{x, y, z}
				noiseOffset := float64(noise.Sample(float64(x), float64(y), float64(z))) * noiseMultiplier
				shell := 0.0
				for _, q := range points {
					dx, dy, dz := float64(x-q.p.x), float64(y-q.p.y), float64(z-q.p.z)
					shell += 1/math.Sqrt(dx*dx+dy*dy+dz*dz+float64(q.offset)) + noiseOffset
				}
				if shell < outerCrust {
					continue
				}
				stateProvider := any(nil)
				isAir := false
				sampled := false
				var sampledState world.NativeState
				var sampledActor world.Block
				if shell >= innerAir {
					stateProvider = blocks["filling_provider"]
				} else {
					crackDistance := 0.0
					for _, q := range crackPoints {
						dx, dy, dz := float64(x-q.x), float64(y-q.y), float64(z-q.z)
						crackDistance += 1/math.Sqrt(dx*dx+dy*dy+dz*dz+float64(crackOffset)) + noiseOffset
					}
					if shouldCrack && crackDistance >= crackSize {
						isAir = true
					} else if shell >= innermost {
						alternate := float64(random.NextFloat()) < altChance
						if alternate {
							stateProvider = blocks["alternate_inner_layer_provider"]
						} else {
							stateProvider = blocks["inner_layer_provider"]
						}
						var sampleOK bool
						sampledState, sampledActor, sampleOK = region.SampleNativeState(stateProvider, p, random)
						if !sampleOK {
							panic("overworld: unresolved Java geode block provider")
						}
						sampled = true
						if (!requireAlternate || alternate) && float64(random.NextFloat()) < placementChance {
							potential = append(potential, p)
						}
					} else if shell >= innerCrust {
						stateProvider = blocks["middle_layer_provider"]
					} else {
						stateProvider = blocks["outer_layer_provider"]
					}
				}
				name := geodeBlockName(region, p)
				canReplace := !cannotReplace.contains(name)
				if isAir {
					if canReplace {
						region.Set(p, airState)
					}
					for _, d := range geodeDirections {
						adjacent := featurePos{p.x + d[0], p.y + d[1], p.z + d[2]}
						fluidName := geodeFluidName(region, adjacent)
						if fluidName == "empty" {
							continue
						}
						if featureInsideTarget(region.target, adjacent) {
							region.result.ScheduleTick(chunk.ScheduledTick{
								Pos: cube.Pos{int(adjacent.x), int(adjacent.y), int(adjacent.z)}, Kind: chunk.FluidTick, Type: "minecraft:" + fluidName,
							})
						}
					}
				} else {
					if !sampled {
						var sampleOK bool
						sampledState, sampledActor, sampleOK = region.SampleNativeState(stateProvider, p, random)
						if !sampleOK {
							panic("overworld: unresolved Java geode block provider")
						}
					}
					if canReplace {
						geodeSetNativeBlock(region, p, sampledState, sampledActor)
					}
				}
			}
		}
	}
	placements, ok := blocks["inner_placements"].([]any)
	if !ok {
		panic("overworld: malformed Java geode inner placements")
	}
	for _, p := range potential {
		if len(placements) == 0 {
			panic("overworld: empty Java geode inner placements")
		}
		placement := placements[random.NextInt(int32(len(placements)))]
		state, ok := placement.(string)
		if !ok {
			panic("overworld: malformed Java geode inner placement")
		}
		for _, d := range geodeDirections {
			q := featurePos{p.x + d[0], p.y + d[1], p.z + d[2]}
			name := geodeBlockName(region, q)
			fluid := geodeFluidName(region, q)
			if name != "minecraft:air" && name != "minecraft:cave_air" && name != "minecraft:void_air" && !(name == "minecraft:water" && fluid == "water") {
				continue
			}
			stateName := normalizeJavaResource(state)
			properties := map[string]any{}
			if stateName != "small_amethyst_bud" && stateName != "medium_amethyst_bud" && stateName != "large_amethyst_bud" && stateName != "amethyst_cluster" {
				panic("overworld: unsupported Java geode placement " + state)
			}
			properties["facing"] = faceForGeode(d, geodeDirectionNames[:])
			properties["waterlogged"] = fluid == "water" || fluid == "lava"
			rid, found := featureStateRID(region.c, map[string]any{"id": "minecraft:" + stateName, "properties": properties})
			if !found {
				panic("overworld: unregistered Java geode crystal state")
			}
			if !cannotReplace.contains(name) {
				region.Set(q, rid)
			}
			break
		}
	}
	return true
}
func geodeSetNativeBlock(region *featureRegion, pos featurePos, state world.NativeState, actor world.Block) bool {
	if !region.setWithFlags(pos, state, 16) {
		return false
	}
	if actor != nil {
		region.recordTemplateActor(javastructure.Block{
			Pos:         [3]int32{pos.x, pos.y, pos.z},
			NativeBlock: actor,
		})
	}
	id := state.CanonicalJavaID(region.c.BlockRegistry())
	name, ok := javadata.BlockStateName(id)
	if !ok {
		name, _, ok = region.c.BlockRegistry().RuntimeIDToState(state.Block)
		if !ok {
			panic("overworld: geode write has unknown Source block state")
		}
	}
	marker := pos
	switch normalizeJavaResource(name) {
	case "brown_mushroom", "red_mushroom":
	case "soul_sand", "magma_block":
		marker.y++
	default:
		return true
	}
	if featureInsideTarget(region.target, marker) {
		region.result.MarkForPostProcessing(cube.Pos{int(marker.x), int(marker.y), int(marker.z)}, region.c.Range())
	}
	return true
}

func faceForGeode(d [3]int32, names []string) string {
	if d[1] < 0 {
		return names[0]
	}
	if d[1] > 0 {
		return names[1]
	}
	if d[2] < 0 {
		return names[2]
	}
	if d[2] > 0 {
		return names[3]
	}
	if d[0] < 0 {
		return names[4]
	}
	return names[5]
}

type geodeHolderSet struct {
	tag    string
	tags   []string
	blocks map[string]struct{}
}

func geodeBlockSet(raw any) geodeHolderSet {
	set := geodeHolderSet{}
	switch value := raw.(type) {
	case string:
		if strings.HasPrefix(value, "#") {
			set.tag = normalizeJavaResource(strings.TrimPrefix(value, "#"))
		} else {
			set.blocks = map[string]struct{}{normalizeJavaResource(value): {}}
		}
	case []any:
		for _, entry := range value {
			name, ok := entry.(string)
			if !ok {
				panic("overworld: malformed Java geode block holder set")
			}
			if strings.HasPrefix(name, "#") {
				set.tags = append(set.tags, normalizeJavaResource(strings.TrimPrefix(name, "#")))
				continue
			}
			if set.blocks == nil {
				set.blocks = make(map[string]struct{}, len(value))
			}
			set.blocks[normalizeJavaResource(name)] = struct{}{}
		}
	case []string:
		for _, name := range value {
			if strings.HasPrefix(name, "#") {
				set.tags = append(set.tags, normalizeJavaResource(strings.TrimPrefix(name, "#")))
				continue
			}
			if set.blocks == nil {
				set.blocks = make(map[string]struct{}, len(value))
			}
			set.blocks[normalizeJavaResource(name)] = struct{}{}
		}
	default:
		panic("overworld: malformed Java geode block holder set")
	}
	return set
}

func (s geodeHolderSet) contains(name string) bool {
	name = normalizeJavaResource(name)
	if _, ok := s.blocks[name]; ok {
		return true
	}
	if s.tag != "" && javaBlockTagContains(s.tag, name) {
		return true
	}
	for _, tag := range s.tags {
		if javaBlockTagContains(tag, name) {
			return true
		}
	}
	return false
}

func geodeBlockName(region *featureRegion, pos featurePos) string {
	if pos.y < region.MinY() || pos.y > region.MaxY() {
		return "minecraft:void_air"
	}
	state := region.nativeState(pos)
	if name, ok := javadata.BlockStateName(state.CanonicalJavaID(region.c.BlockRegistry())); ok {
		return name
	}
	name, _, ok := region.c.BlockRegistry().RuntimeIDToState(state.Block)
	if !ok {
		panic("overworld: geode encountered unknown native block state")
	}
	return name
}

func geodeFluidName(region *featureRegion, pos featurePos) string {
	if pos.y < region.MinY() || pos.y > region.MaxY() {
		return "empty"
	}
	return featureFluidName(region, pos, nil)
}
func geodeFloatDefault(v any, d float64) float64 {
	if n, ok := v.(float64); ok {
		return n
	}
	return d
}
func geodeIntDefault(v any, d int) int {
	if n, ok := v.(float64); ok {
		return int(n)
	}
	return d
}
func geodeIntProvider(v any, min, max int, random *java26.Random) int32 {
	if n, ok := v.(float64); ok {
		return int32(n)
	}
	if p, ok := v.(map[string]any); ok {
		switch p["type"] {
		case "minecraft:uniform":
			min = geodeIntDefault(p["min_inclusive"], min)
			max = geodeIntDefault(p["max_inclusive"], max)
		case "minecraft:constant":
			return int32(geodeIntDefault(p["value"], min))
		default:
			panic("overworld: unsupported Java geode integer provider")
		}
	}
	if min == max {
		return int32(min)
	}
	return int32(min) + random.NextInt(int32(max-min+1))
}
func geodeProviderMax(v any, fallback int) int {
	if p, ok := v.(map[string]any); ok {
		switch p["type"] {
		case "minecraft:uniform":
			return geodeIntDefault(p["max_inclusive"], fallback)
		case "minecraft:constant":
			return geodeIntDefault(p["value"], fallback)
		}
	}
	if value, ok := v.(float64); ok {
		return int(value)
	}
	return fallback
}

func placeModernCaveFamily(region *featureRegion, typeName string, config map[string]any, origin featurePos, random *java26.Random) (handled, placed bool) {
	switch typeName {
	case "underwater_magma":
		return true, placeModernUnderwaterMagma(region, config, origin, random)
	case "geode":
		return true, placeModernGeode(region, config, origin, random)
	case "fossil":
		return true, placeModernFossil(region, config, origin, random)
	case "large_dripstone":
		return true, placeModernLargeDripstone(region, config, origin, random)
	case "speleothem":
		return true, placeModernSpeleothemExact(region, config, origin, random)
	case "speleothem_cluster":
		return true, placeModernSpeleothemClusterExact(region, config, origin, random)
	case "vegetation_patch":
		return true, placeModernVegetationPatch(region, config, origin, random, false)
	case "waterlogged_vegetation_patch":
		return true, placeModernVegetationPatch(region, config, origin, random, true)
	case "multiface_growth":
		return true, placeModernMultifaceGrowth(region, config, origin, random)
	case "monster_room":
		return true, placeModernMonsterRoom(region, origin, random)
	}
	if handled, placed := placeModernIceFamily(region, typeName, config, origin, random); handled {
		return true, placed
	}
	return false, false
}
