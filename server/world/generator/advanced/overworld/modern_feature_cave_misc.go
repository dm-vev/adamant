package overworld

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
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
	name, _ := region.State(p)
	switch normalizeJavaResource(name) {
	case "air", "cave_air", "void_air":
		return true
	default:
		return false
	}
}

type featureBlockSource struct {
	region *featureRegion
}

func (s featureBlockSource) Block(pos cube.Pos) world.Block {
	return s.region.Block(featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])})
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
		name, _ := region.State(p)
		return normalizeJavaResource(name) == "water"
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
	source := featureBlockSource{region: region}
	faceSolid := func(p featurePos, face cube.Face) bool {
		pos := cube.Pos{int(p.x), int(p.y), int(p.z)}
		return region.Block(p).Model().FaceSolid(pos, face, source)
	}
	visibleFromOutside := func(p featurePos, face cube.Face) bool {
		return !faceSolid(p, face)
	}
	placed := false
	for z := floor.z - r; z <= floor.z+r; z++ {
		for y := floor.y - r; y <= floor.y+r; y++ {
			for x := floor.x - r; x <= floor.x+r; x++ {
				p := featurePos{x, y, z}
				if random.NextFloat() >= float32(probability) {
					continue
				}
				name, _ := region.State(p)
				switch normalizeJavaResource(name) {
				case "water", "air", "cave_air", "void_air":
					continue
				}
				if visibleFromOutside(featurePos{p.x, p.y - 1, p.z}, cube.FaceUp) {
					continue
				}
				visible := false
				for _, neighbour := range [...]struct {
					dx, dz int32
					face   cube.Face
				}{
					{0, -1, cube.FaceSouth},
					{1, 0, cube.FaceWest},
					{0, 1, cube.FaceNorth},
					{-1, 0, cube.FaceEast},
				} {
					if visibleFromOutside(featurePos{p.x + neighbour.dx, p.y, p.z + neighbour.dz}, neighbour.face) {
						visible = true
						break
					}
				}
				if !visible && region.Set(p, magma) {
					placed = true
				}
			}
		}
	}
	return placed
}


func placeModernSpike(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	canPlace, ok := config["can_place_on"].(map[string]any)
	if !ok { panic("overworld: malformed Java spike placement predicate") }
	canReplace, ok := config["can_replace"].(map[string]any)
	if !ok { panic("overworld: malformed Java spike replace predicate") }
	state, ok := config["state"].(string)
	if !ok { panic("overworld: malformed Java spike state") }
	for origin.y > region.MinY()+2 {
		if !featureRegionAir(region, origin) { break }
		origin.y--
	}
	if !featureRegionPredicate(region, origin, canPlace) { return false }
	origin.y += random.NextInt(4)
	height := random.NextInt(4)+7
	width := height/4+random.NextInt(2)
	if width > 1 && random.NextInt(60)==0 { origin.y += 10+random.NextInt(30) }
	rid := featureRequireState(region,"minecraft:"+normalizeJavaResource(state))
	for y := int32(0); y < height; y++ {
		scale := (1-float32(y)/float32(height))*float32(width)
		newWidth := int32(math.Ceil(float64(scale)))
		for x := -newWidth; x <= newWidth; x++ {
			dx := float32(absI32(x))-0.25
			for z := -newWidth; z <= newWidth; z++ {
				dz := float32(absI32(z))-0.25
				inside := x == 0 && z == 0 || dx*dx+dz*dz <= scale*scale
				if !inside { continue }
				edge := x == -newWidth || x == newWidth || z == -newWidth || z == newWidth
				if edge && random.NextFloat() > 0.75 { continue }
				place := featurePos{origin.x+x,origin.y+y,origin.z+z}
				if featureRegionAir(region,place) || featureRegionPredicate(region,place,canReplace) { region.Set(place,rid) }
				if y != 0 && newWidth > 1 {
					place.y = origin.y-y
					if featureRegionAir(region,place) || featureRegionPredicate(region,place,canReplace) { region.Set(place,rid) }
				}
			}
		}
	}
	pillarWidth := width-1
	if pillarWidth < 0 { pillarWidth=0 } else if pillarWidth>1 { pillarWidth=1 }
	for x:=-pillarWidth; x<=pillarWidth; x++ {
		for z:=-pillarWidth; z<=pillarWidth; z++ {
			cursor:=featurePos{origin.x+x,origin.y-1,origin.z+z}
			runLength:=int32(50)
			if absI32(x)==1&&absI32(z)==1 { runLength=random.NextInt(5) }
			for cursor.y>50 {
				name,_:=region.State(cursor)
				if !featureRegionAir(region,cursor)&&!featureRegionPredicate(region,cursor,canReplace)&&normalizeJavaResource(name)!=normalizeJavaResource(state) { break }
				region.Set(cursor,rid);cursor.y--
				runLength--
				if runLength<=0 { cursor.y-=random.NextInt(5)+1;runLength=random.NextInt(5) }
			}
		}
	}
	return true
}

