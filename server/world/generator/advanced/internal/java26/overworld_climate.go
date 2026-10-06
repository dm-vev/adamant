package java26

import (
	_ "embed"
	"encoding/binary"
	"math"
)

// The bootstrap's 7,594 parameter points, compiled with Climate.RTree's
// 19-child partitioning and stable axis sorts. The seventh (offset) interval
// is identically zero and is omitted from the packed nodes.
//
//go:embed overworld_climate.bin
var overworldClimateTree []byte

const climateNodeSize = 28

const OverworldBiomeCount = len(overworldBiomeNames)

func OverworldBiomeName(index uint8) string {
	return overworldBiomeNames[index]
}

// FindOverworldBiome resolves Java's temperature, vegetation, continentalness,
// erosion, depth and ridge climate coordinates. Ties use tree order rather than
// a thread-local previous result, so chunk generation order cannot change them.
func FindOverworldBiome(climate [6]float32) string {
	return overworldBiomeNames[findOverworldBiomeIndex(climate)]
}

func findOverworldBiomeIndex(climate [6]float32) uint8 {
	var target [6]int64
	for axis, value := range climate {
		target[axis] = int64(value * 10000)
	}
	root := uint16(binary.LittleEndian.Uint32(overworldClimateTree[4:8]))
	best, _ := searchClimateTree(root, &target, 0, math.MaxInt64)
	return climateNode(best)[27]
}

func climateNode(index uint16) []byte {
	start := 8 + int(index)*climateNodeSize
	return overworldClimateTree[start : start+climateNodeSize]
}

func climateDistance(node []byte, target *[6]int64, limit int64) int64 {
	var distance int64
	for axis, value := range target {
		minimum := int64(int16(binary.LittleEndian.Uint16(node[axis*4:])))
		maximum := int64(int16(binary.LittleEndian.Uint16(node[axis*4+2:])))
		delta := max(minimum-value, value-maximum, 0)
		distance += delta * delta
		if distance >= limit {
			break
		}
	}
	return distance
}

func searchClimateTree(index uint16, target *[6]int64, best uint16, distance int64) (uint16, int64) {
	node := climateNode(index)
	count := int(node[26])
	if count == 0 {
		return index, climateDistance(node, target, math.MaxInt64)
	}
	first := int(binary.LittleEndian.Uint16(node[24:]))
	edges := 8 + int(binary.LittleEndian.Uint32(overworldClimateTree[:4]))*climateNodeSize
	for child := range count {
		childIndex := binary.LittleEndian.Uint16(overworldClimateTree[edges+(first+child)*2:])
		childNode := climateNode(childIndex)
		childDistance := climateDistance(childNode, target, distance)
		if childDistance >= distance {
			continue
		}
		if childNode[26] == 0 {
			best, distance = childIndex, childDistance
		} else {
			best, distance = searchClimateTree(childIndex, target, best, distance)
		}
		if distance == 0 {
			break
		}
	}
	return best, distance
}
