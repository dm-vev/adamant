package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

func placeModernVegetationPatch(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random, waterlogged bool) bool {
	replaceable, ok := config["replaceable"]
	if !ok {
		panic("overworld: malformed Java vegetation patch replaceable set")
	}
	groundState, ok := config["ground_state"]
	if !ok {
		panic("overworld: malformed Java vegetation patch ground state provider")
	}
	vegetation, ok := config["vegetation_feature"]
	if !ok {
		panic("overworld: malformed Java vegetation patch vegetation feature")
	}
	surfaceName, ok := config["surface"].(string)
	if !ok {
		panic("overworld: malformed Java vegetation patch surface")
	}
	var inward, outward featurePos
	switch normalizeJavaResource(surfaceName) {
	case "floor":
		inward, outward = featurePos{y: -1}, featurePos{y: 1}
	case "ceiling":
		inward, outward = featurePos{y: 1}, featurePos{y: -1}
	default:
		panic("overworld: unsupported Java vegetation patch surface " + normalizeJavaResource(surfaceName))
	}
	depth, depthOK := config["depth"]
	verticalRange, verticalOK := config["vertical_range"]
	vegetationChance, chanceOK := config["vegetation_chance"].(float64)
	xzRadius, radiusOK := config["xz_radius"]
	extraBottom, bottomOK := config["extra_bottom_block_chance"].(float64)
	extraEdge, edgeOK := config["extra_edge_column_chance"].(float64)
	if !depthOK || !verticalOK || !chanceOK || !radiusOK || !bottomOK || !edgeOK {
		panic("overworld: malformed Java vegetation patch configuration")
	}
	vr := int32(featureNumber(verticalRange))
	xRadius := sampleJavaInt(xzRadius, random) + 1
	zRadius := sampleJavaInt(xzRadius, random) + 1
	surface := make([]featurePos, 0, (2*xRadius+1)*(2*zRadius+1)-4)
	for dx := -xRadius; dx <= xRadius; dx++ {
		isXEdge := dx == -xRadius || dx == xRadius
		for dz := -zRadius; dz <= zRadius; dz++ {
			isZEdge := dz == -zRadius || dz == zRadius
			isEdge := isXEdge || isZEdge
			isCorner := isXEdge && isZEdge
			isEdgeButNotCorner := isEdge && !isCorner
			if isCorner || isEdgeButNotCorner && (extraEdge == 0 || random.NextFloat() > float32(extraEdge)) {
				continue
			}
			pos := featurePos{origin.x + int32(dx), origin.y, origin.z + int32(dz)}
			for offset := int32(0); offset < vr && featureRegionAir(region, pos); offset++ {
				pos.x += inward.x
				pos.y += inward.y
				pos.z += inward.z
			}
			for offset := int32(0); offset < vr && !featureRegionAir(region, pos); offset++ {
				pos.x += outward.x
				pos.y += outward.y
				pos.z += outward.z
			}
			below := featurePos{pos.x + inward.x, pos.y + inward.y, pos.z + inward.z}
			if !featureRegionAir(region, pos) || !vegetationPatchSturdy(region, below, outward) {
				continue
			}
			patchDepth := sampleJavaInt(depth, random)
			if extraBottom > 0 && random.NextFloat() < float32(extraBottom) {
				patchDepth++
			}
			groundPos := below
			groundPlaced := true
			for i := range patchDepth {
				groundNative, groundActor, sampled := region.SampleNativeState(groundState, below, random)
				if !sampled {
					groundPlaced = false
					break
				}
				registry := region.c.BlockRegistry()
				sampledName, ok := javadata.BlockStateName(groundNative.CanonicalJavaID(registry))
				if !ok {
					panic("overworld: Java vegetation patch sampled unknown block state")
				}
				groundName, ok := javadata.BlockStateName(region.nativeState(below).CanonicalJavaID(registry))
				if !ok {
					panic("overworld: Java vegetation patch encountered unknown ground block state")
				}
				if normalizeJavaResource(sampledName) != normalizeJavaResource(groundName) {
					if !featureBlockHolderContains(replaceable, groundName) {
						groundPlaced = i != 0
						break
					}
					featureSetNativeBlockFlag2(region, below, groundNative, groundActor)
					below.x += inward.x
					below.y += inward.y
					below.z += inward.z
				}
			}
			if groundPlaced {
				// Each column owns a distinct x/z, so this HashSet cannot contain duplicates.
				surface = append(surface, groundPos)
			}
		}
	}
	surface = vegetationPatchHashSetOrder(surface)
	if waterlogged {
		waterSurface := make([]featurePos, 0, len(surface))
		waterRID := featureRequireState(region, "minecraft:water")
		for _, pos := range surface {
			if !vegetationPatchExposed(region, pos) {
				waterSurface = append(waterSurface, pos)
			}
		}
		waterSurface = vegetationPatchHashSetOrder(waterSurface)
		for _, pos := range waterSurface {
			featureSetNativeBlockFlag2(region, pos, waterRID, nil)
		}
		surface = waterSurface
	}
	for _, pos := range surface {
		if vegetationChance > 0 && random.NextFloat() < float32(vegetationChance) {
			placement := featurePos{pos.x + outward.x, pos.y + outward.y, pos.z + outward.z}
			if waterlogged {
				placement.y--
			}
			vegetationPlaced := region.PlacePlacedFeature(vegetation, [3]int32{placement.x, placement.y, placement.z}, random)
			if waterlogged && vegetationPlaced {
				id := region.nativeState(pos).CanonicalJavaID(region.c.BlockRegistry())
				waterloggedValue, hasWaterloggedProperty := javadata.BlockStateBool(id, "waterlogged")
				if hasWaterloggedProperty && !waterloggedValue {
					name, ok := javadata.BlockStateName(id)
					if !ok {
						panic("overworld: Java vegetation patch encountered unknown placed state")
					}
					if normalizeJavaResource(name) != "water" {
						updated, err := javadata.BlockStateWithProperty(id, "waterlogged", true)
						if err != nil {
							panic(err)
						}
						featureSetNativeBlockFlag2(region, pos, world.ResolveStateID(region.c.BlockRegistry(), updated), nil)
					}
				}
			}
		}
	}
	return len(surface) > 0
}

