package overworld

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

// placeModernTreeFamily dispatches the Java 26.3 tree-shaped configured feature families.
func placeModernTreeFamily(region *featureRegion, typeName string, config map[string]any, origin featurePos, r *java26.Random) (bool, bool) {
	switch normalizeJavaResource(typeName) {
	case "tree":
		return true, placeModernTreeRegion(region, config, origin, r)
	case "fallen_tree":
		return true, placeModernFallenTree(region, config, origin, r)
	case "bamboo":
		return true, placeModernBamboo(region, config, origin, r)
	case "huge_brown_mushroom", "huge_red_mushroom":
		return true, placeModernHugeMushroom(region, config, origin, r)
	case "huge_fungus":
		return true, placeModernHugeFungus(region, config, origin, r)
	case "root_system":
		return true, placeModernRootSystem(region, config, origin, r)
	default:
		return false, false
	}
}

func treeInt(v any, fallback int32, r *java26.Random) int32 {
	if v == nil {
		return fallback
	}
	if p, ok := v.(map[string]any); ok {
		return sampleJavaInt(p, r)
	}
	return int32(featureNumber(v))
}
func treeMap(v any) map[string]any     { m, _ := v.(map[string]any); return m }
func treeName(m map[string]any) string { s, _ := m["type"].(string); return normalizeJavaResource(s) }
func treeAirOrLeaves(region *featureRegion, p featurePos) bool {
	n, _ := region.State(p)
	n = normalizeJavaResource(n)
	return n == "air" || n == "cave_air" || n == "void_air" || treeIsLeaves(n)
}
func treeIsAir(region *featureRegion, p featurePos) bool {
	n, _ := region.State(p)
	switch normalizeJavaResource(n) {
	case "air", "cave_air", "void_air":
		return true
	}
	return false
}
func treeIsLeaves(n string) bool { return javaBlockTagContains("leaves", normalizeJavaResource(n)) }
func treeReplaceable(region *featureRegion, p featurePos) bool {
	n, _ := region.State(p)
	n = normalizeJavaResource(n)
	return n == "air" || n == "cave_air" || n == "void_air" || treeTagContains("#minecraft:replaceable_by_trees", n)
}
func treeSet(region *featureRegion, p featurePos, provider any, r *java26.Random) bool {
	state, actor, ok := region.SampleNativeState(provider, p, r)
	return ok && region.SetNativeBlock(p, state, actor)
}
func treeSetLiteral(region *featureRegion, p featurePos, value any) bool {
	placement, ok := featureStateBlock(value)
	if !ok {
		panic("overworld: malformed literal tree block state")
	}
	state, actor := region.resolveFeatureBlock(placement, p)
	return region.SetNativeBlock(p, state, actor)
}
func treeSetAt(region *featureRegion, p, samplePos featurePos, provider any, r *java26.Random) bool {
	state, actor, ok := region.SampleNativeState(provider, samplePos, r)
	return ok && region.SetNativeBlock(p, state, actor)
}
func treeSetNamed(region *featureRegion, p featurePos, name string, props map[string]any) bool {
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	provider := map[string]any{"id": name}
	if len(props) > 0 {
		provider["properties"] = props
	}
	state, actor, ok := region.SampleNativeState(provider, p, &java26.Random{})
	return ok && region.SetNativeBlock(p, state, actor)
}
func treeWaterSource(region *featureRegion, p featurePos) bool {
	state, err := world.JavaState(region.c.BlockRegistry(), region.nativeState(p))
	if err != nil {
		panic(err)
	}
	if normalizeJavaResource(state.Name) == "water" {
		return state.Properties["level"] == nil || fmt.Sprint(state.Properties["level"]) == "0"
	}
	return state.Properties["waterlogged"] == true || state.Properties["waterlogged"] == "true"
}
func treeSetFoliage(region *featureRegion, p featurePos, provider any, r *java26.Random) bool {
	state, actor, ok := region.SampleNativeState(provider, p, r)
	if !ok {
		return false
	}
	javaState, err := world.JavaState(region.c.BlockRegistry(), state)
	if err != nil {
		panic(err)
	}
	if _, exists := javaState.Properties["waterlogged"]; exists {
		javaState.Properties["waterlogged"] = treeWaterSource(region, p)
		state, err = world.ResolveState(region.c.BlockRegistry(), javaState)
		if err != nil {
			panic(err)
		}
	}
	return region.SetNativeBlock(p, state, actor)
}
func treeAxis(region *featureRegion, p featurePos, provider any, axis string, r *java26.Random) bool {
	state, actor, ok := region.SampleNativeState(provider, p, r)
	if !ok {
		return false
	}
	javaState, err := world.JavaState(region.c.BlockRegistry(), state)
	if err != nil {
		panic(err)
	}
	if javaState.Properties == nil {
		javaState.Properties = map[string]any{}
	}
	javaState.Properties["axis"] = axis
	state, err = world.ResolveState(region.c.BlockRegistry(), javaState)
	if err != nil {
		panic(err)
	}
	return region.SetNativeBlock(p, state, actor)
}
func treeSetWaterlogged(region *featureRegion, p featurePos, provider any, r *java26.Random) bool {
	state, actor, ok := region.SampleNativeState(provider, p, r)
	if !ok {
		return false
	}
	javaState, err := world.JavaState(region.c.BlockRegistry(), state)
	if err != nil {
		panic(err)
	}
	if _, exists := javaState.Properties["waterlogged"]; exists {
		javaState.Properties["waterlogged"] = treeWaterSource(region, p)
		state, err = world.ResolveState(region.c.BlockRegistry(), javaState)
		if err != nil {
			panic(err)
		}
	}
	return region.SetNativeBlock(p, state, actor)
}
func treePos(a featurePos, dx, dy, dz int32) featurePos {
	return featurePos{a.x + dx, a.y + dy, a.z + dz}
}
func treeTagContains(tag, name string) bool {
	tag = strings.TrimPrefix(tag, "#")
	return javaBlockTagContains(normalizeJavaResource(tag), normalizeJavaResource(name))
}
func placeTreeBelowTrunk(region *featureRegion, provider any, origin featurePos, trunkType string, r *java26.Random, trunks *[]featurePos) {
	points := [4]featurePos{treePos(origin, 0, -1, 0), treePos(origin, 1, -1, 0), treePos(origin, 0, -1, 1), treePos(origin, 1, -1, 1)}
	count := 1
	if trunkType == "giant_trunk_placer" || trunkType == "dark_oak_trunk_placer" || trunkType == "mega_jungle_trunk_placer" {
		count = 4
	}
	for _, p := range points[:count] {
		placement, ok := sampleOptionalJavaBlockState(region, p, provider, r)
		if !ok {
			continue
		}
		state, actor := region.resolveFeatureBlock(placement, p)
		if region.SetNativeBlock(p, state, actor) {
			*trunks = append(*trunks, p)
		}
	}
}
func placeMangroveRoots(region *featureRegion, root map[string]any, origin, trunkOrigin featurePos, r *java26.Random, roots *[]featurePos) bool {
	rootConfig := treeMap(root["mangrove_root_placement"])
	maxLength := treeInt(rootConfig["max_root_length"], 15, r)
	maxWidth := treeInt(rootConfig["max_root_width"], 8, r)
	skew := float32(treeProbability(rootConfig, "random_skew_chance", .2))
	canPlace := func(p featurePos) bool {
		n, _ := region.State(p)
		return treeReplaceable(region, p) || treeTagContains(fmt.Sprint(rootConfig["can_grow_through"]), n)
	}
	for y := origin.y; y < trunkOrigin.y; y++ {
		if !canPlace(featurePos{origin.x, y, origin.z}) {
			return false
		}
	}
	directions := [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}
	for _, dir := range directions {
		rootPos := treePos(trunkOrigin, dir[0], 0, dir[1])
		local := make([]featurePos, 0, maxLength+1)
		var simulate func(featurePos, int32) bool
		simulate = func(pos featurePos, layer int32) bool {
			if layer == maxLength || int32(len(local)) > maxLength {
				return false
			}
			width := abs32(pos.x-trunkOrigin.x) + abs32(pos.y-trunkOrigin.y) + abs32(pos.z-trunkOrigin.z)
			below := treePos(pos, 0, -1, 0)
			nextTo := treePos(pos, dir[0], 0, dir[1])
			choices := []featurePos{below}
			if width > maxWidth-3 && width <= maxWidth {
				if r.NextFloat() < skew {
					choices = append(choices, treePos(nextTo, 0, -1, 0))
				}
			} else if width <= maxWidth {
				if r.NextFloat() >= skew && r.NextBoolean() {
					choices = []featurePos{nextTo}
				}
			}
			for _, candidate := range choices {
				if !canPlace(candidate) {
					continue
				}
				local = append(local, candidate)
				if !simulate(candidate, layer+1) {
					return false
				}
			}
			return true
		}
		if !simulate(rootPos, 0) {
			return false
		}
		*roots = append(*roots, local...)
		*roots = append(*roots, rootPos)
	}
	*roots = append([]featurePos{treePos(trunkOrigin, 0, -1, 0)}, (*roots)...)
	above := treeMap(root["above_root_placement"])
	for _, p := range *roots {
		if !canPlace(p) {
			continue
		}
		n, _ := region.State(p)
		provider := root["root_provider"]
		for _, mud := range asAnySlice(rootConfig["muddy_roots_in"]) {
			if normalizeJavaResource(fmt.Sprint(mud)) == normalizeJavaResource(n) {
				provider = rootConfig["muddy_roots_provider"]
				break
			}
		}
		treeSetWaterlogged(region, p, provider, r)
		if len(above) > 0 && r.NextFloat() < float32(treeProbability(above, "above_root_placement_chance", 0)) && treeIsAir(region, treePos(p, 0, 1, 0)) {
			treeSetWaterlogged(region, treePos(p, 0, 1, 0), above["above_root_provider"], r)
		}
		*roots = append(*roots, p)
	}
	return true
}
func treeFree(region *featureRegion, p featurePos) bool {
	n, _ := region.State(p)
	n = normalizeJavaResource(n)
	return treeReplaceable(region, p) || treeTagContains("#minecraft:logs", n)
}
func treeDirection(index int32) [2]int32 {
	switch index {
	case 0:
		return [2]int32{0, -1}
	case 1:
		return [2]int32{1, 0}
	case 2:
		return [2]int32{0, 1}
	default:
		return [2]int32{-1, 0}
	}
}
func treeSampleUniform(raw any, r *java26.Random) int32 {
	m := treeMap(raw)
	minValue := int32(featureNumber(m["min_inclusive"]))
	maxValue := int32(featureNumber(m["max_inclusive"]))
	return minValue + r.NextInt(maxValue-minValue+1)
}
func treeCherryBranch(origin featurePos, height, trunkHeight, offset int32, middleContinues bool, dir [2]int32, placer map[string]any, r *java26.Random, placeLog func(featurePos, string) bool) treeFoliageAttachment {
	endOffset := height - 1 + treeInt(placer["branch_end_offset_from_top"], 0, r)
	extend := middleContinues || endOffset < offset
	distance := treeInt(placer["branch_horizontal_length"], 0, r)
	if extend {
		distance++
	}
	end := featurePos{origin.x + dir[0]*distance, origin.y + endOffset, origin.z + dir[1]*distance}
	steps := int32(1)
	if extend {
		steps = 2
	}
	pos := treePos(origin, 0, offset, 0)
	axis := "z"
	if dir[0] != 0 {
		axis = "x"
	}
	for i := int32(0); i < steps; i++ {
		pos.x += dir[0]
		pos.z += dir[1]
		placeLog(pos, axis)
	}
	vertical := int32(1)
	if end.y < pos.y {
		vertical = -1
	}
	for {
		distance := abs32(end.x-pos.x) + abs32(end.y-pos.y) + abs32(end.z-pos.z)
		if distance == 0 {
			return treeFoliageAttachment{pos: treePos(end, 0, 1, 0)}
		}
		chance := float32(abs32(end.y-pos.y)) / float32(distance)
		if r.NextFloat() < chance {
			pos.y += vertical
			placeLog(pos, "y")
		} else {
			pos.x += dir[0]
			pos.z += dir[1]
			placeLog(pos, axis)
		}
	}
}
func treeShuffleDirections(r *java26.Random) [][2]int32 {
	all := [][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
	for i := len(all) - 1; i > 0; i-- {
		j := int(r.NextInt(int32(i + 1)))
		all[i], all[j] = all[j], all[i]
	}
	out := make([][2]int32, 0, 4)
	for _, d := range all {
		if d[1] == 0 {
			out = append(out, [2]int32{d[0], d[2]})
		}
	}
	return out
}
func treeFancyShape(height, y int32) float32 {
	if float32(y) < float32(height)*0.3 {
		return -1
	}
	radius := float32(height) / 2
	adjacent := radius - float32(y)
	distance := float32(math.Sqrt(float64(radius*radius - adjacent*adjacent)))
	if adjacent == 0 {
		distance = radius
	} else if float32(math.Abs(float64(adjacent))) >= radius {
		return 0
	}
	return distance * 0.5
}
func treeFancyLimb(region *featureRegion, placeLog func(featurePos, string) bool, start, end featurePos, doPlace bool) bool {
	if !doPlace && start == end {
		return true
	}
	dx, dy, dz := end.x-start.x, end.y-start.y, end.z-start.z
	steps := max(abs32(dx), max(abs32(dy), abs32(dz)))
	xStep, yStep, zStep := float32(dx)/float32(steps), float32(dy)/float32(steps), float32(dz)/float32(steps)
	for i := int32(0); i <= steps; i++ {
		p := featurePos{start.x + int32(math.Floor(float64(float32(0.5)+float32(i)*xStep))), start.y + int32(math.Floor(float64(float32(0.5)+float32(i)*yStep))), start.z + int32(math.Floor(float64(float32(0.5)+float32(i)*zStep)))}
		if doPlace {
			axis := "y"
			xdiff, zdiff := abs32(p.x-start.x), abs32(p.z-start.z)
			if max(xdiff, zdiff) > 0 {
				if xdiff >= zdiff {
					axis = "x"
				} else {
					axis = "z"
				}
			}
			placeLog(p, axis)
		} else if !treeFree(region, p) {
			return false
		}
	}
	return true
}

type treeFoliageAttachment struct {
	pos                 featurePos
	radiusOffset        int32
	foliageHeightOffset int32
	doubleTrunk         bool
}

func placeModernTreeRegion(region *featureRegion, config map[string]any, origin featurePos, r *java26.Random) bool {
	trunkPlacer, foliagePlacer := treeMap(config["trunk_placer"]), treeMap(config["foliage_placer"])
	if trunkPlacer == nil || foliagePlacer == nil {
		panic("overworld: malformed Java tree placer")
	}
	trunkProvider, foliageProvider := config["trunk_provider"], config["foliage_provider"]
	baseHeight := treeInt(trunkPlacer["base_height"], 0, r)
	heightRandA := treeInt(trunkPlacer["height_rand_a"], 0, r)
	heightRandB := treeInt(trunkPlacer["height_rand_b"], 0, r)
	height := baseHeight + r.NextInt(heightRandA+1) + r.NextInt(heightRandB+1)
	if height <= 0 || height > 80 {
		return false
	}
	root := treeMap(config["root_placer"])
	foliageType := treeName(foliagePlacer)
	foliageHeight := int32(0)
	switch foliageType {
	case "pine_foliage_placer", "cherry_foliage_placer", "poplar_foliage_placer":
		foliageHeight = treeInt(foliagePlacer["height"], 0, r)
	case "spruce_foliage_placer":
		foliageHeight = max(int32(4), height-treeInt(foliagePlacer["trunk_height"], 0, r))
	case "mega_pine_foliage_placer":
		foliageHeight = treeInt(foliagePlacer["crown_height"], 0, r)
	case "mega_jungle_foliage_placer":
		foliageHeight = treeInt(foliagePlacer["height"], 0, r)
	case "blob_foliage_placer", "bush_foliage_placer", "fancy_foliage_placer":
		foliageHeight = treeInt(foliagePlacer["height"], 0, r)
	case "acacia_foliage_placer":
		foliageHeight = 0
	case "dark_oak_foliage_placer":
		foliageHeight = 4
	case "random_spread_foliage_placer":
		foliageHeight = treeInt(foliagePlacer["foliage_height"], 0, r)
	default:
		panic("overworld: unsupported Java tree foliage placer " + foliageType)
	}
	trunkHeight := height - foliageHeight
	radius := treeInt(foliagePlacer["radius"], 2, r)
	if foliageType == "pine_foliage_placer" {
		radius += r.NextInt(max(trunkHeight+1, int32(1)))
	}
	trunkOrigin := origin
	if root != nil {
		trunkOrigin.y += treeInt(root["trunk_offset_y"], 0, r)
	}
	minY := min(origin.y, trunkOrigin.y)
	maxY := max(origin.y, trunkOrigin.y) + height + 1
	if minY < region.MinY()+1 || maxY > region.MaxY()+1 {
		return false
	}
	minimum := treeMap(config["minimum_size"])
	if minimum == nil {
		minimum = map[string]any{}
	}
	minClipped := treeInt(minimum["min_clipped_height"], -1, r)
	freeHeight := height
	ignoreVines, _ := config["ignore_vines"].(bool)
	for y := int32(0); y <= height+1; y++ {
		rad := treeMinimumRadius(minimum, height, y)
		for x := -rad; x <= rad; x++ {
			for z := -rad; z <= rad; z++ {
				p := treePos(trunkOrigin, x, y, z)
				n, _ := region.State(p)
				n = normalizeJavaResource(n)
				if !treeFree(region, p) || (!ignoreVines && n == "vine") {
					freeHeight = y - 2
					goto clipped
				}
			}
		}
	}
clipped:
	if freeHeight < height && (minClipped < 0 || freeHeight < minClipped) {
		return false
	}
	if freeHeight < 0 {
		return false
	}
	height = freeHeight
	trunks := make([]featurePos, 0, height*2)
	roots := make([]featurePos, 0, 8)
	leaves := make([]featurePos, 0, height*4)
	trunkType := treeName(trunkPlacer)
	placeLog := func(p featurePos, axis string) bool {
		if p.y < region.MinY() || p.y > region.MaxY() || (!treeReplaceable(region, p) && !(trunkType == "upwards_branching_trunk_placer" && treeTagContains(fmt.Sprint(trunkPlacer["can_grow_through"]), func() string { n, _ := region.State(p); return n }()))) {
			return false
		}
		if treeAxis(region, p, trunkProvider, axis, r) {
			trunks = append(trunks, p)
			return true
		}
		return false
	}
	if root != nil && treeName(root) == "mangrove_root_placer" {
		if !placeMangroveRoots(region, root, origin, trunkOrigin, r, &roots) {
			return false
		}
	}
	foliageAnchors := []treeFoliageAttachment{{pos: featurePos{trunkOrigin.x, trunkOrigin.y + height, trunkOrigin.z}}}
	switch trunkType {
	case "giant_trunk_placer":
		placeTreeBelowTrunk(region, config["below_trunk_provider"], trunkOrigin, trunkType, r, &trunks)
		placeLogFree := func(p featurePos, axis string) {
			if treeFree(region, p) {
				placeLog(p, axis)
			}
		}
		for y := int32(0); y < height; y++ {
			placeLogFree(treePos(trunkOrigin, 0, y, 0), "y")
			if y < height-1 {
				placeLogFree(treePos(trunkOrigin, 1, y, 0), "y")
				placeLogFree(treePos(trunkOrigin, 1, y, 1), "y")
				placeLogFree(treePos(trunkOrigin, 0, y, 1), "y")
			}
		}
		foliageAnchors = []treeFoliageAttachment{{pos: treePos(trunkOrigin, 0, height, 0), doubleTrunk: true}}
	case "dark_oak_trunk_placer":
		placeTreeBelowTrunk(region, config["below_trunk_provider"], trunkOrigin, trunkType, r, &trunks)
		dir := treeDirection(r.NextInt(4))
		leanHeight := height - r.NextInt(4)
		leanSteps := int32(2) - r.NextInt(3)
		tx, tz := trunkOrigin.x, trunkOrigin.z
		ey := trunkOrigin.y + height - 1
		for dy := int32(0); dy < height; dy++ {
			if dy >= leanHeight && leanSteps > 0 {
				tx += dir[0]
				tz += dir[1]
				leanSteps--
			}
			p := featurePos{tx, trunkOrigin.y + dy, tz}
			if treeAirOrLeaves(region, p) {
				placeLog(p, "y")
				placeLog(treePos(p, 1, 0, 0), "y")
				placeLog(treePos(p, 0, 0, 1), "y")
				placeLog(treePos(p, 1, 0, 1), "y")
			}
		}
		foliageAnchors = []treeFoliageAttachment{{pos: featurePos{tx, ey, tz}, doubleTrunk: true}}
		for ox := int32(-1); ox <= 2; ox++ {
			for oz := int32(-1); oz <= 2; oz++ {
				if (ox < 0 || ox > 1 || oz < 0 || oz > 1) && r.NextInt(3) <= 0 {
					length := r.NextInt(3) + 2
					for by := int32(0); by < length; by++ {
						placeLog(featurePos{trunkOrigin.x + ox, ey - by - 1, trunkOrigin.z + oz}, "y")
					}
					foliageAnchors = append(foliageAnchors, treeFoliageAttachment{pos: featurePos{trunkOrigin.x + ox, ey, trunkOrigin.z + oz}})
				}
			}
		}
	case "straight_trunk_placer":
		placeTreeBelowTrunk(region, config["below_trunk_provider"], trunkOrigin, trunkType, r, &trunks)
		for y := int32(0); y < height; y++ {
			placeLog(treePos(trunkOrigin, 0, y, 0), "y")
		}
	case "fancy_trunk_placer":
		placeTreeBelowTrunk(region, config["below_trunk_provider"], trunkOrigin, trunkType, r, &trunks)
		fancyHeight := height + 2
		trunkHeight := int32(math.Floor(float64(fancyHeight) * .618))
		trunkTop := trunkOrigin.y + trunkHeight
		type branch struct {
			anchor treeFoliageAttachment
			base   int32
		}
		branches := []branch{{treeFoliageAttachment{pos: treePos(trunkOrigin, 0, fancyHeight-5, 0)}, trunkTop}}
		for relativeY := fancyHeight - 5; relativeY >= 0; relativeY-- {
			shape := treeFancyShape(fancyHeight, relativeY)
			if shape < 0 {
				continue
			}
			rad := float64(shape) * (float64(r.NextFloat()) + .328)
			angle := float64(r.NextFloat()*2) * math.Pi
			start := treePos(trunkOrigin, int32(math.Floor(rad*math.Sin(angle)+.5)), relativeY-1, int32(math.Floor(rad*math.Cos(angle)+.5)))
			end := treePos(start, 0, 5, 0)
			if !treeFancyLimb(region, placeLog, start, end, false) {
				continue
			}
			dx, dz := trunkOrigin.x-start.x, trunkOrigin.z-start.z
			branchY := float64(start.y) - math.Sqrt(float64(dx*dx+dz*dz))*.381
			branchTop := trunkTop
			if branchY <= float64(trunkTop) {
				branchTop = int32(branchY)
			}
			base := featurePos{trunkOrigin.x, branchTop, trunkOrigin.z}
			if treeFancyLimb(region, placeLog, base, start, false) {
				branches = append(branches, branch{treeFoliageAttachment{pos: start}, branchTop})
			}
		}
		treeFancyLimb(region, placeLog, trunkOrigin, treePos(trunkOrigin, 0, trunkHeight, 0), true)
		foliageAnchors = make([]treeFoliageAttachment, 0, len(branches))
		for _, branch := range branches {
			if float64(branch.base-trunkOrigin.y) >= float64(fancyHeight)*.2 {
				base := featurePos{trunkOrigin.x, branch.base, trunkOrigin.z}
				if base != branch.anchor.pos {
					treeFancyLimb(region, placeLog, base, branch.anchor.pos, true)
				}
				foliageAnchors = append(foliageAnchors, branch.anchor)
			}
		}
	case "upwards_branching_trunk_placer":
		anchors := make([]treeFoliageAttachment, 0)
		for hp := int32(0); hp < height; hp++ {
			currentHeight := trunkOrigin.y + hp
			logPos := treePos(trunkOrigin, 0, hp, 0)
			if placeLog(logPos, "y") && hp < height-1 && r.NextFloat() < treeProbability(trunkPlacer, "place_branch_per_log_probability", 0) {
				dir := treeDirection(r.NextInt(4))
				branchLen := treeInt(trunkPlacer["extra_branch_length"], 0, r)
				branchPos := max(int32(0), branchLen-treeInt(trunkPlacer["extra_branch_length"], 0, r)-1)
				branchSteps := treeInt(trunkPlacer["extra_branch_steps"], 0, r)
				heightAlong := currentHeight + branchPos
				logX, logZ := logPos.x, logPos.z
				idx := branchPos
				for idx < height && branchSteps > 0 {
					if idx >= 1 {
						placeY := currentHeight + idx
						logX += dir[0]
						logZ += dir[1]
						heightAlong = placeY
						p := featurePos{logX, placeY, logZ}
						if placeLog(p, "y") {
							heightAlong++
						}
						anchors = append(anchors, treeFoliageAttachment{pos: p})
					}
					idx++
					branchSteps--
				}
				if heightAlong-currentHeight > 1 {
					p := featurePos{logX, heightAlong, logZ}
					anchors = append(anchors, treeFoliageAttachment{pos: p}, treeFoliageAttachment{pos: treePos(p, 0, -2, 0)})
				}
			}
			if hp == height-1 {
				anchors = append(anchors, treeFoliageAttachment{pos: treePos(trunkOrigin, 0, currentHeight-trunkOrigin.y+1, 0)})
			}
		}
		foliageAnchors = anchors
	case "bending_trunk_placer":
		foliageAnchors = make([]treeFoliageAttachment, 0, height)
		dirs := [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}
		dir := dirs[r.NextInt(4)]
		placeTreeBelowTrunk(region, config["below_trunk_provider"], trunkOrigin, trunkType, r, &trunks)
		logHeight := height - 1
		pos := trunkOrigin
		minLeaves := treeInt(trunkPlacer["min_height_for_leaves"], 1, r)
		for y := int32(0); y <= logHeight; y++ {
			if y+1 >= logHeight+r.NextInt(2) {
				pos.x += dir[0]
				pos.z += dir[1]
			}
			if treeReplaceable(region, pos) {
				placeLog(pos, "y")
			}
			if y >= minLeaves {
				foliageAnchors = append(foliageAnchors, treeFoliageAttachment{pos: pos})
			}
			pos.y++
		}
		length := treeInt(trunkPlacer["bend_length"], 0, r)
		for y := int32(0); y <= length; y++ {
			if treeReplaceable(region, pos) {
				placeLog(pos, "y")
			}
			foliageAnchors = append(foliageAnchors, treeFoliageAttachment{pos: pos})
			pos.x += dir[0]
			pos.z += dir[1]
		}
	case "forking_trunk_placer":
		placeTreeBelowTrunk(region, config["below_trunk_provider"], trunkOrigin, trunkType, r, &trunks)
		leanDir := treeDirection(r.NextInt(4))
		leanHeight := height - r.NextInt(4) - 1
		leanSteps := int32(3) - r.NextInt(3)
		tx, tz := trunkOrigin.x, trunkOrigin.z
		ey := int32(0)
		hasEy := false
		for yo := int32(0); yo < height; yo++ {
			yy := trunkOrigin.y + yo
			if yo >= leanHeight && leanSteps > 0 {
				tx += leanDir[0]
				tz += leanDir[1]
				leanSteps--
			}
			if placeLog(featurePos{tx, yy, tz}, "y") {
				ey = yy + 1
				hasEy = true
			}
		}
		if hasEy {
			foliageAnchors = append(foliageAnchors, treeFoliageAttachment{pos: featurePos{tx, ey, tz}, radiusOffset: 1})
		}
		tx, tz = trunkOrigin.x, trunkOrigin.z
		branchDir := treeDirection(r.NextInt(4))
		if branchDir != leanDir {
			branchPos := leanHeight - r.NextInt(2) - 1
			branchSteps := int32(1) + r.NextInt(3)
			hasEy = false
			yo := branchPos
			for yo < height && branchSteps > 0 {
				if yo >= 1 {
					yy := trunkOrigin.y + yo
					tx += branchDir[0]
					tz += branchDir[1]
					if placeLog(featurePos{tx, yy, tz}, "y") {
						ey = yy + 1
						hasEy = true
					}
				}
				yo++
				branchSteps--
			}
			if hasEy {
				foliageAnchors = append(foliageAnchors, treeFoliageAttachment{pos: featurePos{tx, ey, tz}})
			}
		}
	case "poplar_trunk_placer":
		placeTreeBelowTrunk(region, config["below_trunk_provider"], trunkOrigin, trunkType, r, &trunks)
		branchHeight := height - treeInt(trunkPlacer["trunk_height_above_branches"], 0, r)
		for y := int32(0); y < height; y++ {
			placeLog(treePos(trunkOrigin, 0, y, 0), "y")
			dirs := treeShuffleDirections(r)
			if branchHeight-1 == y {
				branches := treeInt(trunkPlacer["branch_amount"], 0, r)
				for i := int32(0); i < branches; i++ {
					dir := dirs[i]
					axis := "z"
					if dir[0] != 0 {
						axis = "x"
					}
					placeLog(treePos(trunkOrigin, dir[0], y, dir[1]), axis)
				}
			}
		}
		foliageAnchors = []treeFoliageAttachment{{pos: treePos(trunkOrigin, 0, branchHeight, 0)}}
	case "mega_jungle_trunk_placer":
		placeTreeBelowTrunk(region, config["below_trunk_provider"], trunkOrigin, trunkType, r, &trunks)
		placeLogFree := func(p featurePos, axis string) {
			if treeFree(region, p) {
				placeLog(p, axis)
			}
		}
		for y := int32(0); y < height; y++ {
			placeLogFree(treePos(trunkOrigin, 0, y, 0), "y")
			if y < height-1 {
				placeLogFree(treePos(trunkOrigin, 1, y, 0), "y")
				placeLogFree(treePos(trunkOrigin, 1, y, 1), "y")
				placeLogFree(treePos(trunkOrigin, 0, y, 1), "y")
			}
		}
		foliageAnchors = []treeFoliageAttachment{{pos: treePos(trunkOrigin, 0, height, 0), doubleTrunk: true}}
		for bh := height - 2 - r.NextInt(4); bh > height/2; bh -= 2 + r.NextInt(4) {
			angle := r.NextFloat() * float32(math.Pi*2)
			bx, bz := int32(0), int32(0)
			for b := int32(0); b < 5; b++ {
				bx = int32(float32(1.5) + java26.Cos(float64(angle))*float32(b))
				bz = int32(float32(1.5) + java26.Sin(float64(angle))*float32(b))
				placeLog(treePos(trunkOrigin, bx, bh-3+b/2, bz), "y")
			}
			foliageAnchors = append(foliageAnchors, treeFoliageAttachment{pos: treePos(trunkOrigin, bx, bh, bz), radiusOffset: -2})
		}
	case "cherry_trunk_placer":
		placeTreeBelowTrunk(region, config["below_trunk_provider"], trunkOrigin, trunkType, r, &trunks)
		foliageAnchors = make([]treeFoliageAttachment, 0, 3)
		firstOffset := max(int32(0), height-1+treeSampleUniform(trunkPlacer["branch_start_offset_from_top"], r))
		startProvider := treeMap(trunkPlacer["branch_start_offset_from_top"])
		secondMin := int32(featureNumber(startProvider["min_inclusive"]))
		secondMax := int32(featureNumber(startProvider["max_inclusive"])) - 1
		secondOffset := max(int32(0), height-1+secondMin+r.NextInt(secondMax-secondMin+1))
		if secondOffset >= firstOffset {
			secondOffset++
		}
		branchCount := treeInt(trunkPlacer["branch_count"], 1, r)
		middle := branchCount == 3
		bothSides := branchCount >= 2
		trunkHeight := firstOffset + 1
		if bothSides {
			trunkHeight = max(firstOffset, secondOffset) + 1
		}
		if middle {
			trunkHeight = height
		}
		for y := int32(0); y < trunkHeight; y++ {
			placeLog(treePos(trunkOrigin, 0, y, 0), "y")
		}
		if middle {
			foliageAnchors = append(foliageAnchors, treeFoliageAttachment{pos: treePos(trunkOrigin, 0, trunkHeight, 0)})
		}
		dir := treeDirection(r.NextInt(4))
		foliageAnchors = append(foliageAnchors, treeCherryBranch(trunkOrigin, height, trunkHeight, firstOffset, firstOffset < trunkHeight-1, dir, trunkPlacer, r, placeLog))
		if bothSides {
			opposite := [2]int32{-dir[0], -dir[1]}
			foliageAnchors = append(foliageAnchors, treeCherryBranch(trunkOrigin, height, trunkHeight, secondOffset, secondOffset < trunkHeight-1, opposite, trunkPlacer, r, placeLog))
		}
	default:
		panic("overworld: unsupported Java tree trunk placer " + trunkType)
	}
	leafSet := make(map[featurePos]bool)
	for _, anchor := range foliageAnchors {
		offset := treeInt(foliagePlacer["offset"], 0, r)
		leaves = treeCreateFoliage(region, foliageType, foliagePlacer, foliageProvider, trunkProvider, anchor, foliageHeight, height, radius, offset, r, leaves, leafSet)
	}
	if len(trunks) == 0 && len(leaves) == 0 {
		return false
	}
	overlayBeforeDecorators := make(map[featurePos]world.NativeState, len(region.overlay))
	for pos, state := range region.overlay {
		overlayBeforeDecorators[pos] = state
	}
	placeModernTreeDecorators(region, config["decorators"], trunks, leaves, roots, r)
	decorations := make([]featurePos, 0, len(region.overlay)-len(overlayBeforeDecorators))
	for pos, state := range region.overlay {
		if before, existed := overlayBeforeDecorators[pos]; !existed || before != state {
			decorations = append(decorations, pos)
		}
	}
	updateModernLeafDistance(region, trunks, leaves, roots, decorations)
	return true
}

// treePositionSet preserves Java HashSet<BlockPos> bucket iteration for the
// updateLeaves work queues; each source pop recreates an iterator at bucket 0.

type treePositionSet struct {
	buckets   [][]featurePos
	size      int
	threshold int
}

func treePositionHash(pos featurePos) uint32 {
	hash := uint32(pos.y) + uint32(pos.z)*31
	hash = hash*31 + uint32(pos.x)
	return hash ^ hash>>16
}

func (s *treePositionSet) add(pos featurePos) bool {
	if s.buckets == nil {
		s.buckets = make([][]featurePos, 16)
		s.threshold = 12
	}
	index := treePositionHash(pos) & uint32(len(s.buckets)-1)
	for _, existing := range s.buckets[index] {
		if existing == pos {
			return false
		}
	}
	s.buckets[index] = append(s.buckets[index], pos)
	s.size++
	if s.size > s.threshold {
		s.resize()
	}
	return true
}

func (s *treePositionSet) popFirst() (featurePos, bool) {
	for index := range s.buckets {
		if len(s.buckets[index]) == 0 {
			continue
		}
		pos := s.buckets[index][0]
		s.buckets[index] = s.buckets[index][1:]
		s.size--
		return pos, true
	}
	return featurePos{}, false
}

func (s *treePositionSet) resize() {
	previous := s.buckets
	s.buckets = make([][]featurePos, len(previous)*2)
	for _, bucket := range previous {
		for _, pos := range bucket {
			index := treePositionHash(pos) & uint32(len(s.buckets)-1)
			s.buckets[index] = append(s.buckets[index], pos)
		}
	}
	s.threshold = len(s.buckets) - len(s.buckets)/4
}

func updateModernLeafDistance(region *featureRegion, trunks, leaves, roots, decorations []featurePos) {
	if len(trunks) == 0 {
		return
	}
	positionCount := len(trunks) + len(leaves) + len(roots) + len(decorations)
	minX, maxX := trunks[0].x, trunks[0].x
	minY, maxY := trunks[0].y, trunks[0].y
	minZ, maxZ := trunks[0].z, trunks[0].z
	for _, positions := range [...][]featurePos{trunks[1:], leaves, roots, decorations} {
		for _, p := range positions {
			minX, maxX = min(minX, p.x), max(maxX, p.x)
			minY, maxY = min(minY, p.y), max(maxY, p.y)
			minZ, maxZ = min(minZ, p.z), max(maxZ, p.z)
		}
	}
	low := cube.Pos{int(minX), int(minY), int(minZ)}
	size := [3]int{int(maxX-minX) + 1, int(maxY-minY) + 1, int(maxZ-minZ) + 1}
	occupied := make(map[featurePos]struct{}, positionCount)
	filled := make([]cube.Pos, 0, positionCount)
	for _, positions := range [...][]featurePos{roots, decorations} {
		for _, p := range positions {
			if _, full := occupied[p]; !full {
				occupied[p] = struct{}{}
				filled = append(filled, cube.Pos{int(p.x), int(p.y), int(p.z)})
			}
		}
	}

	var buckets [7]treePositionSet
	logPositions := make([]featurePos, 0, len(trunks))
	seenLogs := make(map[featurePos]struct{}, len(trunks))
	for _, p := range trunks {
		if _, exists := seenLogs[p]; exists {
			continue
		}
		seenLogs[p] = struct{}{}
		logPositions = append(logPositions, p)
	}
	for _, p := range vegetationPatchHashSetOrder(logPositions) {
		buckets[0].add(p)
	}
	faceOffsets := [...]featurePos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
	distance := 0
	for {
		for distance < 7 {
			pos, ok := buckets[distance].popFirst()
			if !ok {
				break
			}
			if pos.x < minX || pos.x > maxX || pos.y < minY || pos.y > maxY || pos.z < minZ || pos.z > maxZ {
				continue
			}
			if _, full := occupied[pos]; !full {
				occupied[pos] = struct{}{}
				filled = append(filled, cube.Pos{int(pos.x), int(pos.y), int(pos.z)})
			}
			if distance != 0 {
				pos3 := cube.Pos{int(pos.x), int(pos.y), int(pos.z)}
				stateID, err := region.JavaBlockStateID(pos3)
				if err != nil {
					panic(err)
				}
				updated, err := javadata.BlockStateWithProperty(stateID, "distance", distance)
				if err != nil {
					panic(err)
				}
				region.SetJavaBlockState(pos3, updated, 19)
			}
			for _, offset := range faceOffsets {
				neighbor := treePos(pos, offset.x, offset.y, offset.z)
				if neighbor.x < minX || neighbor.x > maxX || neighbor.y < minY || neighbor.y > maxY || neighbor.z < minZ || neighbor.z > maxZ {
					continue
				}
				if _, full := occupied[neighbor]; full {
					continue
				}
				stateID, err := region.JavaBlockStateID(cube.Pos{int(neighbor.x), int(neighbor.y), int(neighbor.z)})
				if err != nil {
					panic(err)
				}
				neighborDistance, hasDistance := javadata.BlockStateInt(stateID, "distance")
				if treeTagContains("prevents_nearby_leaf_decay", treeSourceStateName(stateID)) {
					neighborDistance, hasDistance = 0, true
				}
				if !hasDistance {
					continue
				}
				next := min(int(neighborDistance), distance+1)
				if next < 7 {
					buckets[next].add(neighbor)
					distance = min(distance, next)
				}
			}
		}
		if distance >= 7 {
			break
		}
		distance++
	}
	world.UpdateJavaGenerationShapeAtEdge(region, low, size, filled, 3)
}

func treeSourceStateName(id uint32) string {
	name, ok := javadata.BlockStateName(id)
	if !ok {
		panic(fmt.Sprintf("overworld: invalid Java block state ID %d", id))
	}
	return name
}
func treeMinimumRadius(m map[string]any, height, y int32) int32 {
	t := treeName(m)
	switch t {
	case "two_layers_feature_size":
		if y < treeInt(m["limit"], 1, nil) {
			return treeInt(m["lower_size"], 0, nil)
		}
		return treeInt(m["upper_size"], 1, nil)
	case "three_layers_feature_size":
		if y < treeInt(m["limit"], 1, nil) {
			return treeInt(m["lower_size"], 1, nil)
		}
		if y >= height-treeInt(m["upper_limit"], 1, nil) {
			return treeInt(m["upper_size"], 1, nil)
		}
		return treeInt(m["middle_size"], 0, nil)
	default:
		return treeInt(m["size"], 0, nil)
	}
}
func treeTryPlaceLeaf(region *featureRegion, p featurePos, provider any, r *java26.Random, placed map[featurePos]bool, leaves []featurePos) ([]featurePos, bool) {
	_, props := region.State(p)
	if value := props["persistent"]; value == "true" || value == true {
		return leaves, false
	}
	if !treeReplaceable(region, p) {
		return leaves, false
	}
	if !treeSetFoliage(region, p, provider, r) {
		return leaves, false
	}
	placed[p] = true
	return append(leaves, p), true
}

func treeFoliageRow(region *featureRegion, typ string, props map[string]any, provider any, center featurePos, radius, y int32, double bool, random *java26.Random, placed map[featurePos]bool, leaves []featurePos) []featurePos {
	extra := int32(0)
	if double {
		extra = 1
	}
	for dx := -radius; dx <= radius+extra; dx++ {
		for dz := -radius; dz <= radius+extra; dz++ {
			if treeSkipFoliage(typ, props, dx, y, dz, radius, double, random) {
				continue
			}
			var ok bool
			leaves, ok = treeTryPlaceLeaf(region, treePos(center, dx, y, dz), provider, random, placed, leaves)
			_ = ok
		}
	}
	return leaves
}

func treeSkipFoliage(typ string, props map[string]any, dx, y, dz, radius int32, double bool, r *java26.Random) bool {
	a, b := abs32(dx), abs32(dz)
	if double {
		a = min(a, abs32(dx-1))
		b = min(b, abs32(dz-1))
	}
	switch typ {
	case "blob_foliage_placer":
		return a == radius && b == radius && (r.NextInt(2) == 0 || y == 0)
	case "bush_foliage_placer":
		return a == radius && b == radius && r.NextInt(2) == 0
	case "fancy_foliage_placer":
		return (float64(a)+.5)*(float64(a)+.5)+(float64(b)+.5)*(float64(b)+.5) > float64(radius*radius)
	case "acacia_foliage_placer":
		if y == 0 {
			return (a > 1 || b > 1) && a != 0 && b != 0
		}
		return a == radius && b == radius && radius > 0
	case "pine_foliage_placer", "spruce_foliage_placer":
		return a == radius && b == radius && radius > 0
	case "mega_pine_foliage_placer", "mega_jungle_foliage_placer":
		return a+b >= 7 || a*a+b*b > radius*radius
	case "dark_oak_foliage_placer":
		if y == 0 && double && ((dx == -radius || dx >= radius) && (dz == -radius || dz >= radius)) {
			return true
		}
		if double {
			a = min(a, abs32(dx-1))
			b = min(b, abs32(dz-1))
		}
		if y == -1 && !double {
			return a == radius && b == radius
		}
		return y == 1 && a+b > radius*2-2
	case "cherry_foliage_placer":
		wide := treeProbability(props, "wide_bottom_layer_hole_chance", .5)
		cornerChance := treeProbability(props, "corner_hole_chance", .25)
		if y == -1 && (a == radius || b == radius) && r.NextFloat() < float32(wide) {
			return true
		}
		corner := a == radius && b == radius
		if radius > 2 {
			return corner || a+b > radius*2-2 && r.NextFloat() < float32(cornerChance)
		}
		return corner && r.NextFloat() < float32(cornerChance)
	}
	return false
}

func treeCreateFoliage(region *featureRegion, typ string, placer map[string]any, foliageProvider, trunkProvider any, attachment treeFoliageAttachment, foliageHeight, treeHeight, radius, offset int32, r *java26.Random, leaves []featurePos, placed map[featurePos]bool) []featurePos {
	center := attachment.pos
	double := attachment.doubleTrunk
	h := foliageHeight + attachment.foliageHeightOffset
	row := func(rad, dy int32) {
		leaves = treeFoliageRow(region, typ, placer, foliageProvider, center, rad, dy, double, r, placed, leaves)
	}
	switch typ {
	case "blob_foliage_placer":
		for yo := offset; yo >= offset-h; yo-- {
			rad := max(radius+attachment.radiusOffset-1-yo/2, int32(0))
			row(rad, yo)
		}
	case "bush_foliage_placer":
		for yo := offset; yo >= offset-h; yo-- {
			rad := max(radius+attachment.radiusOffset-1-yo, int32(0))
			row(rad, yo)
		}
	case "fancy_foliage_placer":
		for yo := offset; yo >= offset-foliageHeight; yo-- {
			rad := radius
			if yo != offset && yo != offset-foliageHeight {
				rad++
			}
			row(rad, yo)
		}
	case "acacia_foliage_placer":
		center.y += offset
		leaves = treeFoliageRow(region, typ, placer, foliageProvider, center, radius+attachment.radiusOffset, -1-h, double, r, placed, leaves)
		leaves = treeFoliageRow(region, typ, placer, foliageProvider, center, max(radius-1, int32(0)), -h, double, r, placed, leaves)
		leaves = treeFoliageRow(region, typ, placer, foliageProvider, center, max(radius+attachment.radiusOffset-1, int32(0)), 0, double, r, placed, leaves)
	case "pine_foliage_placer":
		center.y += offset
		curr := int32(0)
		for yo := offset; yo >= offset-h; yo-- {
			leaves = treeFoliageRow(region, typ, placer, foliageProvider, center, curr, yo, double, r, placed, leaves)
			if curr >= 1 && yo == offset-h+1 {
				curr--
			} else if curr < radius+attachment.radiusOffset {
				curr++
			}
		}
	case "spruce_foliage_placer":
		center.y += offset
		curr, maxRadius, minRadius := int32(r.NextInt(2)), int32(1), int32(0)
		for yo := offset; yo >= -h; yo-- {
			leaves = treeFoliageRow(region, typ, placer, foliageProvider, center, curr, yo, double, r, placed, leaves)
			if curr >= maxRadius {
				curr = minRadius
				minRadius = 1
				maxRadius = min(maxRadius+1, radius+attachment.radiusOffset)
			} else {
				curr++
			}
		}
	case "mega_pine_foliage_placer":
		center.y += offset
		previous := int32(0)
		for yy := center.y - h; yy <= center.y; yy++ {
			yo := center.y - yy
			smooth := radius + attachment.radiusOffset + int32(math.Floor(float64(float32(yo)/float32(h)*3.5)))
			rad := smooth
			if yo > 0 && smooth == previous && yy&1 == 0 {
				rad++
			}
			leaves = treeFoliageRow(region, typ, placer, foliageProvider, featurePos{center.x, yy, center.z}, rad, 0, double, r, placed, leaves)
			previous = smooth
		}
	case "mega_jungle_foliage_placer":
		center.y += offset
		leafHeight := h
		if !double {
			leafHeight = 1 + r.NextInt(2) + attachment.foliageHeightOffset
		}
		for yo := offset; yo >= offset-leafHeight; yo-- {
			row(radius+attachment.radiusOffset+1-yo, yo)
		}
	case "dark_oak_foliage_placer":
		center.y += offset
		if double {
			leaves = treeFoliageRow(region, typ, placer, foliageProvider, center, radius+2, -1, true, r, placed, leaves)
			leaves = treeFoliageRow(region, typ, placer, foliageProvider, center, radius+3, 0, true, r, placed, leaves)
			leaves = treeFoliageRow(region, typ, placer, foliageProvider, center, radius+2, 1, true, r, placed, leaves)
			if r.NextBoolean() {
				leaves = treeFoliageRow(region, typ, placer, foliageProvider, center, radius, 2, true, r, placed, leaves)
			}
		} else {
			leaves = treeFoliageRow(region, typ, placer, foliageProvider, center, radius+2, -1, false, r, placed, leaves)
			leaves = treeFoliageRow(region, typ, placer, foliageProvider, center, radius+1, 0, false, r, placed, leaves)
		}
	case "random_spread_foliage_placer":
		attempts := treeInt(placer["leaf_placement_attempts"], 0, r)
		for range attempts {
			p := featurePos{center.x + r.NextInt(radius) - r.NextInt(radius), center.y + r.NextInt(foliageHeight) - r.NextInt(foliageHeight), center.z + r.NextInt(radius) - r.NextInt(radius)}
			var ok bool
			leaves, ok = treeTryPlaceLeaf(region, p, foliageProvider, r, placed, leaves)
			_ = ok
		}
	case "cherry_foliage_placer":
		center.y += offset
		current := radius + attachment.radiusOffset - 1
		row(current-2, h-3)
		row(current-1, h-4)
		for y := h - 5; y >= 0; y-- {
			row(current, y)
		}
		leaves = treeCherryHangingRows(region, placer, foliageProvider, center, current, -1, double, r, placed, leaves)
		leaves = treeCherryHangingRows(region, placer, foliageProvider, center, current-1, -2, double, r, placed, leaves)
	case "poplar_foliage_placer":
		center.y += offset
		current := radius + attachment.radiusOffset - 1
		flip := r.NextBoolean()
		for _, v := range [][2]int32{{current - 2, h - 1}, {current - 1, h - 2}, {current - 1, h - 3}} {
			leaves = treePoplarRow(region, placer, foliageProvider, v[0], v[1], h, center, flip, double, r, placed, leaves)
		}
		for y := h - 4; y >= 1; y-- {
			leaves = treePoplarRow(region, placer, foliageProvider, current, y, h, center, flip, double, r, placed, leaves)
		}
		leaves = treePoplarReplaceLogs(region, placer, foliageProvider, trunkProvider, current, h-4, h, center, flip, double, r, placed, leaves)
		leaves = treePoplarRow(region, placer, foliageProvider, current-1, 0, h, center, flip, double, r, placed, leaves)
		leaves = treePoplarRow(region, placer, foliageProvider, max(int32(1), min(current-2, int32(2))), -1, h, center, flip, double, r, placed, leaves)
	default:
		panic("overworld: unsupported Java tree foliage placer " + typ)
	}
	return leaves
}

func treeCherryHangingRows(region *featureRegion, placer map[string]any, provider any, center featurePos, radius, y int32, double bool, r *java26.Random, placed map[featurePos]bool, leaves []featurePos) []featurePos {
	leaves = treeFoliageRow(region, "cherry_foliage_placer", placer, provider, center, radius, y, double, r, placed, leaves)
	extra := int32(0)
	if double {
		extra = 1
	}
	logPos := treePos(center, 0, -1, 0)
	directions := [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}
	for _, edge := range directions {
		toX, toZ := -edge[1], edge[0]
		off := radius
		if toX > 0 || toZ > 0 {
			off += extra
		}
		pos := treePos(center, toX*off-edge[0]*radius, y-1, toZ*off-edge[1]*radius)
		for along := -radius; along < radius+extra; along++ {
			above := treePos(pos, 0, 1, 0)
			if placed[above] {
				placeExtension := func(p featurePos, chance float32) bool {
					if abs32(p.x-logPos.x)+abs32(p.y-logPos.y)+abs32(p.z-logPos.z) >= 7 || r.NextFloat() > chance {
						return false
					}
					var ok bool
					leaves, ok = treeTryPlaceLeaf(region, p, provider, r, placed, leaves)
					return ok
				}
				first := treePos(pos, 0, 0, 0)
				if placeExtension(first, float32(treeProbability(placer, "hanging_leaves_chance", .16666667))) {
					placeExtension(treePos(first, 0, -1, 0), float32(treeProbability(placer, "hanging_leaves_extension_chance", .33333334)))
				}
			}
			pos = treePos(pos, edge[0], 0, edge[1])
		}
	}
	return leaves
}

func treePoplarRow(region *featureRegion, placer map[string]any, provider any, radius, y, height int32, center featurePos, flip, double bool, r *java26.Random, placed map[featurePos]bool, leaves []featurePos) []featurePos {
	extra := int32(0)
	if double {
		extra = 1
	}
	partial := y == height-1 || y == height-2
	for dx := -radius; dx <= radius+extra; dx++ {
		for dz := -radius; dz <= radius+extra; dz++ {
			small := (flip && ((dx > 0 && dz > 0) || (dx < 0 && dz < 0))) || (!flip && ((dx > 0 && dz < 0) || (dx < 0 && dz > 0)))
			cut := radius
			if small {
				cut--
			} else if partial {
				cut++
			}
			a, b := abs32(dx), abs32(dz)
			if partial && (a == radius || b == radius) {
				continue
			}
			remove := int32(0)
			if r.NextFloat() <= float32(treeProbability(placer, "side_hole_chance", .25)) {
				remove = 1
			}
			if a+b > radius*2-(cut+remove) {
				continue
			}
			var ok bool
			leaves, ok = treeTryPlaceLeaf(region, treePos(center, dx, y, dz), provider, r, placed, leaves)
			_ = ok
		}
	}
	return leaves
}

func treePoplarReplaceLogs(region *featureRegion, placer map[string]any, foliageProvider, trunkProvider any, radius, y, height int32, center featurePos, flip, double bool, r *java26.Random, placed map[featurePos]bool, leaves []featurePos) []featurePos {
	extra := int32(0)
	if double {
		extra = 1
	}
	partial := y == height-1 || y == height-2
	for dx := -radius; dx <= radius+extra; dx++ {
		for dz := -radius; dz <= radius+extra; dz++ {
			small := (flip && ((dx > 0 && dz > 0) || (dx < 0 && dz < 0))) || (!flip && ((dx > 0 && dz < 0) || (dx < 0 && dz > 0)))
			cut := radius
			if small {
				cut--
			} else if partial {
				cut++
			}
			a, b := abs32(dx), abs32(dz)
			if a+b > radius*2-(cut+2) || !((b == 0 && radius-a >= 4) || (a == 0 && radius-b >= 4)) {
				continue
			}
			expected, _, ok := region.SampleNativeState(foliageProvider, treePos(center, dx, y, dz), r)
			if !ok {
				continue
			}
			p := treePos(center, dx, y, dz)
			actual := region.nativeState(p)
			if actual != expected {
				continue
			}
			axis := "z"
			if b == 0 {
				axis = "x"
			}
			if treeAxis(region, p, trunkProvider, axis, r) {
				delete(placed, p)
				leaves = append(leaves, p)
			}
		}
	}
	return leaves
}

func placeModernFallenTree(region *featureRegion, config map[string]any, origin featurePos, r *java26.Random) bool {
	provider := config["trunk_provider"]
	// The stump is independent of whether the lateral log can be fitted.
	treeSet(region, origin, provider, r)
	placeModernTreeDecorators(region, config["stump_decorators"], []featurePos{origin}, nil, nil, r)
	d := r.NextInt(4)
	dx, dz := int32(0), int32(0)
	switch d {
	case 0:
		dz = -1
	case 1:
		dx = 1
	case 2:
		dz = 1
	case 3:
		dx = -1
	}
	length := treeInt(config["log_length"], 0, r) - 2
	start := origin
	dist := int32(2 + r.NextInt(2))
	start = treePos(origin, dx*dist, 0, dz*dist)
	start.y++
	for range 6 {
		if treeReplaceable(region, start) && treeSturdyUp(region, treePos(start, 0, -1, 0)) {
			break
		}
		start.y--
	}
	valid := length > 0
	if length > 0 {
		end := start
		gap := 0
		for i := int32(0); i < length; i++ {
			if !treeReplaceable(region, end) {
				valid = false
				break
			}
			if !treeSturdyUp(region, treePos(end, 0, -1, 0)) {
				gap++
				if gap > 2 {
					valid = false
					break
				}
			} else {
				gap = 0
			}
			end = treePos(end, dx, 0, dz)
		}
	}
	logs := make([]featurePos, 0, max(length, int32(0)))
	if valid && length > 0 {
		axis := "z"
		if dx != 0 {
			axis = "x"
		}
		pos := start
		for range length {
			if treeAxis(region, pos, provider, axis, r) {
				region.markAboveForPostProcessing(pos)
			}
			logs = append(logs, pos)
			pos = treePos(pos, dx, 0, dz)
		}
	}
	placeModernTreeDecorators(region, config["log_decorators"], logs, nil, nil, r)
	return true
}

func treeSolid(region *featureRegion, p featurePos) bool {
	n, _ := region.State(p)
	name := normalizeJavaResource(n)
	if name == "lava" || name == "water" {
		return false
	}
	return treeSturdyUp(region, p)
}
func treeSturdyUp(region *featureRegion, p featurePos) bool {
	block := region.Block(p)
	pos := cube.Pos{int(p.x), int(p.y), int(p.z)}
	return block.Model().FaceSolid(pos, cube.FaceUp, featureRegionBlockSource{region})
}
func treeSturdyDown(region *featureRegion, p featurePos) bool {
	block := region.Block(p)
	pos := cube.Pos{int(p.x), int(p.y), int(p.z)}
	return block.Model().FaceSolid(pos, cube.FaceDown, featureRegionBlockSource{region})
}
func treeSolidRender(region *featureRegion, p featurePos) bool {
	state, err := region.JavaBlockStateID(cube.Pos{int(p.x), int(p.y), int(p.z)})
	if err != nil {
		panic(err)
	}
	shape, err := javadata.BlockGenerationShapeID(state)
	if err != nil {
		panic(err)
	}
	return shape.SolidRender
}
func placeModernBamboo(region *featureRegion, config map[string]any, origin featurePos, r *java26.Random) bool {
	n, _ := region.State(origin)
	if normalizeJavaResource(n) != "air" {
		return false
	}
	soil, _ := region.State(treePos(origin, 0, -1, 0))
	if !treeTagContains("#minecraft:bamboo_plantable_on", soil) {
		return false
	}
	height := int32(r.NextInt(12) + 5)
	probability := 0.0
	if v, ok := config["probability"].(float64); ok {
		probability = v
	}
	if r.NextFloat() < float32(probability) {
		radius := int32(r.NextInt(4) + 1)
		for x := -radius; x <= radius; x++ {
			for z := -radius; z <= radius; z++ {
				if x*x+z*z > radius*radius {
					continue
				}
				p := treePos(origin, x, 0, z)
				ground := region.Height(p.x, p.z, false) - 1
				p.y = ground
				s, _ := region.State(p)
				if treeTagContains("#minecraft:beneath_bamboo_podzol_replaceable", s) {
					treeSetNamed(region, p, "podzol", nil)
				}
			}
		}
	}
	y := int32(0)
	for ; y < height && origin.y+y <= region.MaxY(); y++ {
		p := treePos(origin, 0, y, 0)
		n, _ := region.State(p)
		if normalizeJavaResource(n) != "air" {
			break
		}
		treeSetNamed(region, p, "bamboo", map[string]any{"age": "1", "leaves": "none", "stage": "0"})
	}
	if y >= 3 {
		treeSetNamed(region, treePos(origin, 0, y, 0), "bamboo", map[string]any{"age": "1", "leaves": "large", "stage": "1"})
		treeSetNamed(region, treePos(origin, 0, y-1, 0), "bamboo", map[string]any{"age": "1", "leaves": "large", "stage": "0"})
		treeSetNamed(region, treePos(origin, 0, y-2, 0), "bamboo", map[string]any{"age": "1", "leaves": "small", "stage": "0"})
	}
	return true
}

func placeModernHugeMushroom(region *featureRegion, config map[string]any, origin featurePos, r *java26.Random) bool {
	height := int32(r.NextInt(3) + 4)
	if r.NextInt(12) == 0 {
		height *= 2
	}
	radius := treeInt(config["foliage_radius"], 2, r)
	if origin.y < region.MinY()+1 || origin.y+height+1 > region.MaxY()+1 {
		return false
	}
	if !featurePredicate(region, treePos(origin, 0, -1, 0), treeMap(config["can_place_on"])) {
		return false
	}
	red := strings.Contains(fmt.Sprint(config["cap_provider"]), "red_mushroom")
	for y := int32(0); y <= height; y++ {
		rad := int32(0)
		if red && y >= height-3 || !red && y > 3 {
			rad = radius
		}
		for x := -rad; x <= rad; x++ {
			for z := -rad; z <= rad; z++ {
				if !treeAirOrLeaves(region, treePos(origin, x, y, z)) {
					return false
				}
			}
		}
	}
	if red {
		for y := height - 3; y <= height; y++ {
			rad := radius
			if y == height {
				rad = radius - 1
			}
			center := radius - 2
			for x := -rad; x <= rad; x++ {
				for z := -rad; z <= rad; z++ {
					edgeX := x == -rad || x == rad
					edgeZ := z == -rad || z == rad
					if y < height && edgeX == edgeZ {
						continue
					}
					props := map[string]any{"up": fmt.Sprint(y >= height-1), "west": fmt.Sprint(x < -center), "east": fmt.Sprint(x > center), "north": fmt.Sprint(z < -center), "south": fmt.Sprint(z > center)}
					treeSetAt(region, treePos(origin, x, y, z), origin, mushroomCapWithProperties(config["cap_provider"], props), r)
				}
			}
		}
	} else {
		for x := -radius; x <= radius; x++ {
			for z := -radius; z <= radius; z++ {
				if (x == -radius || x == radius) && (z == -radius || z == radius) {
					continue
				}
				props := map[string]any{"west": fmt.Sprint(x == -radius || (z == -radius || z == radius) && x == 1-radius), "east": fmt.Sprint(x == radius || (z == -radius || z == radius) && x == radius-1), "north": fmt.Sprint(z == -radius || (x == -radius || x == radius) && z == 1-radius), "south": fmt.Sprint(z == radius || (x == -radius || x == radius) && z == radius-1)}
				treeSetAt(region, treePos(origin, x, height, z), origin, mushroomCapWithProperties(config["cap_provider"], props), r)
			}
		}
	}
	for y := int32(0); y < height; y++ {
		treeSetAt(region, treePos(origin, 0, y, 0), origin, config["stem_provider"], r)
	}
	return true
}
func mushroomCapWithProperties(provider any, props map[string]any) any {
	p, ok := provider.(map[string]any)
	if !ok {
		return provider
	}
	copy := make(map[string]any, len(p))
	for k, v := range p {
		copy[k] = v
	}
	existing, _ := p["properties"].(map[string]any)
	merged := make(map[string]any, len(existing)+len(props))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range props {
		merged[k] = v
	}
	copy["properties"] = merged
	return copy
}

func placeModernHugeFungus(region *featureRegion, config map[string]any, origin featurePos, r *java26.Random) bool {
	base, _ := region.State(treePos(origin, 0, -1, 0))
	if normalizeJavaResource(base) != normalizeJavaResource(fmt.Sprint(config["valid_base_block"])) {
		return false
	}
	total := int32(4 + r.NextInt(10))
	if r.NextInt(12) == 0 {
		total *= 2
	}
	planted, _ := config["planted"].(bool)
	if !planted && origin.y+total >= region.MaxY() {
		return false
	}
	huge := !planted && r.NextFloat() < .06
	hatName := normalizeJavaResource(fmt.Sprint(config["hat_state"]))
	if state, ok := config["hat_state"].(map[string]any); ok {
		hatName = normalizeJavaResource(fmt.Sprint(state["id"]))
	}
	treeSetNamed(region, origin, "air", nil)
	stemRadius := int32(0)
	if huge {
		stemRadius = 1
	}
	for dx := -stemRadius; dx <= stemRadius; dx++ {
		for dz := -stemRadius; dz <= stemRadius; dz++ {
			corner := huge && abs32(dx) == stemRadius && abs32(dz) == stemRadius
			for dy := int32(0); dy < total; dy++ {
				p := treePos(origin, dx, dy, dz)
				if !treeIsFungusReplaceable(config, p, region, true) {
					continue
				}
				if planted {
					below, _ := region.State(treePos(p, 0, -1, 0))
					if !treeIsAirName(below) {
						treeSetNamed(region, p, "air", nil)
					}
					treeSetLiteral(region, p, config["stem_state"])
				} else if corner {
					if r.NextFloat() < .1 {
						treeSetLiteral(region, p, config["stem_state"])
					}
				} else {
					treeSetLiteral(region, p, config["stem_state"])
				}
			}
		}
	}
	hatHeight := min(int32(r.NextInt(1+total/3)+5), total)
	hatStart := total - hatHeight
	for dy := hatStart; dy <= total; dy++ {
		radius := int32(2)
		if dy >= total-r.NextInt(3) {
			radius = 1
		}
		if hatHeight > 8 && dy < hatStart+4 {
			radius = 3
		}
		if huge {
			radius++
		}
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				edgeX := dx == -radius || dx == radius
				edgeZ := dz == -radius || dz == radius
				inside := !edgeX && !edgeZ && dy != total
				corner := edgeX && edgeZ
				hatBottom := dy < hatStart+3
				p := treePos(origin, dx, dy, dz)
				if !treeIsFungusReplaceable(config, p, region, false) {
					continue
				}
				if planted {
					below, _ := region.State(treePos(p, 0, -1, 0))
					if !treeIsAirName(below) {
						treeSetNamed(region, p, "air", nil)
					}
				}
				if hatBottom {
					if !inside {
						placeFungusHatDrop(region, p, hatName, config["hat_state"], r)
					}
					continue
				}
				decorChance, hatChance, vineChance := float32(.0005), float32(.98), float32(0)
				if inside {
					decorChance, hatChance = .1, .2
				} else if corner {
					decorChance, hatChance = .01, .7
				}
				if hatName == "nether_wart_block" {
					vineChance = .07
					if inside {
						vineChance = .1
					} else if corner {
						vineChance = .083
					}
				}
				if r.NextFloat() < decorChance {
					treeSetLiteral(region, p, config["decor_state"])
				} else if r.NextFloat() < hatChance {
					treeSetLiteral(region, p, config["hat_state"])
					if r.NextFloat() < vineChance {
						placeWeepingVines(region, p, r)
					}
				}
			}
		}
	}
	return true
}
func treeIsAirName(name string) bool {
	switch normalizeJavaResource(name) {
	case "air", "cave_air", "void_air":
		return true
	}
	return false
}
func placeFungusHatDrop(region *featureRegion, p featurePos, hatName string, hatProvider any, r *java26.Random) {
	below, _ := region.State(treePos(p, 0, -1, 0))
	if normalizeJavaResource(below) == hatName {
		treeSetLiteral(region, p, hatProvider)
	} else if r.NextFloat() < .15 {
		treeSetLiteral(region, p, hatProvider)
		if hatName == "nether_wart_block" && r.NextInt(11) == 0 {
			placeWeepingVines(region, p, r)
		}
	}
}
func treeIsFungusReplaceable(config map[string]any, p featurePos, region *featureRegion, checkPlants bool) bool {
	shape, err := javadata.BlockGenerationShapeID(region.nativeState(p).CanonicalJavaID(region.c.BlockRegistry()))
	if err != nil {
		panic(err)
	}
	return shape.Replaceable || checkPlants && featurePredicate(region, p, treeMap(config["replaceable_blocks"]))
}
func placeWeepingVines(region *featureRegion, origin featurePos, r *java26.Random) {
	p := treePos(origin, 0, -1, 0)
	if !treeIsAir(region, p) {
		return
	}
	height := int32(r.NextInt(5) + 1)
	if r.NextInt(7) == 0 {
		height *= 2
	}
	for y := int32(0); y <= height; y++ {
		at := treePos(p, 0, -y, 0)
		if treeIsAir(region, at) {
			below := treePos(at, 0, -1, 0)
			if y == height || !treeIsAir(region, below) {
				treeSetNamed(region, at, "weeping_vines", map[string]any{"age": fmt.Sprint(23 + r.NextInt(3))})
				break
			}
			treeSetNamed(region, at, "weeping_vines_plant", nil)
		}
	}
}
func rootSystemLevelTest(region *featureRegion, p featurePos, distance, deviation int32) bool {
	if distance <= 0 {
		return true
	}
	dirs := [][2]int32{{0, 1}, {-1, 0}, {0, -1}, {1, 0}}
	for _, dir := range dirs {
		corner := treePos(p, dir[0]*distance, 0, dir[1]*distance)
		below := treePos(corner, 0, -deviation, 0)
		above := treePos(corner, 0, deviation, 0)
		bn, _ := region.State(below)
		an, _ := region.State(above)
		if normalizeJavaResource(bn) == "air" || normalizeJavaResource(an) != "air" {
			return false
		}
	}
	return true
}