func featureRegionPredicate(region *featureRegion, p featurePos, predicate map[string]any) bool {
	typeName,ok:=predicate["type"].(string);if !ok {panic("overworld: Java block predicate without type")}
	name,_:=region.State(p)
	switch normalizeJavaResource(typeName) {
	case "true":
		return true
	case "false":
		return false
	case "not":
		child,ok:=predicate["predicate"].(map[string]any);if !ok{panic("overworld: malformed Java negated predicate")};return !featureRegionPredicate(region,p,child)
	
	case "matching_blocks":
		switch v:=predicate["blocks"].(type) {case string:return normalizeJavaResource(name)==normalizeJavaResource(v);case []any:for _,raw:=range v {if normalizeJavaResource(name)==normalizeJavaResource(raw.(string)){return true}};return false;default:panic("overworld: malformed matching_blocks predicate")}
	case "matching_block_tag":
		tag,ok:=predicate["tag"].(string);if !ok{panic("overworld: malformed matching_block_tag predicate")};return javaBlockTagContains(normalizeJavaResource(tag), normalizeJavaResource(name))
	case "all_of","any_of":
		predicates,ok:=predicate["predicates"].([]any);if !ok{panic("overworld: malformed compound block predicate")}
		all:=normalizeJavaResource(typeName)=="all_of"
		for _,raw:=range predicates {match:=featureRegionPredicate(region,p,raw.(map[string]any));if match!=all{return !all}}
		return all
	default: panic("overworld: unsupported Java block predicate "+typeName)
	}
}

func placeModernBlueIce(region *featureRegion, origin featurePos, random *java26.Random) bool {
	if origin.y > 62 { return false }
	name, _ := region.State(origin)
	below, _ := region.State(featurePos{origin.x, origin.y-1, origin.z})
	if name != "minecraft:water" && below != "minecraft:water" { return false }
	packed := false
	for _, d := range [][3]int32{{0,1,0},{0,0,-1},{0,0,1},{-1,0,0},{1,0,0}} {
		name, _ = region.State(featurePos{origin.x+d[0],origin.y+d[1],origin.z+d[2]})
		if name == "minecraft:packed_ice" { packed = true; break }
	}
	if !packed { return false }
	blue := featureRequireState(region, "minecraft:blue_ice")
	region.Set(origin, blue)
	for range 200 {
		yOff := random.NextInt(5)-random.NextInt(6)
		xzDiff := int32(3)
		if yOff < 2 { xzDiff += yOff/2 }
		if xzDiff < 1 { continue }
		p := featurePos{origin.x+random.NextInt(xzDiff)-random.NextInt(xzDiff), origin.y+yOff, origin.z+random.NextInt(xzDiff)-random.NextInt(xzDiff)}
		name,_ = region.State(p)
		if !featureRegionAir(region,p) && name != "minecraft:water" && name != "minecraft:packed_ice" && name != "minecraft:ice" { continue }
		for _,d := range [][3]int32{{0,-1,0},{0,1,0},{0,0,-1},{0,0,1},{-1,0,0},{1,0,0}} {
			neighbor,_ := region.State(featurePos{p.x+d[0],p.y+d[1],p.z+d[2]})
			if neighbor == "minecraft:blue_ice" { region.Set(p,blue); break }
		}
	}
	return true
}
func placeModernScatteredOre(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	size := int32(featureNumber(config["size"]))
	if size < 0 { panic("overworld: negative Java scattered ore size") }
	targets, ok := config["targets"].([]any)
	if !ok { panic("overworld: malformed Java scattered ore targets") }
	type replacement struct { state world.NativeState; predicate map[string]any }
	replacements := make([]replacement, 0, len(targets))
	for _, raw := range targets {
		entry, ok := raw.(map[string]any); if !ok { panic("overworld: malformed Java scattered ore target") }
		state, ok := entry["state"].(string); if !ok { panic("overworld: malformed Java scattered ore state") }
		predicate, ok := entry["target"].(map[string]any); if !ok { panic("overworld: malformed Java scattered ore rule") }
		replacements = append(replacements, replacement{featureRequireState(region,"minecraft:"+normalizeJavaResource(state)),predicate})
	}
	numberOfTries := random.NextInt(size+1)
	discardChance := float32(0)
	if raw,ok:=config["discard_chance_on_air_exposure"].(float64);ok { discardChance=float32(raw) }
	for i:=int32(0);i<numberOfTries;i++ {
		distance:=i;if distance>7{distance=7}
		axis:=func() int32 {
			difference:=random.NextFloat()-random.NextFloat()
			return int32(math.Floor(float64(difference*float32(distance))+0.5))
		}
		p:=featurePos{origin.x+axis(),origin.y+axis(),origin.z+axis()}
		name,_:=region.State(p)
		for _,target:=range replacements {
			if !orePredicateMatches(p,target.predicate,name){continue}
			skipAirCheck:=discardChance<=0
			if discardChance>0&&discardChance<1 {skipAirCheck=random.NextFloat()>=discardChance}
			if !skipAirCheck {
				exposed:=false
				for _,d:=range [][3]int32{{0,-1,0},{0,1,0},{0,0,-1},{0,0,1},{-1,0,0},{1,0,0}} {if featureRegionAir(region,featurePos{p.x+d[0],p.y+d[1],p.z+d[2]}){exposed=true;break}}
				if exposed {break}
			}
			region.Set(p,target.state)
			break
		}
	}
	return true
}

