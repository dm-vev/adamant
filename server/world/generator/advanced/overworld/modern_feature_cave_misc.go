package overworld

import (
	"math"
	"strings"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

func placeModernIceFamily(region *featureRegion, typeName string, config map[string]any, origin featurePos, random *java26.Random) (bool, bool) {
	switch typeName {
	case "blue_ice":
		return true, placeModernBlueIce(region, origin, random)
	case "netherrack_replace_blobs":
		return true, placeModernReplaceBlob(region, config, origin, random)
	case "spike":
		return true, placeModernSpike(region, config, origin, random)
	case "scattered_ore":
		return true, placeModernScatteredOre(region, config, origin, random)
	case "random_neighbor_spread":
		return true, placeModernRandomNeighborSpread(region, config, origin, random)
	case "lake":
		return true, placeModernLake(region, config, origin, random)
	case "delta_feature":
		return true, placeModernDelta(region, config, origin, random)
	case "iceberg":
		return true, placeModernIceberg(region, config, origin, random)
	default:
		return false, false
	}
}

func featureRegionAir(region *featureRegion, p featurePos) bool {
	name, _ := javadata.BlockStateName(region.nativeState(p).CanonicalJavaID(region.c.BlockRegistry()))
	return treeIsAirName(name)
}

func placeModernUnderwaterMagma(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	floorSearchRange, ok := config["floor_search_range"].(float64)
	if !ok || floorSearchRange < 0 || floorSearchRange > 512 || floorSearchRange != math.Trunc(floorSearchRange) {
		panic("overworld: malformed Java underwater magma floor_search_range")
	}
	radius, ok := config["placement_radius_around_floor"].(float64)
	if !ok || radius < 0 || radius > 64 || radius != math.Trunc(radius) {
		panic("overworld: malformed Java underwater magma placement_radius_around_floor")
	}
	probability, ok := config["placement_probability_per_valid_position"].(float64)
	if !ok || probability < 0 || probability > 1 {
		panic("overworld: malformed Java underwater magma placement probability")
	}

	isWater := func(p featurePos) bool {
		name, _ := javadata.BlockStateName(region.nativeState(p).CanonicalJavaID(region.c.BlockRegistry()))
		return name == "minecraft:water"
	}
	if !isWater(origin) {
		return false
	}
	floor := origin
	searchRange := int32(floorSearchRange)
	for i := int32(1); i < searchRange && isWater(floor); i++ {
		floor.y--
	}
	if isWater(floor) {
		return false
	}

	r := int32(radius)
	magma := featureRequireState(region, "minecraft:magma_block")
	visibleFromOutside := func(p featurePos, direction int) bool {
		shape, err := javadata.BlockGenerationShapeID(region.nativeState(p).CanonicalJavaID(region.c.BlockRegistry()))
		if err != nil {
			panic(err)
		}
		return shape.Occlusion&(1<<uint(direction)) == 0
	}
	placed := false
	for z := floor.z - r; z <= floor.z+r; z++ {
		for y := floor.y - r; y <= floor.y+r; y++ {
			for x := floor.x - r; x <= floor.x+r; x++ {
				p := featurePos{x, y, z}
				if random.NextFloat() >= float32(probability) {
					continue
				}
				name, _ := javadata.BlockStateName(region.nativeState(p).CanonicalJavaID(region.c.BlockRegistry()))
				switch name {
				case "minecraft:water", "minecraft:air", "minecraft:cave_air", "minecraft:void_air":
					continue
				}
				if visibleFromOutside(featurePos{p.x, p.y - 1, p.z}, 1) {
					continue
				}
				visible := false
				for _, neighbour := range [...]struct {
					dx, dz    int32
					direction int
				}{
					{0, -1, 3},
					{1, 0, 4},
					{0, 1, 2},
					{-1, 0, 5},
				} {
					if visibleFromOutside(featurePos{p.x + neighbour.dx, p.y, p.z + neighbour.dz}, neighbour.direction) {
						visible = true
						break
					}
				}
				if !visible {
					region.setWithFlags(p, magma, 2)
					placed = true
				}
			}
		}
	}
	return placed
}

func placeModernSpike(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	canPlace, ok := config["can_place_on"].(map[string]any)
	if !ok {
		panic("overworld: malformed Java spike placement predicate")
	}
	canReplace, ok := config["can_replace"].(map[string]any)
	if !ok {
		panic("overworld: malformed Java spike replace predicate")
	}
	for featureRegionAir(region, origin) && origin.y > region.MinY()+2 {
		origin.y--
	}
	if !featurePredicate(region, origin, canPlace) {
		return false
	}
	placement, ok := featureStateBlock(config["state"])
	if !ok {
		panic("overworld: malformed Java spike state")
	}
	// A literal Source BlockState is independent of the fluid it overwrites.
	state, err := placement.ResolveNative(region.c.BlockRegistry())
	if err != nil {
		panic(err)
	}
	sourceStateID := state.CanonicalJavaID(region.c.BlockRegistry())
	origin.y += random.NextInt(4)
	height := random.NextInt(4) + 7
	width := height/4 + random.NextInt(2)
	if width > 1 && random.NextInt(60) == 0 {
		origin.y += 10 + random.NextInt(30)
	}
	for y := range height {
		scale := (1 - float32(y)/float32(height)) * float32(width)
		newWidth := int32(math.Ceil(float64(scale)))
		for x := -newWidth; x <= newWidth; x++ {
			dx := float32(absI32(x)) - 0.25
			for z := -newWidth; z <= newWidth; z++ {
				dz := float32(absI32(z)) - 0.25
				if x != 0 || z != 0 {
					if dx*dx+dz*dz > scale*scale {
						continue
					}
				}
				edge := x == -newWidth || x == newWidth || z == -newWidth || z == newWidth
				if edge && random.NextFloat() > 0.75 {
					continue
				}
				p := featurePos{origin.x + x, origin.y + y, origin.z + z}
				if featureRegionAir(region, p) || featurePredicate(region, p, canReplace) {
					region.SetNativeBlock(p, state, placement.NativeBlock)
				}
				if y != 0 && newWidth > 1 {
					p.y = origin.y - y
					if featureRegionAir(region, p) || featurePredicate(region, p, canReplace) {
						region.SetNativeBlock(p, state, placement.NativeBlock)
					}
				}
			}
		}
	}
	pillarWidth := max(int32(0), min(int32(1), width-1))
	for x := -pillarWidth; x <= pillarWidth; x++ {
		for z := -pillarWidth; z <= pillarWidth; z++ {
			cursor := featurePos{origin.x + x, origin.y - 1, origin.z + z}
			runLength := int32(50)
			if absI32(x) == 1 && absI32(z) == 1 {
				runLength = random.NextInt(5)
			}
			for cursor.y > 50 {
				id := region.nativeState(cursor).CanonicalJavaID(region.c.BlockRegistry())
				name, _ := javadata.BlockStateName(id)
				if !treeIsAirName(name) && !featurePredicate(region, cursor, canReplace) && id != sourceStateID {
					break
				}
				region.SetNativeBlock(cursor, state, placement.NativeBlock)
				cursor.y--
				runLength--
				if runLength <= 0 {
					cursor.y -= random.NextInt(5) + 1
					runLength = random.NextInt(5)
				}
			}
		}
	}
	return true
}

func placeModernBlueIce(region *featureRegion, origin featurePos, random *java26.Random) bool {
	if origin.y > region.f.source.SeaLevel()-1 {
		return false
	}
	nameAt := func(p featurePos) string {
		name, _ := javadata.BlockStateName(region.nativeState(p).CanonicalJavaID(region.c.BlockRegistry()))
		return name
	}
	if nameAt(origin) != "minecraft:water" && nameAt(featurePos{origin.x, origin.y - 1, origin.z}) != "minecraft:water" {
		return false
	}
	packed := false
	for _, d := range [...]featurePos{{0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		if nameAt(featurePos{origin.x + d.x, origin.y + d.y, origin.z + d.z}) == "minecraft:packed_ice" {
			packed = true
			break
		}
	}
	if !packed {
		return false
	}
	blue := featureRequireState(region, "minecraft:blue_ice")
	region.setWithFlags(origin, blue, 2)
	for range 200 {
		yOff := random.NextInt(5) - random.NextInt(6)
		xzDiff := int32(3)
		if yOff < 2 {
			xzDiff += yOff / 2
		}
		if xzDiff < 1 {
			continue
		}
		p := featurePos{origin.x + random.NextInt(xzDiff) - random.NextInt(xzDiff), origin.y + yOff, origin.z + random.NextInt(xzDiff) - random.NextInt(xzDiff)}
		name := nameAt(p)
		if !treeIsAirName(name) && name != "minecraft:water" && name != "minecraft:packed_ice" && name != "minecraft:ice" {
			continue
		}
		for _, d := range [...]featurePos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			if nameAt(featurePos{p.x + d.x, p.y + d.y, p.z + d.z}) == "minecraft:blue_ice" {
				region.setWithFlags(p, blue, 2)
				break
			}
		}
	}
	return true
}
func placeModernScatteredOre(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	size := int32(featureNumber(config["size"]))
	targets, ok := config["targets"].([]any)
	if !ok || size < 0 || size > 64 {
		panic("overworld: malformed Java scattered ore configuration")
	}
	var targetBuffer [2]modernOreTarget
	replacements := targetBuffer[:0]
	if len(targets) > len(targetBuffer) {
		replacements = make([]modernOreTarget, 0, len(targets))
	}
	for _, raw := range targets {
		entry := raw.(map[string]any)
		placement, ok := featureStateBlock(entry["state"])
		if !ok {
			panic("overworld: malformed Java scattered ore target state")
		}
		predicate, ok := entry["target"].(map[string]any)
		if !ok {
			panic("overworld: malformed Java scattered ore target predicate")
		}
		state, actor := region.resolveFeatureBlock(placement, origin)
		replacements = append(replacements, modernOreTarget{rid: state, actor: actor, predicate: predicate})
	}
	numberOfTries := random.NextInt(size + 1)
	discard := float32(number(config["discard_chance_on_air_exposure"]))
	for i := range numberOfTries {
		distance := min(i, 7)
		axis := func() int32 {
			difference := random.NextFloat() - random.NextFloat()
			return int32(math.Floor(float64(difference*float32(distance)) + .5))
		}
		p := featurePos{origin.x + axis(), origin.y + axis(), origin.z + axis()}
		stateID := region.nativeState(p).CanonicalJavaID(region.c.BlockRegistry())
		for _, target := range replacements {
			if !featureRuleMatches(p, target.predicate, stateID, random) {
				continue
			}
			if discard > 0 && (discard >= 1 || random.NextFloat() < discard) {
				exposed := false
				for _, d := range [6]featurePos{{y: -1}, {y: 1}, {z: -1}, {z: 1}, {x: -1}, {x: 1}} {
					neighbour := featurePos{p.x + d.x, p.y + d.y, p.z + d.z}
					name, _ := javadata.BlockStateName(region.nativeState(neighbour).CanonicalJavaID(region.c.BlockRegistry()))
					if treeIsAirName(name) {
						exposed = true
						break
					}
				}
				if exposed {
					continue
				}
			}
			featureSetNativeBlockFlag2(region, p, target.rid, target.actor)
			break
		}
	}
	return true
}

func placeModernReplaceBlob(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	targetPlacement, ok := featureStateBlock(config["target"])
	if !ok {
		panic("overworld: malformed Java replace-blob target")
	}
	replacePlacement, ok := featureStateBlock(config["state"])
	if !ok {
		panic("overworld: malformed Java replace-blob state")
	}
	targetState, _ := region.resolveFeatureBlock(targetPlacement, origin)
	targetBlock, _ := javadata.BlockStateName(targetState.CanonicalJavaID(region.c.BlockRegistry()))
	center := origin
	if center.y > region.MaxY() {
		center.y = region.MaxY()
	}
	if center.y < region.MinY()+1 {
		center.y = region.MinY() + 1
	}
	found := false
	for center.y > region.MinY()+1 {
		name, _ := javadata.BlockStateName(region.nativeState(center).CanonicalJavaID(region.c.BlockRegistry()))
		if name == targetBlock {
			found = true
			break
		}
		center.y--
	}
	if !found {
		return false
	}
	radius := config["radius"]
	rx := sampleJavaIntOrUniform(radius, 0, 0, random)
	ry := sampleJavaIntOrUniform(radius, 0, 0, random)
	rz := sampleJavaIntOrUniform(radius, 0, 0, random)
	maxRadius := max(rx, ry, rz)
	replaceState, actor := region.resolveFeatureBlock(replacePlacement, origin)
	replacedAny := false
	manhattanPositions(center, rx, ry, rz, func(p featurePos, depth int32) bool {
		if depth > maxRadius {
			return false
		}
		name, _ := javadata.BlockStateName(region.nativeState(p).CanonicalJavaID(region.c.BlockRegistry()))
		if name == targetBlock {
			featureSetNativeBlockFlag2(region, p, replaceState, actor)
			replacedAny = true
		}
		return true
	})
	return replacedAny
}

func featureRequireState(region *featureRegion, id string) world.NativeState {
	if !strings.Contains(id, ":") { id = "minecraft:" + id }
	state, err := world.ResolveState(region.c.BlockRegistry(), world.BlockState{Name: id})
	if err != nil { panic(err) }
	return state
}
func absI32(x int32) int32 { if x<0 { return -x }; return x }
func placeModernDelta(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	contents, ok := featureStateBlock(config["contents"])
	if !ok {
		panic("overworld: malformed Java delta contents")
	}
	rim, ok := featureStateBlock(config["rim"])
	if !ok {
		panic("overworld: malformed Java delta rim")
	}
	spawnRim := random.NextDouble() < 0.9
	rimX, rimZ := int32(0), int32(0)
	if spawnRim {
		rimX = sampleJavaIntOrUniform(config["rim_size"], 0, 0, random)
		rimZ = sampleJavaIntOrUniform(config["rim_size"], 0, 0, random)
	}
	hasRim := spawnRim && rimX != 0 && rimZ != 0
	radiusX := sampleJavaIntOrUniform(config["size"], 0, 0, random)
	radiusZ := sampleJavaIntOrUniform(config["size"], 0, 0, random)
	radiusLimit := radiusX
	if radiusZ > radiusLimit {
		radiusLimit = radiusZ
	}
	contentsState, contentsActor := region.resolveFeatureBlock(contents, origin)
	rimState, rimActor := region.resolveFeatureBlock(rim, origin)
	contentsBlockName := contents.State.Name
	if !strings.Contains(contentsBlockName, ":") {
		contentsBlockName = "minecraft:" + contentsBlockName
	}
	placed := false
	manhattanPositions(origin, radiusX, 0, radiusZ, func(p featurePos, depth int32) bool {
		if depth > radiusLimit {
			return false
		}
		if caveDeltaClear(region, p, contentsBlockName) {
			if hasRim {
				placed = true
				featureSetNativeBlockFlag2(region, p, rimState, rimActor)
			}
			shifted := featurePos{p.x + rimX, p.y, p.z + rimZ}
			if caveDeltaClear(region, shifted, contentsBlockName) {
				placed = true
				featureSetNativeBlockFlag2(region, shifted, contentsState, contentsActor)
			}
		}
		return true
	})
	return placed
}
func caveDeltaClear(region *featureRegion, p featurePos, contents string) bool {
	name, _ := javadata.BlockStateName(region.nativeState(p).CanonicalJavaID(region.c.BlockRegistry()))
	if name == contents {
		return false
	}
	switch name {
	case "minecraft:bedrock", "minecraft:nether_bricks", "minecraft:nether_brick_fence", "minecraft:nether_brick_stairs", "minecraft:nether_wart", "minecraft:chest", "minecraft:spawner":
		return false
	}
	for i := range 6 {
		nx, ny, nz := p.x, p.y, p.z
		switch i {
		case 0:
			ny--
		case 1:
			ny++
		case 2:
			nz--
		case 3:
			nz++
		case 4:
			nx--
		case 5:
			nx++
		}
		isAir := featureRegionAir(region, featurePos{nx, ny, nz})
		if isAir && i != 1 || !isAir && i == 1 {
			return false
		}
	}
	return true
}
func manhattanPositions(origin featurePos,reachX,reachY,reachZ int32,visit func(featurePos,int32)bool){
	maxDepth:=reachX+reachY+reachZ
	for depth:=int32(0);depth<=maxDepth;depth++{
		maxX:=reachX;if depth<maxX{maxX=depth}
		for x:=-maxX;x<=maxX;x++{
			maxY:=reachY;if depth-absI32(x)<maxY{maxY=depth-absI32(x)}
			for y:=-maxY;y<=maxY;y++{
				z:=depth-absI32(x)-absI32(y)
				if z>reachZ{continue}
				for _,zz:=range []int32{z,-z}{
					if !visit(featurePos{origin.x+x,origin.y+y,origin.z+zz},depth){return}
					if zz==0{break}
				}
			}
		}
	}
}
func placeModernRandomNeighborSpread(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	blockProvider := config["block"]
	state, actor, ok := region.SampleNativeState(blockProvider, origin, random)
	if !ok {
		panic("overworld: unresolved Java random-neighbor block provider")
	}
	featureSetNativeBlockFlag2(region, origin, state, actor)
	attempts := sampleJavaInt(config["attempts"], random)
	canReplace, ok := config["can_replace"].(map[string]any)
	if !ok {
		panic("overworld: malformed Java random-neighbor replace predicate")
	}
	accepted := config["accepted_neighbors"]
	for range attempts {
		p := featurePos{origin.x + sampleJavaInt(config["xz_offset"], random), origin.y + sampleJavaInt(config["y_offset"], random), origin.z + sampleJavaInt(config["xz_offset"], random)}
		if !featurePredicate(region, p, canReplace) {
			continue
		}
		neighbors := 0
		for _, d := range [...][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			name, _ := region.State(featurePos{p.x + d[0], p.y + d[1], p.z + d[2]})
			if featureBlockHolderContains(accepted, name) {
				neighbors++
				if neighbors > 1 {
					break
				}
			}
		}
		if neighbors == 1 {
			state, actor, ok = region.SampleNativeState(blockProvider, p, random)
			if !ok {
				panic("overworld: unresolved Java random-neighbor block provider")
			}
			featureSetNativeBlockFlag2(region, p, state, actor)
		}
	}
	return true
}
