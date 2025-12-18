package overworld

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

// worldGenBigTree is a port of net.minecraft.world.gen.feature.WorldGenBigTree (Java 1.12).
type worldGenBigTree struct {
	g       *Overworld
	c       *chunk.Chunk
	preview map[world.ChunkPos]*chunk.Chunk
	chunkX  int
	chunkZ  int
	apply   bool

	rand    *mc112.Rand
	basePos cube.Pos

	heightLimit        int
	height             int
	heightAttenuation  float64
	branchSlope        float64
	scaleWidth         float64
	leafDensity        float64
	trunkSize          int
	heightLimitLimit   int
	leafDistanceLimit  int
	foliageCoordinates []foliageCoordinate
}

type foliageCoordinate struct {
	pos       cube.Pos
	branchBase int
}

func (w *worldGenBigTree) generate(x, y, z int, r *mc112.Rand) bool {
	w.basePos = cube.Pos{x, y, z}
	// Java uses new Random(rand.nextLong()).
	w.rand = mc112.NewRand(r.Long())

	if w.heightLimit == 0 {
		w.heightLimit = 5 + int(w.rand.Intn(int32(w.heightLimitLimit)))
	}
	if !w.validTreeLocation() {
		return false
	}
	w.generateLeafNodeList()
	w.generateLeaves()
	w.generateTrunk()
	w.generateLeafNodeBases()
	return true
}

func (w *worldGenBigTree) validTreeLocation() bool {
	soil := w.g.blockRIDAt(w.c, w.preview, w.chunkX, w.chunkZ, w.basePos[0], w.basePos[1]-1, w.basePos[2])
	if !(soil == w.g.dirtRID || soil == w.g.coarseDirtRID || soil == w.g.grassRID || soil == w.g.podzolRID || soil == w.g.farmlandRID) {
		return false
	}
	i := w.checkBlockLine(w.basePos, cube.Pos{w.basePos[0], w.basePos[1] + w.heightLimit - 1, w.basePos[2]})
	if i == -1 {
		return true
	}
	if i < 6 {
		return false
	}
	w.heightLimit = i
	return true
}

func (w *worldGenBigTree) generateLeafNodeList() {
	w.height = int(float64(w.heightLimit) * w.heightAttenuation)
	if w.height >= w.heightLimit {
		w.height = w.heightLimit - 1
	}

	i := int(1.382 + math.Pow(w.leafDensity*float64(w.heightLimit)/13.0, 2.0))
	if i < 1 {
		i = 1
	}

	j := w.basePos[1] + w.height
	k := w.heightLimit - w.leafDistanceLimit

	w.foliageCoordinates = w.foliageCoordinates[:0]
	w.foliageCoordinates = append(w.foliageCoordinates, foliageCoordinate{
		pos:       cube.Pos{w.basePos[0], w.basePos[1] + k, w.basePos[2]},
		branchBase: j,
	})

	for ; k >= 0; k-- {
		f := w.layerSize(k)
		if f < 0.0 {
			continue
		}
		for l := 0; l < i; l++ {
			d0 := w.scaleWidth * float64(f) * (float64(w.rand.Float32()) + 0.328)
			d1 := float64(w.rand.Float32()*2.0) * math.Pi
			d2 := d0*math.Sin(d1) + 0.5
			d3 := d0*math.Cos(d1) + 0.5
			blockpos := cube.Pos{w.basePos[0] + floorToInt(d2), w.basePos[1] + (k - 1), w.basePos[2] + floorToInt(d3)}
			blockpos1 := cube.Pos{blockpos[0], blockpos[1] + w.leafDistanceLimit, blockpos[2]}

			if w.checkBlockLine(blockpos, blockpos1) == -1 {
				i1 := w.basePos[0] - blockpos[0]
				j1 := w.basePos[2] - blockpos[2]
				d4 := float64(blockpos[1]) - math.Sqrt(float64(i1*i1+j1*j1))*w.branchSlope
				k1 := int(d4)
				if d4 > float64(j) {
					k1 = j
				}
				blockpos2 := cube.Pos{w.basePos[0], k1, w.basePos[2]}

				if w.checkBlockLine(blockpos2, blockpos) == -1 {
					w.foliageCoordinates = append(w.foliageCoordinates, foliageCoordinate{pos: blockpos, branchBase: blockpos2[1]})
				}
			}
		}
	}
}

func (w *worldGenBigTree) layerSize(y int) float32 {
	if float32(y) < float32(w.heightLimit)*0.3 {
		return -1.0
	}
	f := float32(w.heightLimit) / 2.0
	f1 := f - float32(y)
	f2 := float32(math.Sqrt(float64(f*f - f1*f1)))
	if f1 == 0 {
		f2 = f
	} else if absFloat32(f1) >= f {
		return 0.0
	}
	return f2 * 0.5
}

func absFloat32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

func (w *worldGenBigTree) leafSize(y int) float32 {
	if y >= 0 && y < w.leafDistanceLimit {
		if y != 0 && y != w.leafDistanceLimit-1 {
			return 3.0
		}
		return 2.0
	}
	return -1.0
}

