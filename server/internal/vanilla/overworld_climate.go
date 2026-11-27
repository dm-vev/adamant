package vanilla

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

type climateNodeFlat struct {
	min   [6]int16
	max   [6]int16
	first uint16
	count uint8
	biome uint8
}

var (
	climateNodes []climateNodeFlat
	climateEdges []uint16
	climateRoot  uint16
)

func init() {
	nodeCount := int(binary.LittleEndian.Uint32(overworldClimateTree[:4]))
	climateRoot = uint16(binary.LittleEndian.Uint32(overworldClimateTree[4:8]))
	climateNodes = make([]climateNodeFlat, nodeCount)
	for i := range nodeCount {
		raw := overworldClimateTree[8+i*climateNodeSize : 8+(i+1)*climateNodeSize]
		n := &climateNodes[i]
		for axis := range 6 {
			n.min[axis] = int16(binary.LittleEndian.Uint16(raw[axis*4:]))
			n.max[axis] = int16(binary.LittleEndian.Uint16(raw[axis*4+2:]))
		}
		n.first = binary.LittleEndian.Uint16(raw[24:])
		n.count = raw[26]
		n.biome = raw[27]
	}
	edgeBytes := overworldClimateTree[8+nodeCount*climateNodeSize:]
	edgeCount := len(edgeBytes) / 2
	climateEdges = make([]uint16, edgeCount)
	for i := range edgeCount {
		climateEdges[i] = binary.LittleEndian.Uint16(edgeBytes[i*2:])
	}
}

const OverworldBiomeCount = len(overworldBiomeNames)

func OverworldBiomeName(index uint8) string {
	return overworldBiomeNames[index]
}

// FindOverworldBiome resolves Java's temperature, vegetation, continentalness,
// erosion, depth and ridge climate coordinates. Ties use tree order rather than
// a thread-local previous result, so chunk generation order cannot change them.
func FindOverworldBiome(climate [6]float32) string {
	return overworldBiomeNames[findOverworldBiomeIndex(climate, nil)]
}

// ClimateHint remembers the leaf a climate search found. Like the last result
// of Java's Climate.RTree it bounds the next search, which nearby climates
// make much shorter; unlike it, the result never depends on the hint.
type ClimateHint struct {
	leaf uint16
	ok   bool
}

func findOverworldBiomeIndex(climate [6]float32, hint *ClimateHint) uint8 {
	t0 := int64(climate[0] * 10000)
	t1 := int64(climate[1] * 10000)
	t2 := int64(climate[2] * 10000)
	t3 := int64(climate[3] * 10000)
	t4 := int64(climate[4] * 10000)
	t5 := int64(climate[5] * 10000)
	return findOverworldBiomeIndexFlat(t0, t1, t2, t3, t4, t5, hint)
}

func findOverworldBiomeIndexFlat(t0, t1, t2, t3, t4, t5 int64, hint *ClimateHint) uint8 {
	best, distance := uint16(0), int64(math.MaxInt64)
	if hint != nil && hint.ok {
		// Subtrees farther than the hinted leaf hold no leaf the search could
		// return, so it still finds the first nearest leaf in tree order.
		best, distance = hint.leaf, climateDistanceFlat(&climateNodes[hint.leaf], t0, t1, t2, t3, t4, t5, math.MaxInt64)+1
	}
	best, _ = searchClimateTreeFlat(climateRoot, t0, t1, t2, t3, t4, t5, best, distance)
	if hint != nil {
		hint.leaf, hint.ok = best, true
	}
	return climateNodes[best].biome
}

func climateDistanceFlat(n *climateNodeFlat, t0, t1, t2, t3, t4, t5, limit int64) int64 {
	d := max(int64(n.min[0])-t0, t0-int64(n.max[0]), 0)
	dist := d * d
	if dist >= limit {
		return dist
	}
	d = max(int64(n.min[1])-t1, t1-int64(n.max[1]), 0)
	dist += d * d
	if dist >= limit {
		return dist
	}
	d = max(int64(n.min[2])-t2, t2-int64(n.max[2]), 0)
	dist += d * d
	if dist >= limit {
		return dist
	}
	d = max(int64(n.min[3])-t3, t3-int64(n.max[3]), 0)
	dist += d * d
	if dist >= limit {
		return dist
	}
	d = max(int64(n.min[4])-t4, t4-int64(n.max[4]), 0)
	dist += d * d
	if dist >= limit {
		return dist
	}
	d = max(int64(n.min[5])-t5, t5-int64(n.max[5]), 0)
	return dist + d*d
}

