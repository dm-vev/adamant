package world

import (
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// RedirectLightning applies the native rod search used by natural lightning
// and skeleton-horse traps, retaining pos if no eligible rod is found.
func (tx *Tx) RedirectLightning(pos mgl64.Vec3) mgl64.Vec3 {
	if rod, ok := tx.findLightningRod(cube.PosFromVec3(pos)); ok {
		return rod.Vec3()
	}
	return pos
}

// findLightningRod follows LevelChunk::findExposedLightningRod: enumerate the
// loaded cylinder, sort candidates, then skip ineligible rods. A covered nearest
// candidate must not prevent a farther exposed rod from attracting the bolt.
func (tx *Tx) findLightningRod(center cube.Pos) (cube.Pos, bool) {
	w := tx.World()
	if !w.raining() {
		return cube.Pos{}, false
	}
	positions := make([]cube.Pos, 0)
	for pos, poi := range w.pois {
		if poi.Kind != Other || !lightningRodName(poi.Type) {
			continue
		}
		b, loaded := w.blockLoaded(pos)
		if !loaded {
			continue
		}
		name, _ := b.EncodeBlock()
		if lightningRodName(name) {
			positions = append(positions, pos)
		}
	}
	for _, candidate := range orderLightningRods(center, positions) {
		pos := candidate.pos
		above := pos.Side(cube.FaceUp)
		if above.OutOfBounds(tx.Range()) || !tx.biome(above).HasPrecipitation() ||
			float32(tx.temperature(above)) <= float32(.15) || !tx.lightningRainReaches(above) {
			continue
		}
		return pos, true
	}
	return cube.Pos{}, false
}

func (tx *Tx) lightningRainReaches(pos cube.Pos) bool {
	w := tx.World()
	c, ok := w.loadedChunk(chunkPosFromBlockPos(pos))
	if !ok {
		return false
	}
	src := &loadedWorldSource{w: w, complete: true}
	for y := int(c.HighestBlock(uint8(pos[0]), uint8(pos[2]))); y >= pos[1]; y-- {
		p := cube.Pos{pos[0], y, pos[2]}
		b := src.Block(p)
		if rain, ok := b.(RainObstructor); ok {
			if rain.ObstructsRain() {
				return false
			}
			continue
		}
		m := b.Model()
		if m.FaceSolid(p, cube.FaceUp, src) || m.FaceSolid(p, cube.FaceDown, src) {
			return false
		}
	}
	return src.complete
}

func lightningRodName(name string) bool {
	switch name {
	case "minecraft:lightning_rod", "minecraft:exposed_lightning_rod", "minecraft:weathered_lightning_rod", "minecraft:oxidized_lightning_rod",
		"minecraft:waxed_lightning_rod", "minecraft:waxed_exposed_lightning_rod", "minecraft:waxed_weathered_lightning_rod", "minecraft:waxed_oxidized_lightning_rod":
		return true
	}
	return false
}

type lightningRodCandidate struct {
	pos      cube.Pos
	distance uint32
}

func orderLightningRods(center cube.Pos, positions []cube.Pos) []lightningRodCandidate {
	candidates := make([]lightningRodCandidate, 0, len(positions))
	for _, pos := range positions {
		dx, dy, dz := int64(pos[0])-int64(center[0]), int64(pos[1])-int64(center[1]), int64(pos[2])-int64(center[2])
		if dy < -64 || dy > 64 || dx < -64 || dx > 64 || dz < -64 || dz > 64 || dx*dx+dz*dz > 64*64 {
			continue
		}
		candidates = append(candidates, lightningRodCandidate{pos: pos, distance: uint32(dx*dx + dy*dy + dz*dz)})
	}
	// BlockSource visits chunk X, chunk Z, subchunk Y, then packed X/Z/Y.
	// Native distance sorting preserves this order only in small partitions.
	slices.SortFunc(candidates, func(a, b lightningRodCandidate) int {
		ak := [6]int{a.pos[0] >> 4, a.pos[2] >> 4, a.pos[1] >> 4, a.pos[0] & 15, a.pos[2] & 15, a.pos[1] & 15}
		bk := [6]int{b.pos[0] >> 4, b.pos[2] >> 4, b.pos[1] >> 4, b.pos[0] & 15, b.pos[2] & 15, b.pos[1] & 15}
		for i, v := range ak {
			if v < bk[i] {
				return -1
			}
			if v > bk[i] {
				return 1
			}
		}
		return 0
	})
	sortLightningRods(candidates, len(candidates))
	return candidates
}

// sortLightningRods matches the inspected MSVC distance sort at 0x2d9d600.
// Its partition rotations affect which equidistant candidate is considered
// first. A different unstable sort or lexicographic tie-break changes selection.
func sortLightningRods(a []lightningRodCandidate, ideal int) {
	for len(a) > 32 {
		if ideal <= 0 {
			heapSortLightningRods(a)
			return
		}
		first, last := partitionLightningRods(a)
		ideal = (ideal >> 1) + (ideal >> 2)
		if first < len(a)-last {
			sortLightningRods(a[:first], ideal)
			a = a[last:]
		} else {
			sortLightningRods(a[last:], ideal)
			a = a[:first]
		}
	}
	for i := 1; i < len(a); i++ {
		v, j := a[i], i
		for j > 0 && v.distance < a[j-1].distance {
			a[j] = a[j-1]
			j--
		}
		a[j] = v
	}
}

func medianLightningRods(a []lightningRodCandidate, first, mid, last int) {
	if a[mid].distance < a[first].distance {
		a[mid], a[first] = a[first], a[mid]
	}
	if a[last].distance < a[mid].distance {
		a[last], a[mid] = a[mid], a[last]
		if a[mid].distance < a[first].distance {
			a[mid], a[first] = a[first], a[mid]
		}
	}
}

func partitionLightningRods(a []lightningRodCandidate) (int, int) {
	n := len(a)
	first, last := n/2, n/2+1
	if n-1 > 40 {
		step := n >> 3
		medianLightningRods(a, 0, step, step*2)
		medianLightningRods(a, first-step, first, first+step)
		medianLightningRods(a, n-1-step*2, n-1-step, n-1)
		medianLightningRods(a, step, first, n-1-step)
	} else {
		medianLightningRods(a, 0, first, n-1)
	}
	for first > 0 && a[first-1].distance == a[first].distance {
		first--
	}
	for last < n && a[last].distance == a[first].distance {
		last++
	}
	lo, hi := first, last
	for {
		for hi < n {
			if a[first].distance < a[hi].distance {
				hi++
				continue
			}
			if a[hi].distance < a[first].distance {
				break
			}
			if last != hi {
				a[last], a[hi] = a[hi], a[last]
			}
			last++
			hi++
		}
		for lo > 0 {
			if a[lo-1].distance < a[first].distance {
				lo--
				continue
			}
			if a[first].distance < a[lo-1].distance {
				break
			}
			first--
			if first != lo-1 {
				a[first], a[lo-1] = a[lo-1], a[first]
			}
			lo--
		}
		if lo == 0 && hi == n {
			return first, last
		}
		if lo == 0 {
			if last != hi {
				a[first], a[last] = a[last], a[first]
			}
			last++
			a[first], a[hi] = a[hi], a[first]
			first++
			hi++
		} else if hi == n {
			lo--
			first--
			if lo != first {
				a[lo], a[first] = a[first], a[lo]
			}
			last--
			a[first], a[last] = a[last], a[first]
		} else {
			lo--
			a[hi], a[lo] = a[lo], a[hi]
			hi++
		}
	}
}

func heapSortLightningRods(a []lightningRodCandidate) {
	for hole := len(a) / 2; hole > 0; {
		hole--
		adjustLightningHeap(a, hole, len(a), a[hole])
	}
	for last := len(a) - 1; last > 0; last-- {
		v := a[last]
		a[last] = a[0]
		adjustLightningHeap(a, 0, last, v)
	}
}

func adjustLightningHeap(a []lightningRodCandidate, hole, bottom int, value lightningRodCandidate) {
	top, index := hole, hole
	maxNonLeaf := (bottom - 1) >> 1
	for index < maxNonLeaf {
		index = index*2 + 2
		if a[index].distance < a[index-1].distance {
			index--
		}
		a[hole] = a[index]
		hole = index
	}
	if index == maxNonLeaf && bottom%2 == 0 {
		a[hole] = a[bottom-1]
		hole = bottom - 1
	}
	for hole > top {
		parent := (hole - 1) >> 1
		if a[parent].distance >= value.distance {
			break
		}
		a[hole] = a[parent]
		hole = parent
	}
	a[hole] = value
}
