package world

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
