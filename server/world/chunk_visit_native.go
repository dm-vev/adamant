package world

import "math"

// nextChunkVisitState is the nonzero eight-bit LFSR used by
// TickingAreaView::tick's once-per-second rectangle traversal.
func nextChunkVisitState(state uint8) uint8 {
	feedback := (state ^ state>>2 ^ state>>3 ^ state>>4) & 1
	return state>>1 | feedback<<7
}

// Level::_subTick staggers area randomization by its index in the dimension's
// area vector. Areas beyond the twentieth reuse the corresponding phase.
func randomizeChunkArea(tick int64, index uint64) bool {
	return uint64(tick)%20 == index%20
}

// nativeChunkCircleContains follows TickingAreaView::tick's binary32 sphere
// test at Y=0. The radius is half the inclusive x span plus native sqrt(3),
// and the squared-distance comparison is strict. Bounds are checked separately.
func nativeChunkCircleContains(dx, dz int64, radius int32) bool {
	span := float32(int64(radius)*2 + 1)
	r := float32(span * 0.5)
	r = float32(r + float32(1.7320507764816284))
	x, z := float32(dx), float32(dz)
	return float32(r*r) > float32(float32(x*x)+float32(z*z))
}

func blockColumnWithinArea(pos ChunkPos, area loaderActiveArea) bool {
	dx, dz := int64(pos[0])-int64(area.pos[0]), int64(pos[1])-int64(area.pos[1])
	r := int64(area.radius)
	if dx < -r || dx > r || dz < -r || dz > r {
		return false
	}
	return area.square || nativeChunkCircleContains(dx, dz, area.radius)
}

func blockColumnWithinAreas(pos ChunkPos, areas []loaderActiveArea) bool {
	for _, area := range areas {
		if blockColumnWithinArea(pos, area) {
			return true
		}
	}
	return false
}

// blockAreaSetIndexMaxRadius is the maximum loader area radius expanded into
// blockAreaSet.chunks. Standard Bedrock simulation distances (4..12) stay well
// below this bound; larger custom areas fall back to blockAreaSet.large.
const blockAreaSetIndexMaxRadius = 16

// blockAreaKey identifies the geometry of a loaderActiveArea for deduplication.
type blockAreaKey struct {
	pos    ChunkPos
	radius int32
	square bool
}

// blockAreaSet caches chunk membership across a slice of loaderActiveAreas so
// repeated containment queries are O(1) instead of O(len(areas)).
type blockAreaSet struct {
	chunks    map[ChunkPos]struct{}
	seenAreas map[blockAreaKey]struct{}
	unique    []loaderActiveArea
	large     []loaderActiveArea
	expanded  bool
}

func areaCanOverflowInt32(area loaderActiveArea) bool {
	r := int64(area.radius)
	return int64(area.pos[0])-r < math.MinInt32 || int64(area.pos[0])+r > math.MaxInt32 ||
		int64(area.pos[1])-r < math.MinInt32 || int64(area.pos[1])+r > math.MaxInt32
}

// reset deduplicates areas into s.unique and clears any previously expanded set.
func (s *blockAreaSet) reset(areas []loaderActiveArea) {
	s.expanded = false
	s.unique = s.unique[:0]
	s.large = s.large[:0]
	if len(areas) == 0 {
		return
	}
	if s.seenAreas == nil {
		s.seenAreas = make(map[blockAreaKey]struct{}, len(areas))
	} else {
		clear(s.seenAreas)
	}
	for _, area := range areas {
		if area.radius < 0 {
			continue
		}
		key := blockAreaKey{pos: area.pos, radius: area.radius, square: area.square}
		if _, dup := s.seenAreas[key]; dup {
			continue
		}
		s.seenAreas[key] = struct{}{}
		s.unique = append(s.unique, area)
	}
}

// expand precomputes the exact set of ChunkPos covered by s.unique (up to
// blockAreaSetIndexMaxRadius), placing unusually large areas in s.large.
func (s *blockAreaSet) expand() {
	if s.chunks == nil {
		s.chunks = make(map[ChunkPos]struct{})
	} else {
		clear(s.chunks)
	}
	for _, area := range s.unique {
		if area.radius == 0 {
			s.chunks[area.pos] = struct{}{}
			continue
		}
		if area.radius > blockAreaSetIndexMaxRadius || areaCanOverflowInt32(area) {
			s.large = append(s.large, area)
			continue
		}
		r := area.radius
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if area.square || nativeChunkCircleContains(int64(dx), int64(dz), r) {
					s.chunks[ChunkPos{area.pos[0] + dx, area.pos[1] + dz}] = struct{}{}
				}
			}
		}
	}
	s.expanded = true
}

// contains reports whether pos lies within any area of s, with exact
// equivalence to blockColumnWithinAreas(pos, areas).
func (s *blockAreaSet) contains(pos ChunkPos) bool {
	if !s.expanded {
		return blockColumnWithinAreas(pos, s.unique)
	}
	if _, ok := s.chunks[pos]; ok {
		return true
	}
	return len(s.large) != 0 && blockColumnWithinAreas(pos, s.large)
}

// visitChunkRectangle visits each coordinate once. Ordinary ticks use z/x
// raster order; shuffled ticks consume two bounded draws, then walk independent
// nonzero LFSR cycles for x and z. Callbacks execute inline so an earlier area's
// actors consume RNG before the following area's traversal is selected.
// Native area spans fit in unsigned bytes. Larger custom areas use the caller's
// existing fallback instead of truncating their bounds.
func visitChunkRectangle(minimum, maximum ChunkPos, shuffled bool, bounded func(int) int, visit func(ChunkPos)) bool {
	width, depth := int64(maximum[0])-int64(minimum[0])+1, int64(maximum[1])-int64(minimum[1])+1
	if width < 1 || depth < 1 || width > 255 || depth > 255 {
		return false
	}
	if !shuffled {
		for z := int64(minimum[1]); z <= int64(maximum[1]); z++ {
			for x := int64(minimum[0]); x <= int64(maximum[0]); x++ {
				visit(ChunkPos{int32(x), int32(z)})
			}
		}
		return true
	}
	xStart, zStart := uint8(bounded(255)+1), uint8(bounded(255)+1)
	x := xStart
	for {
		x = nextChunkVisitState(x)
		if int64(x) <= width {
			z := zStart
			for {
				z = nextChunkVisitState(z)
				if int64(z) <= depth {
					visit(ChunkPos{minimum[0] + int32(x) - 1, minimum[1] + int32(z) - 1})
				}
				if z == zStart {
					break
				}
			}
		}
		if x == xStart {
			break
		}
	}
	return true
}