func (w *worldGenBigTree) generateLeaves() {
	for _, fc := range w.foliageCoordinates {
		w.generateLeafNode(fc.pos)
	}
}

func (w *worldGenBigTree) generateLeafNode(pos cube.Pos) {
	for i := 0; i < w.leafDistanceLimit; i++ {
		w.crossSection(cube.Pos{pos[0], pos[1] + i, pos[2]}, w.leafSize(i), w.g.oakLeavesRID)
	}
}

func (w *worldGenBigTree) crossSection(pos cube.Pos, size float32, leavesRID uint32) {
	i := int(float64(size) + 0.618)
	for dx := -i; dx <= i; dx++ {
		for dz := -i; dz <= i; dz++ {
			if math.Pow(float64(absInt(dx))+0.5, 2.0)+math.Pow(float64(absInt(dz))+0.5, 2.0) <= float64(size*size) {
				wx, wy, wz := pos[0]+dx, pos[1], pos[2]+dz
				rid := w.g.blockRIDAt(w.c, w.preview, w.chunkX, w.chunkZ, wx, wy, wz)
				if rid == w.g.airRID || w.g.isLeaves(rid) {
					if w.apply {
						w.g.setRIDIfInChunk(w.c, w.chunkX, w.chunkZ, wx, wy, wz, leavesRID)
					}
				}
			}
		}
	}
}

func (w *worldGenBigTree) generateTrunk() {
	blockpos := w.basePos
	blockpos1 := cube.Pos{w.basePos[0], w.basePos[1] + w.height, w.basePos[2]}
	w.limb(blockpos, blockpos1)
	if w.trunkSize == 2 {
		w.limb(cube.Pos{blockpos[0] + 1, blockpos[1], blockpos[2]}, cube.Pos{blockpos1[0] + 1, blockpos1[1], blockpos1[2]})
		w.limb(cube.Pos{blockpos[0] + 1, blockpos[1], blockpos[2] + 1}, cube.Pos{blockpos1[0] + 1, blockpos1[1], blockpos1[2] + 1})
		w.limb(cube.Pos{blockpos[0], blockpos[1], blockpos[2] + 1}, cube.Pos{blockpos1[0], blockpos1[1], blockpos1[2] + 1})
	}
}

func (w *worldGenBigTree) leafNodeNeedsBase(height int) bool {
	return float64(height) >= float64(w.heightLimit)*0.2
}

func (w *worldGenBigTree) generateLeafNodeBases() {
	for _, fc := range w.foliageCoordinates {
		i := fc.branchBase
		blockpos := cube.Pos{w.basePos[0], i, w.basePos[2]}
		if blockpos != fc.pos && w.leafNodeNeedsBase(i-w.basePos[1]) {
			w.limb(blockpos, fc.pos)
		}
	}
}

func (w *worldGenBigTree) limb(from, to cube.Pos) {
	dx, dy, dz := to[0]-from[0], to[1]-from[1], to[2]-from[2]
	i := greatestDistance(dx, dy, dz)
	if i == 0 {
		return
	}
	fx := float64(dx) / float64(i)
	fy := float64(dy) / float64(i)
	fz := float64(dz) / float64(i)
	for j := 0; j <= i; j++ {
		wx := from[0] + floorToInt(0.5+float64(j)*fx)
		wy := from[1] + floorToInt(0.5+float64(j)*fy)
		wz := from[2] + floorToInt(0.5+float64(j)*fz)
		axis := logAxis(from, cube.Pos{wx, wy, wz})
		if w.apply {
			w.g.setRIDIfInChunk(w.c, w.chunkX, w.chunkZ, wx, wy, wz, world.BlockRuntimeID(block.Log{Wood: block.OakWood(), Axis: axis}))
		}
	}
}

func greatestDistance(dx, dy, dz int) int {
	i := absInt(dx)
	j := absInt(dy)
	k := absInt(dz)
	if k > i && k > j {
		return k
	}
	if j > i {
		return j
	}
	return i
}

func logAxis(from, to cube.Pos) cube.Axis {
	axis := cube.Y
	i := absInt(to[0] - from[0])
	j := absInt(to[2] - from[2])
	k := i
	if j > k {
		k = j
	}
	if k > 0 {
		if i == k {
			axis = cube.X
		} else if j == k {
			axis = cube.Z
		}
	}
	return axis
}

func (w *worldGenBigTree) checkBlockLine(from, to cube.Pos) int {
	dx, dy, dz := to[0]-from[0], to[1]-from[1], to[2]-from[2]
	i := greatestDistance(dx, dy, dz)
	if i == 0 {
		return -1
	}
	fx := float64(dx) / float64(i)
	fy := float64(dy) / float64(i)
	fz := float64(dz) / float64(i)
	for j := 0; j <= i; j++ {
		wx := from[0] + floorToInt(0.5+float64(j)*fx)
		wy := from[1] + floorToInt(0.5+float64(j)*fy)
		wz := from[2] + floorToInt(0.5+float64(j)*fz)
		rid := w.g.blockRIDAt(w.c, w.preview, w.chunkX, w.chunkZ, wx, wy, wz)
		if !w.g.canGrowIntoRID(rid) {
			return j
		}
	}
	return -1
}