func placeModernReplaceBlob(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	target, ok := config["target"].(string)
	if !ok { panic("overworld: malformed Java replace-blob target") }
	state, ok := config["state"].(string)
	if !ok { panic("overworld: malformed Java replace-blob state") }
	target, state = normalizeJavaResource(target), normalizeJavaResource(state)
	center := origin
	if center.y > region.MaxY() { center.y = region.MaxY() }
	if center.y < region.MinY()+1 { center.y = region.MinY()+1 }
	for center.y > region.MinY()+1 {
		name, _ := region.State(center)
		if normalizeJavaResource(name) == target { break }
		center.y--
	}
	name, _ := region.State(center)
	if normalizeJavaResource(name) != target { return false }
	radius := config["radius"]
	rx := sampleJavaIntOrUniform(radius, 0, 0, random)
	ry := sampleJavaIntOrUniform(radius, 0, 0, random)
	rz := sampleJavaIntOrUniform(radius, 0, 0, random)
	maxRadius := rx
	if ry > maxRadius { maxRadius = ry }
	if rz > maxRadius { maxRadius = rz }
	replace := featureRequireState(region, "minecraft:"+state)
	replacedAny := false
	for depth := int32(0); depth <= rx+ry+rz; depth++ {
		maxX := rx
		if depth < maxX { maxX = depth }
		for x := -maxX; x <= maxX; x++ {
			maxY := ry
			if depth-absI32(x) < maxY { maxY = depth-absI32(x) }
			for y := -maxY; y <= maxY; y++ {
				z := depth-absI32(x)-absI32(y)
				if z > rz { continue }
				for _, zz := range []int32{z, -z} {
					if absI32(x)+absI32(y)+absI32(zz) > maxRadius { return replacedAny }
					p := featurePos{center.x+x, center.y+y, center.z+zz}
					name, _ := region.State(p)
					if normalizeJavaResource(name) == target { region.Set(p, replace); replacedAny = true }
					if zz == 0 { break }
				}
			}
		}
	}
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
	contents,ok:=config["contents"].(string);if !ok{panic("overworld: malformed Java delta contents")}
	rim,ok:=config["rim"].(string);if !ok{panic("overworld: malformed Java delta rim")}
	spawnRim:=random.NextDouble()<0.9
	rimX,rimZ:=int32(0),int32(0)
	if spawnRim {rimX=sampleJavaIntOrUniform(config["rim_size"],0,0,random);rimZ=sampleJavaIntOrUniform(config["rim_size"],0,0,random)}
	hasRim:=spawnRim&&rimX!=0&&rimZ!=0
	radiusX:=sampleJavaIntOrUniform(config["size"],0,0,random)
	radiusZ:=sampleJavaIntOrUniform(config["size"],0,0,random)
	radiusLimit:=radiusX;if radiusZ>radiusLimit{radiusLimit=radiusZ}
	contentRID:=featureRequireState(region,"minecraft:"+normalizeJavaResource(contents))
	rimRID:=featureRequireState(region,"minecraft:"+normalizeJavaResource(rim))
	placed:=false
	manhattanPositions(origin,radiusX,0,radiusZ,func(p featurePos,depth int32) bool {
		if depth>radiusLimit{return false}
		if caveDeltaClear(region,p,normalizeJavaResource(contents)) {
			if hasRim {placed=region.Set(p,rimRID)||placed}
			shifted:=featurePos{p.x+rimX,p.y,p.z+rimZ}
			if caveDeltaClear(region,shifted,normalizeJavaResource(contents)){placed=region.Set(shifted,contentRID)||placed}
		}
		return true
	})
	return placed
}
func caveDeltaClear(region *featureRegion,p featurePos,contents string)bool{
	name,_:=region.State(p)
	if normalizeJavaResource(name)==contents{return false}
	switch normalizeJavaResource(name){case "bedrock","nether_bricks","nether_brick_fence","nether_brick_stairs","nether_wart","chest","spawner":return false}
	for i,d:=range [][3]int32{{0,-1,0},{0,1,0},{0,0,-1},{0,0,1},{-1,0,0},{1,0,0}}{
		neighbor:=featurePos{p.x+d[0],p.y+d[1],p.z+d[2]}
		isAir:=featureRegionAir(region,neighbor)
		if isAir&&i!=1||!isAir&&i==1{return false}
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
		if !featureRegionPredicate(region, p, canReplace) {
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