// vegetationPatchHashSetOrder reproduces Java HashSet iteration for BlockPos values.
// BlockPos inherits Vec3i.hashCode, and HashMap iterates its buckets in table order.
func vegetationPatchHashSetOrder(positions []featurePos) []featurePos {
	if len(positions) < 2 {
		return positions
	}
	capacity := 16
	for len(positions) > capacity*3/4 {
		capacity *= 2
	}
	// The codec caps xz_radius at 32: at most 4485 columns and 8192 buckets.
	// Stable counting distribution preserves bucket insertion order without
	// allocating a slice for each occupied Java HashMap bucket.
	var next [8192]uint16
	for _, pos := range positions {
		hash := uint32((pos.y+pos.z*31)*31 + pos.x)
		hash ^= hash >> 16
		next[hash&uint32(capacity-1)]++
	}
	var offset uint16
	for i := range capacity {
		count := next[i]
		next[i] = offset
		offset += count
	}
	ordered := make([]featurePos, len(positions))
	for _, pos := range positions {
		hash := uint32((pos.y+pos.z*31)*31 + pos.x)
		hash ^= hash >> 16
		index := hash & uint32(capacity-1)
		ordered[next[index]] = pos
		next[index]++
	}
	return ordered
}


func vegetationPatchSturdy(region *featureRegion, pos, faceDirection featurePos) bool {
	face := cube.FaceUp
	switch {
	case faceDirection.y < 0:
		face = cube.FaceDown
	case faceDirection.x < 0:
		face = cube.FaceWest
	case faceDirection.x > 0:
		face = cube.FaceEast
	case faceDirection.z < 0:
		face = cube.FaceNorth
	case faceDirection.z > 0:
		face = cube.FaceSouth
	}
	id := region.nativeState(pos).CanonicalJavaID(region.c.BlockRegistry())
	shape, err := javadata.BlockGenerationShapeID(id)
	if err != nil {
		panic(err)
	}
	return shape.Sturdy&(1<<uint(face)) != 0
}

func vegetationPatchExposed(region *featureRegion, pos featurePos) bool {
	neighbors := [...]featurePos{{z: -1}, {x: 1}, {z: 1}, {x: -1}, {y: -1}}
	faces := [...]featurePos{{z: 1}, {x: -1}, {z: -1}, {x: 1}, {y: 1}}
	for i, offset := range neighbors {
		adjacent := featurePos{pos.x + offset.x, pos.y + offset.y, pos.z + offset.z}
		if !vegetationPatchSturdy(region, adjacent, faces[i]) {
			return true
		}
	}
	return false
}