func searchClimateTreeFlat(index uint16, t0, t1, t2, t3, t4, t5 int64, best uint16, distance int64) (uint16, int64) {
	node := &climateNodes[index]
	count := int(node.count)
	if count == 0 {
		return index, climateDistanceFlat(node, t0, t1, t2, t3, t4, t5, math.MaxInt64)
	}
	edges := climateEdges[node.first : int(node.first)+count]
	for _, childIndex := range edges {
		childNode := &climateNodes[childIndex]
		childDistance := climateDistanceFlat(childNode, t0, t1, t2, t3, t4, t5, distance)
		if childDistance >= distance {
			continue
		}
		if childNode.count == 0 {
			best, distance = childIndex, childDistance
		} else {
			best, distance = searchClimateTreeFlat(childIndex, t0, t1, t2, t3, t4, t5, best, distance)
		}
		if distance == 0 {
			break
		}
	}
	return best, distance
}

// climateDepthRegion partitions integer depth*10000 by the endpoints of all
// leaf depth intervals in vanilla's Overworld climate R-tree:
// (0,0), (2000,9000), (10000,10000), (11000,11000).
// Within any single region, if the same R-tree leaf wins at both endpoints
// of a sub-interval, it mathematically wins at every interior point with
// identical tie-breaking.
func climateDepthRegion(d int64) uint8 {
	switch {
	case d <= 0:
		return 0
	case d <= 2000:
		return 1
	case d <= 9000:
		return 2
	case d <= 10000:
		return 3
	case d <= 11000:
		return 4
	default:
		return 5
	}
}

// FillOverworldBiomeColumn fills dst for quartY in -16..79 along one column
// without recomputing fixed 5D coordinates or walking interior R-tree levels
// for constant depth intervals.
func FillOverworldBiomeColumn(c OverworldColumn, dst *[96]uint8, hint *ClimateHint) {
	t0 := int64(c.Temperature * 10000)
	t1 := int64(c.Vegetation * 10000)
	t2 := int64(c.Continentalness * 10000)
	t3 := int64(c.Erosion * 10000)
	t5 := int64(c.Weirdness * 10000)

	var depths [96]int64
	var regions [96]uint8
	for y := range int32(96) {
		d := int64(c.Depth((y-16)*4) * 10000)
		depths[y] = d
		regions[y] = climateDepthRegion(d)
	}
	for start := 0; start < 96; {
		reg := regions[start]
		end := start
		for end+1 < 96 && regions[end+1] == reg {
			end++
		}
		hLo := *hint
		bLo := findOverworldBiomeIndexFlat(t0, t1, t2, t3, depths[start], t5, &hLo)
		dst[start] = bLo
		if start == end {
			*hint = hLo
			start = end + 1
			continue
		}
		hHi := hLo
		bHi := findOverworldBiomeIndexFlat(t0, t1, t2, t3, depths[end], t5, &hHi)
		dst[end] = bHi
		*hint = hHi
		if end > start+1 {
			if hLo == hHi {
				for y := start + 1; y < end; y++ {
					dst[y] = bLo
				}
			} else {
				bisectOverworldBiomeRange(t0, t1, t2, t3, t5, &depths, dst, start, bLo, hLo, end, bHi, hHi)
			}
		}
		start = end + 1
	}
}

func bisectOverworldBiomeRange(t0, t1, t2, t3, t5 int64, depths *[96]int64, dst *[96]uint8, lo int, bLo uint8, hLo ClimateHint, hi int, bHi uint8, hHi ClimateHint) {
	mid := (lo + hi) >> 1
	hMid := hLo
	bMid := findOverworldBiomeIndexFlat(t0, t1, t2, t3, depths[mid], t5, &hMid)
	dst[mid] = bMid
	if mid > lo+1 {
		if hLo == hMid {
			for y := lo + 1; y < mid; y++ {
				dst[y] = bLo
			}
		} else {
			bisectOverworldBiomeRange(t0, t1, t2, t3, t5, depths, dst, lo, bLo, hLo, mid, bMid, hMid)
		}
	}
	if hi > mid+1 {
		if hMid == hHi {
			for y := mid + 1; y < hi; y++ {
				dst[y] = bMid
			}
		} else {
			bisectOverworldBiomeRange(t0, t1, t2, t3, t5, depths, dst, mid, bMid, hMid, hi, bHi, hHi)
		}
	}
}