func placeModernRootSystem(region *featureRegion, config map[string]any, origin featurePos, r *java26.Random) bool {
	n, _ := region.State(origin)
	if normalizeJavaResource(n) != "air" {
		return false
	}
	column := origin
	target := origin
	found := false
	max := treeInt(config["root_column_max_height"], 100, r)
	for i := int32(0); i < max; i++ {
		column.y++
		if column.y > region.MaxY() {
			break
		}
		if region.Height(column.x, column.z, false) < column.y {
			return true
		}
		if !rootSystemLevelTest(region, column, treeInt(config["level_test_distance"], 0, r), treeInt(config["max_level_deviation"], 0, r)) || !rootSystemAllowedTreePosition(region, column, treeMap(config["allowed_tree_position"])) || !rootSystemSpace(region, column, treeInt(config["required_vertical_space_for_tree"], 3, r), treeInt(config["allowed_vertical_water_for_tree"], 2, r)) {
			continue
		}
		below := treePos(column, 0, -1, 0)
		if !treeSolid(region, below) {
			return true
		}
		target = column
		found = true
		break
	}
	if !found {
		return true
	}
	if !region.PlacePlacedFeature(config["feature"], [3]int32{target.x, target.y, target.z}, r) {
		return true
	}
	for y := origin.y; y < target.y; y++ {
		for i := int32(0); i < treeInt(config["root_placement_attempts"], 20, r); i++ {
			x := origin.x + r.NextInt(treeInt(config["root_radius"], 3, r)) - r.NextInt(treeInt(config["root_radius"], 3, r))
			z := origin.z + r.NextInt(treeInt(config["root_radius"], 3, r)) - r.NextInt(treeInt(config["root_radius"], 3, r))
			p := featurePos{x, y, z}
			if rootSystemReplaceable(region, p, config["root_replaceable"]) {
				treeSet(region, p, config["root_state_provider"], r)
			}
		}
	}
	for i := int32(0); i < treeInt(config["hanging_root_placement_attempts"], 20, r); i++ {
		rad := treeInt(config["hanging_root_radius"], 3, r)
		span := treeInt(config["hanging_roots_vertical_span"], 2, r)
		p := featurePos{origin.x + r.NextInt(rad) - r.NextInt(rad), origin.y + r.NextInt(span) - r.NextInt(span), origin.z + r.NextInt(rad) - r.NextInt(rad)}
		if treeIsAir(region, p) && treeSturdyDown(region, treePos(p, 0, 1, 0)) {
			treeSet(region, p, config["hanging_root_state_provider"], r)
		}
	}
	return true
}
func rootSystemAllowedTreePosition(region *featureRegion, p featurePos, predicate map[string]any) bool {
	return featurePredicate(region, p, predicate)
}
func rootSystemSpace(region *featureRegion, p featurePos, height, water int32) bool {
	for i := int32(1); i <= height; i++ {
		n, _ := region.State(treePos(p, 0, i, 0))
		n = normalizeJavaResource(n)
		if n != "air" && !(i+1 <= water && n == "water") {
			return false
		}
	}
	return true
}
func rootSystemReplaceable(region *featureRegion, p featurePos, raw any) bool {
	name, _ := region.State(p)
	matches := func(value any) bool {
		block, ok := value.(string)
		if !ok {
			panic("overworld: malformed Java root replaceable block")
		}
		block = normalizeJavaResource(block)
		if strings.HasPrefix(block, "#") {
			return treeTagContains(block, name)
		}
		return normalizeJavaResource(name) == block
	}
	switch values := raw.(type) {
	case string:
		return matches(values)
	case []any:
		for _, value := range values {
			if matches(value) {
				return true
			}
		}
		return false
	default:
		panic("overworld: malformed Java root replaceable set")
	}
}

func asAnySlice(v any) []any { a, _ := v.([]any); return a }

func placeModernTreeDecorators(region *featureRegion, raw any, trunks, leaves, roots []featurePos, r *java26.Random) {
	for _, value := range asAnySlice(raw) {
		d := treeMap(value)
		switch treeName(d) {
		case "trunk_vine":
			for _, p := range trunks {
				for _, v := range []struct {
					dx, dz   int32
					name     string
					attached string
				}{{-1, 0, "west", "east"}, {1, 0, "east", "west"}, {0, -1, "north", "south"}, {0, 1, "south", "north"}} {
					if r.NextInt(3) > 0 {
						q := treePos(p, v.dx, 0, v.dz)
						if treeIsAir(region, q) {
							treeSetNamed(region, q, "vine", map[string]any{v.attached: "true"})
						}
					}
				}
			}
		case "leave_vine":
			p := float32(0)
			if v, ok := d["probability"].(float64); ok {
				p = float32(v)
			}
			for _, leaf := range leaves {
				for _, v := range []struct {
					dx, dz   int32
					attached string
				}{{-1, 0, "east"}, {1, 0, "west"}, {0, -1, "south"}, {0, 1, "north"}} {
					if r.NextFloat() < p {
						q := treePos(leaf, v.dx, 0, v.dz)
						if treeIsAir(region, q) {
							for i := int32(0); i <= 4; i++ {
								at := treePos(q, 0, -i, 0)
								if !treeIsAir(region, at) {
									break
								}
								treeSetNamed(region, at, "vine", map[string]any{v.attached: "true"})
							}
						}
					}
				}
			}
		case "alter_ground":
			placeTreeAlterGround(region, d, trunks, roots, r)
		case "beehive":
			placeTreeBeeNest(region, d, trunks, leaves, r)
		case "cocoa":
			placeTreeCocoa(region, d, trunks, r)
		case "place_on_ground":
			placeTreeGroundDecorator(region, d, trunks, roots, r)
		case "attached_to_logs":
			placeTreeLogAttachments(region, d, trunks, r)
		case "attached_to_leaves":
			placeTreeLeafAttachments(region, d, leaves, r)
		case "pale_moss":
			placePaleMoss(region, d, trunks, leaves, r)
		case "shelf_mushroom":
			placeShelfMushrooms(region, d, trunks, r)
		case "creaking_heart":
			placeCreakingHeart(region, d, trunks, r)
		default:
			panic("overworld: unsupported Java tree decorator " + treeName(d))
		}
	}
}
func treeProbability(d map[string]any, key string, def float32) float32 {
	if v, ok := d[key].(float64); ok {
		return float32(v)
	}
	return def
}
func treeShufflePositions(in []featurePos, r *java26.Random) []featurePos {
	out := append([]featurePos(nil), in...)
	for i := len(out) - 1; i > 0; i-- {
		j := int(r.NextInt(int32(i + 1)))
		out[i], out[j] = out[j], out[i]
	}
	return out
}
func treeDirections(raw any) []string {
	out := make([]string, 0)
	for _, v := range asAnySlice(raw) {
		out = append(out, normalizeJavaResource(fmt.Sprint(v)))
	}
	return out
}
func treeNeighbor(p featurePos, d string) featurePos {
	switch d {
	case "down":
		return treePos(p, 0, -1, 0)
	case "up":
		return treePos(p, 0, 1, 0)
	case "north":
		return treePos(p, 0, 0, -1)
	case "south":
		return treePos(p, 0, 0, 1)
	case "west":
		return treePos(p, -1, 0, 0)
	default:
		return treePos(p, 1, 0, 0)
	}
}
func placeTreeBeeNest(region *featureRegion, d map[string]any, trunks, leaves []featurePos, r *java26.Random) {
	if len(trunks) == 0 || r.NextFloat() >= treeProbability(d, "probability", 0) {
		return
	}
	hiveY := trunks[0].y + 1
	if len(leaves) > 0 {
		hiveY = max(leaves[0].y-1, trunks[0].y+1)
	} else {
		hiveY = min(trunks[0].y+1+r.NextInt(3), trunks[len(trunks)-1].y)
	}
	dirs := [3]string{"east", "south", "west"}
	placements := make([]featurePos, 0)
	for _, p := range trunks {
		if p.y == hiveY {
			for _, dir := range dirs {
				placements = append(placements, treeNeighbor(p, dir))
			}
		}
	}
	placements = treeShufflePositions(placements, r)
	for _, p := range placements {
		if !treeIsAir(region, p) || !treeIsAir(region, treeNeighbor(p, "south")) {
			continue
		}
		if !treeSetNamed(region, p, "bee_nest", map[string]any{"facing": "south", "honey_level": "0"}) {
			return
		}
		count := 2 + r.NextInt(2)
		bees := make([]any, 0, count)
		for range count {
			bees = append(bees, map[string]any{"entity_data": map[string]any{"id": "minecraft:bee"}, "ticks_in_hive": r.NextInt(599), "min_ticks_in_hive": 600})
		}
		if featureInsideTarget(region.target, p) {
			name, props := region.State(p)
			if !strings.Contains(name, ":") {
				name = "minecraft:" + name
			}
			placement := javastructure.Placement{Blocks: []javastructure.Block{{Pos: [3]int32{p.x, p.y, p.z}, State: world.BlockState{Name: name, Properties: props}, NBT: map[string]any{"id": "minecraft:beehive", "bees": bees}}}}
			region.result.BlockEntities = append(region.result.BlockEntities, placement.BlockEntityPayloads()...)
		}
		return
	}
}
func placeTreeCocoa(region *featureRegion, d map[string]any, trunks []featurePos, r *java26.Random) {
	if len(trunks) == 0 || r.NextFloat() >= treeProbability(d, "probability", 0) {
		return
	}
	baseY := trunks[0].y
	directions := [4]struct{ direction, opposite string }{{"north", "south"}, {"east", "west"}, {"south", "north"}, {"west", "east"}}
	for _, log := range trunks {
		if log.y-baseY > 2 {
			continue
		}
		for _, side := range directions {
			if r.NextFloat() <= .25 {
				p := treeNeighbor(log, side.opposite)
				if treeIsAir(region, p) {
					treeSetNamed(region, p, "cocoa", map[string]any{"age": fmt.Sprint(r.NextInt(3)), "facing": side.direction})
				}
			}
		}
	}
}
func treeLowestTrunkRoots(trunks, roots []featurePos) []featurePos {
	if len(roots) == 0 {
		return trunks
	}
	if len(trunks) > 0 && roots[0].y == trunks[0].y {
		return append(append([]featurePos(nil), trunks...), roots...)
	}
	return roots
}
func placeTreeGroundDecorator(region *featureRegion, d map[string]any, trunks, roots []featurePos, r *java26.Random) {
	positions := treeLowestTrunkRoots(trunks, roots)
	if len(positions) == 0 {
		return
	}
	origin := positions[0]
	minY, minX, maxX, minZ, maxZ := origin.y, origin.x, origin.x, origin.z, origin.z
	for _, p := range positions {
		if p.y == minY {
			minX = min(minX, p.x)
			maxX = max(maxX, p.x)
			minZ = min(minZ, p.z)
			maxZ = max(maxZ, p.z)
		}
	}
	radius, height, tries := treeInt(d["radius"], 2, r), treeInt(d["height"], 1, r), treeInt(d["tries"], 128, r)
	provider := d["block_state_provider"]
	for range tries {
		p := featurePos{minX - radius + r.NextInt(maxX-minX+2*radius+1), minY - height + r.NextInt(height*2+1), minZ - radius + r.NextInt(maxZ-minZ+2*radius+1)}
		above := treePos(p, 0, 1, 0)
		n, _ := region.State(above)
		if normalizeJavaResource(n) != "air" && normalizeJavaResource(n) != "vine" {
			continue
		}
		if !treeSolidRender(region, p) || region.placementHeight(p.x, p.z, "MOTION_BLOCKING_NO_LEAVES") > above.y {
			continue
		}
		treeSet(region, above, provider, r)
	}
}
func placeTreeAlterGround(region *featureRegion, d map[string]any, trunks, roots []featurePos, r *java26.Random) {
	positions := treeLowestTrunkRoots(trunks, roots)
	if len(positions) == 0 {
		return
	}
	minY := positions[0].y
	for _, p := range positions {
		if p.y != minY {
			continue
		}
		for _, offset := range [4][2]int32{{-1, -1}, {2, -1}, {-1, 2}, {2, 2}} {
			treeAlterGroundCircle(region, d["provider"], treePos(p, offset[0], 0, offset[1]), r)
		}
		for range 5 {
			selection := r.NextInt(64)
			x, z := selection%8, selection/8
			if x == 0 || x == 7 || z == 0 || z == 7 {
				treeAlterGroundCircle(region, d["provider"], treePos(p, -3+x, 0, -3+z), r)
			}
		}
	}
}
func treeAlterGroundCircle(region *featureRegion, provider any, origin featurePos, r *java26.Random) {
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			if abs32(x) == 2 && abs32(z) == 2 {
				continue
			}
			for dy := int32(2); dy >= -3; dy-- {
				p := treePos(origin, x, dy, z)
				state, actor, ok := region.SampleNativeState(provider, p, r)
				if ok {
					region.SetNativeBlock(p, state, actor)
					break
				}
				if !treeIsAir(region, p) && dy < 0 {
					break
				}
			}
		}
	}
}
func placeTreeLogAttachments(region *featureRegion, d map[string]any, trunks []featurePos, r *java26.Random) {
	dirs := treeDirections(d["directions"])
	for _, p := range treeShufflePositions(trunks, r) {
		dir := dirs[r.NextInt(int32(len(dirs)))]
		at := treeNeighbor(p, dir)
		if r.NextFloat() <= treeProbability(d, "probability", 0) && treeIsAir(region, at) {
			treeSet(region, at, d["block_provider"], r)
		}
	}
}
func placeTreeLeafAttachments(region *featureRegion, d map[string]any, leaves []featurePos, r *java26.Random) {
	dirs := treeDirections(d["directions"])
	blacklist := make(map[featurePos]bool)
	for _, p := range treeShufflePositions(leaves, r) {
		dir := dirs[r.NextInt(int32(len(dirs)))]
		at := treeNeighbor(p, dir)
		if blacklist[at] || r.NextFloat() >= treeProbability(d, "probability", 0) {
			continue
		}
		empty := treeInt(d["required_empty_blocks"], 1, r)
		valid := true
		check := p
		for i := int32(0); i < empty; i++ {
			check = treeNeighbor(check, dir)
			if !treeIsAir(region, check) {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		ex := treeInt(d["exclusion_radius_xz"], 0, r)
		ey := treeInt(d["exclusion_radius_y"], 0, r)
		for x := -ex; x <= ex; x++ {
			for y := -ey; y <= ey; y++ {
				for z := -ex; z <= ex; z++ {
					blacklist[treePos(at, x, y, z)] = true
				}
			}
		}
		treeSet(region, at, d["block_provider"], r)
	}
}
func placePaleMoss(region *featureRegion, d map[string]any, trunks, leaves []featurePos, r *java26.Random) {
	logs := treeShufflePositions(trunks, r)
	if len(logs) == 0 {
		return
	}
	origin := logs[0]
	for _, p := range logs {
		if p.y < origin.y {
			origin = p
		}
	}
	if r.NextFloat() < treeProbability(d, "ground_probability", 0) {
		region.PlaceValue("pale_moss_patch", treePos(origin, 0, 1, 0), r)
	}
	for _, p := range trunks {
		if r.NextFloat() < treeProbability(d, "trunk_probability", 0) {
			down := treePos(p, 0, -1, 0)
			if treeIsAir(region, down) {
				treeHangMoss(region, down, r)
			}
		}
	}
	for _, p := range leaves {
		if r.NextFloat() < treeProbability(d, "leaves_probability", 0) {
			down := treePos(p, 0, -1, 0)
			if treeIsAir(region, down) {
				treeHangMoss(region, down, r)
			}
		}
	}
}
func treeHangMoss(region *featureRegion, p featurePos, r *java26.Random) {
	for treeIsAir(region, treePos(p, 0, -1, 0)) && !(r.NextFloat() < .5) {
		treeSetNamed(region, p, "pale_hanging_moss", map[string]any{"tip": "false"})
		p.y--
	}
	treeSetNamed(region, p, "pale_hanging_moss", map[string]any{"tip": "true"})
}
func treeShelfMushroom(region *featureRegion, p featurePos, facing string, standing bool, r *java26.Random) {
	if !treeReplaceable(region, p) || treeIsWaterNearby(region, p) {
		return
	}
	if standing {
		below := treePos(p, 0, -1, 0)
		belowName, _ := region.State(below)
		if normalizeJavaResource(belowName) == "shelf_mushroom" {
			return
		}
	}
	treeSetNamed(region, p, "shelf_mushroom", map[string]any{"facing": facing, "age": fmt.Sprint(r.NextInt(2))})
}
func treeIsWaterNearby(region *featureRegion, p featurePos) bool {
	for _, dir := range []string{"east", "west", "north", "south"} {
		name, _ := region.State(treeNeighbor(p, dir))
		if normalizeJavaResource(name) == "water" {
			return true
		}
	}
	name, _ := region.State(p)
	return normalizeJavaResource(name) == "water"
}
func placeShelfMushrooms(region *featureRegion, d map[string]any, trunks []featurePos, r *java26.Random) {
	if r.NextFloat() >= treeProbability(d, "probability", 0) || len(trunks) == 0 {
		return
	}
	if trunks[0].y == trunks[len(trunks)-1].y {
		first, last := trunks[0], trunks[len(trunks)-1]
		dirs := []string{"east", "west"}
		if first.x != last.x {
			dirs = []string{"north", "south"}
		}
		for _, log := range trunks {
			for _, facing := range dirs {
				if r.NextFloat() <= .25 {
					p := treeNeighbor(log, facing)
					if treeReplaceable(region, p) && !treeIsWaterNearby(region, p) {
						adjacent := false
						for _, direction := range []string{"north", "east", "south", "west"} {
							if name, _ := region.State(treeNeighbor(p, direction)); normalizeJavaResource(name) == "shelf_mushroom" {
								adjacent = true
							}
							if name, _ := region.State(treeNeighbor(log, direction)); normalizeJavaResource(name) == "shelf_mushroom" {
								adjacent = true
							}
						}
						if !adjacent {
							treeShelfMushroom(region, p, facing, false, r)
						}
					}
				}
			}
		}
		return
	}
	dir := treeDirection(r.NextInt(4))
	clockwise := [2]int32{-dir[1], dir[0]}
	directions := []string{treeDirName(dir), treeDirName(clockwise)}
	baseY := trunks[0].y
	for _, log := range trunks {
		dy := log.y - baseY
		if dy < 1 || dy > 4 {
			continue
		}
		for _, facing := range directions {
			if r.NextFloat() <= .25 {
				p := treeNeighbor(log, facing)
				if !treeReplaceable(region, p) || treeIsWaterNearby(region, p) {
					continue
				}
				below := treePos(p, 0, -1, 0)
				name, _ := region.State(below)
				if normalizeJavaResource(name) != "shelf_mushroom" {
					treeShelfMushroom(region, p, facing, true, r)
					break
				}
			}
		}
	}
}
func treeDirName(dir [2]int32) string {
	if dir[0] < 0 {
		return "west"
	}
	if dir[0] > 0 {
		return "east"
	}
	if dir[1] < 0 {
		return "north"
	}
	return "south"
}
func placeCreakingHeart(region *featureRegion, d map[string]any, trunks []featurePos, r *java26.Random) {
	if len(trunks) == 0 || r.NextFloat() >= treeProbability(d, "probability", 0) {
		return
	}
	for _, p := range treeShufflePositions(trunks, r) {
		valid := true
		for _, dir := range []string{"down", "up", "north", "south", "west", "east"} {
			name, _ := region.State(treeNeighbor(p, dir))
			if !treeTagContains("#minecraft:logs", name) {
				valid = false
				break
			}
		}
		if valid {
			treeSetNamed(region, p, "creaking_heart", map[string]any{"natural": "true", "state": "dormant", "axis": "y"})
			return
		}
	}
}
