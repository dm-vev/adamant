package overworld

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

// genWorldGenTrees is a port of net.minecraft.world.gen.feature.WorldGenTrees (Java 1.12).
func (g *Overworld) genWorldGenTrees(
	c *chunk.Chunk,
	preview map[world.ChunkPos]*chunk.Chunk,
	chunkX, chunkZ int,
	x, y, z int,
	r *mc112.Rand,
	minTreeHeight int,
	logRID, leavesRID uint32,
	vinesGrow bool,
	apply bool,
) bool {
	height := int(r.Intn(3)) + minTreeHeight
	flag := true

	if y < 1 || y+height+1 > 256 {
		return false
	}

	for yy := y; yy <= y+1+height; yy++ {
		k := 1
		if yy == y {
			k = 0
		}
		if yy >= y+1+height-2 {
			k = 2
		}
		for xx := x - k; xx <= x+k && flag; xx++ {
			for zz := z - k; zz <= z+k && flag; zz++ {
				if yy < 0 || yy >= 256 {
					flag = false
					break
				}
				rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz)
				if !g.canGrowIntoRID(rid) {
					flag = false
				}
			}
		}
	}

	if !flag {
		return false
	}

	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !(soil == g.grassRID || soil == g.dirtRID || soil == g.coarseDirtRID || soil == g.podzolRID || soil == g.farmlandRID) || y >= 256-height-1 {
		return false
	}

	g.setDirtAt(c, preview, chunkX, chunkZ, x, y-1, z, apply)

	for yy := y - 3 + height; yy <= y+height; yy++ {
		i4 := yy - (y + height)
		j1 := 1 - i4/2

		for xx := x - j1; xx <= x+j1; xx++ {
			l1 := xx - x
			for zz := z - j1; zz <= z+j1; zz++ {
				j2 := zz - z
				if absInt(l1) != j1 || absInt(j2) != j1 || (r.Intn(2) != 0 && i4 != 0) {
					rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz)
					if rid == g.airRID || g.isLeaves(rid) || g.isVinesRID(rid) {
						if apply {
							g.setRIDIfInChunk(c, chunkX, chunkZ, xx, yy, zz, leavesRID)
						}
					}
				}
			}
		}
	}

	for j3 := 0; j3 < height; j3++ {
		yy := y + j3
		rid := g.blockRIDAt(c, preview, chunkX, chunkZ, x, yy, z)
		if rid == g.airRID || g.isLeaves(rid) || g.isVinesRID(rid) {
			if apply {
				g.setRIDIfInChunk(c, chunkX, chunkZ, x, yy, z, logRID)
			}

			if vinesGrow && j3 > 0 {
				if r.Intn(3) > 0 && g.blockRIDAt(c, preview, chunkX, chunkZ, x-1, yy, z) == g.airRID {
					g.placeVines(c, chunkX, chunkZ, x-1, yy, z, cube.East, apply)
				}
				if r.Intn(3) > 0 && g.blockRIDAt(c, preview, chunkX, chunkZ, x+1, yy, z) == g.airRID {
					g.placeVines(c, chunkX, chunkZ, x+1, yy, z, cube.West, apply)
				}
				if r.Intn(3) > 0 && g.blockRIDAt(c, preview, chunkX, chunkZ, x, yy, z-1) == g.airRID {
					g.placeVines(c, chunkX, chunkZ, x, yy, z-1, cube.South, apply)
				}
				if r.Intn(3) > 0 && g.blockRIDAt(c, preview, chunkX, chunkZ, x, yy, z+1) == g.airRID {
					g.placeVines(c, chunkX, chunkZ, x, yy, z+1, cube.North, apply)
				}
			}
		}
	}

	if vinesGrow {
		for yy := y - 3 + height; yy <= y+height; yy++ {
			j4 := yy - (y + height)
			k4 := 2 - j4/2
			for xx := x - k4; xx <= x+k4; xx++ {
				for zz := z - k4; zz <= z+k4; zz++ {
					rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz)
					if !g.isLeaves(rid) {
						continue
					}

					if r.Intn(4) == 0 && g.blockRIDAt(c, preview, chunkX, chunkZ, xx-1, yy, zz) == g.airRID {
						g.addHangingVines(c, preview, chunkX, chunkZ, xx-1, yy, zz, cube.East, apply)
					}
					if r.Intn(4) == 0 && g.blockRIDAt(c, preview, chunkX, chunkZ, xx+1, yy, zz) == g.airRID {
						g.addHangingVines(c, preview, chunkX, chunkZ, xx+1, yy, zz, cube.West, apply)
					}
					if r.Intn(4) == 0 && g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz-1) == g.airRID {
						g.addHangingVines(c, preview, chunkX, chunkZ, xx, yy, zz-1, cube.South, apply)
					}
					if r.Intn(4) == 0 && g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz+1) == g.airRID {
						g.addHangingVines(c, preview, chunkX, chunkZ, xx, yy, zz+1, cube.North, apply)
					}
				}
			}
		}

		if r.Intn(5) == 0 && height > 5 {
			for l3 := 0; l3 < 2; l3++ {
				for _, facing := range []cube.Direction{cube.North, cube.East, cube.South, cube.West} {
					if r.Intn(int32(4-l3)) == 0 {
						opp := oppositeDir(facing)
						g.placeCocoa(c, chunkX, chunkZ, x+dirDX(opp), y+height-5+l3, z+dirDZ(opp), facing, int(r.Intn(3)), apply)
					}
				}
			}
		}
	}
	return true
}

// genWorldGenBirch is a port of net.minecraft.world.gen.feature.WorldGenBirchTree (Java 1.12).
func (g *Overworld) genWorldGenBirch(
	c *chunk.Chunk,
	preview map[world.ChunkPos]*chunk.Chunk,
	chunkX, chunkZ int,
	x, y, z int,
	r *mc112.Rand,
	useExtraRandomHeight bool,
	apply bool,
) bool {
	height := int(r.Intn(3)) + 5
	if useExtraRandomHeight {
		height += int(r.Intn(7))
	}
	flag := true

	if y < 1 || y+height+1 > 256 {
		return false
	}

	for yy := y; yy <= y+1+height; yy++ {
		k := 1
		if yy == y {
			k = 0
		}
		if yy >= y+1+height-2 {
			k = 2
		}
		for xx := x - k; xx <= x+k && flag; xx++ {
			for zz := z - k; zz <= z+k && flag; zz++ {
				if yy < 0 || yy >= 256 {
					flag = false
					break
				}
				rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz)
				if !g.canGrowIntoRID(rid) {
					flag = false
				}
			}
		}
	}
	if !flag {
		return false
	}

	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !(soil == g.grassRID || soil == g.dirtRID || soil == g.coarseDirtRID || soil == g.podzolRID || soil == g.farmlandRID) || y >= 256-height-1 {
		return false
	}
	g.setDirtAt(c, preview, chunkX, chunkZ, x, y-1, z, apply)

	for yy := y - 3 + height; yy <= y+height; yy++ {
		k2 := yy - (y + height)
		l2 := 1 - k2/2
		for xx := x - l2; xx <= x+l2; xx++ {
			j1 := xx - x
			for zz := z - l2; zz <= z+l2; zz++ {
				l1 := zz - z
				if absInt(j1) != l2 || absInt(l1) != l2 || (r.Intn(2) != 0 && k2 != 0) {
					rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz)
					if rid == g.airRID || g.isLeaves(rid) {
						if apply {
							g.setRIDIfInChunk(c, chunkX, chunkZ, xx, yy, zz, g.birchLeavesRID)
						}
					}
				}
			}
		}
	}

	for j2 := 0; j2 < height; j2++ {
		yy := y + j2
		rid := g.blockRIDAt(c, preview, chunkX, chunkZ, x, yy, z)
		if rid == g.airRID || g.isLeaves(rid) {
			if apply {
				g.setRIDIfInChunk(c, chunkX, chunkZ, x, yy, z, g.birchLogRID)
			}
		}
	}

	return true
}

func (g *Overworld) genWorldGenShrub(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, apply bool) bool {
	// Port of WorldGenShrub: Jungle log + oak leaves.
	yy := y
	for {
		rid := g.blockRIDAt(c, preview, chunkX, chunkZ, x, yy, z)
		if !(rid == g.airRID || g.isLeaves(rid)) || yy <= 0 {
			break
		}
		yy--
	}
	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, yy, z)
	if soil == g.dirtRID || soil == g.coarseDirtRID || soil == g.grassRID || soil == g.podzolRID {
		yy++
		if apply {
			g.setRIDIfInChunk(c, chunkX, chunkZ, x, yy, z, g.jungleLogRID)
		}
		for y2 := yy; y2 <= yy+2; y2++ {
			j := y2 - yy
			k := 2 - j
			for xx := x - k; xx <= x+k; xx++ {
				i1 := xx - x
				for zz := z - k; zz <= z+k; zz++ {
					k1 := zz - z
					if absInt(i1) != k || absInt(k1) != k || r.Intn(2) != 0 {
						rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, y2, zz)
						if rid == g.airRID || g.isLeaves(rid) {
							if apply {
								g.setRIDIfInChunk(c, chunkX, chunkZ, xx, y2, zz, g.oakLeavesRID)
							}
						}
					}
				}
			}
		}
	}
	return true
}

func (g *Overworld) genWorldGenTaiga1(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, apply bool) bool {
	i := int(r.Intn(5)) + 7
	j := i - int(r.Intn(2)) - 3
	k := i - j
	l := 1 + int(r.Intn(int32(k+1)))

	if y < 1 || y+i+1 > 256 {
		return false
	}

	flag := true
	for yy := y; yy <= y+1+i && flag; yy++ {
		j1 := 1
		if yy-y < j {
			j1 = 0
		} else {
			j1 = l
		}
		for xx := x - j1; xx <= x+j1 && flag; xx++ {
			for zz := z - j1; zz <= z+j1 && flag; zz++ {
				if yy < 0 || yy >= 256 {
					flag = false
					break
				}
				rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz)
				if !g.canGrowIntoRID(rid) {
					flag = false
				}
			}
		}
	}
	if !flag {
		return false
	}

	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !(soil == g.grassRID || soil == g.dirtRID || soil == g.coarseDirtRID || soil == g.podzolRID) || y >= 256-i-1 {
		return false
	}

	g.setDirtAt(c, preview, chunkX, chunkZ, x, y-1, z, apply)

	k2 := 0
	for yy := y + i; yy >= y+j; yy-- {
		for xx := x - k2; xx <= x+k2; xx++ {
			k3 := xx - x
			for zz := z - k2; zz <= z+k2; zz++ {
				j2 := zz - z
				if absInt(k3) != k2 || absInt(j2) != k2 || k2 <= 0 {
					rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz)
					if !g.isFullBlockRID(rid) {
						if apply {
							g.setRIDIfInChunk(c, chunkX, chunkZ, xx, yy, zz, g.spruceLeavesRID)
						}
					}
				}
			}
		}
		if k2 >= 1 && yy == y+j+1 {
			k2--
		} else if k2 < l {
			k2++
		}
	}

	for i3 := 0; i3 < i-1; i3++ {
		yy := y + i3
		rid := g.blockRIDAt(c, preview, chunkX, chunkZ, x, yy, z)
		if rid == g.airRID || g.isLeaves(rid) {
			if apply {
				g.setRIDIfInChunk(c, chunkX, chunkZ, x, yy, z, g.spruceLogRID)
			}
		}
	}
	return true
}

func (g *Overworld) genWorldGenTaiga2(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, apply bool) bool {
	i := int(r.Intn(4)) + 6
	j := 1 + int(r.Intn(2))
	k := i - j
	l := 2 + int(r.Intn(2))
	flag := true

	if y < 1 || y+i+1 > 256 {
		return false
	}

	for yy := y; yy <= y+1+i && flag; yy++ {
		j1 := 0
		if yy-y < j {
			j1 = 0
		} else {
			j1 = l
		}
		for xx := x - j1; xx <= x+j1 && flag; xx++ {
			for zz := z - j1; zz <= z+j1 && flag; zz++ {
				if yy < 0 || yy >= 256 {
					flag = false
					break
				}
				rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz)
				if rid != g.airRID && !g.isLeaves(rid) {
					flag = false
				}
			}
		}
	}
	if !flag {
		return false
	}

	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !(soil == g.grassRID || soil == g.dirtRID || soil == g.coarseDirtRID || soil == g.podzolRID || soil == g.farmlandRID) || y >= 256-i-1 {
		return false
	}
	g.setDirtAt(c, preview, chunkX, chunkZ, x, y-1, z, apply)

	i3 := int(r.Intn(2))
	j3 := 1
	k3 := 0
	for l3 := 0; l3 <= k; l3++ {
		j4 := y + i - l3
		for xx := x - i3; xx <= x+i3; xx++ {
			j2 := xx - x
			for zz := z - i3; zz <= z+i3; zz++ {
				l2 := zz - z
				if absInt(j2) != i3 || absInt(l2) != i3 || i3 <= 0 {
					rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, j4, zz)
					if !g.isFullBlockRID(rid) {
						if apply {
							g.setRIDIfInChunk(c, chunkX, chunkZ, xx, j4, zz, g.spruceLeavesRID)
						}
					}
				}
			}
		}

		if i3 >= j3 {
			i3 = k3
			k3 = 1
			j3++
			if j3 > l {
				j3 = l
			}
		} else {
			i3++
		}
	}

	i4 := int(r.Intn(3))
	for k4 := 0; k4 < i-i4; k4++ {
		yy := y + k4
		rid := g.blockRIDAt(c, preview, chunkX, chunkZ, x, yy, z)
		if rid == g.airRID || g.isLeaves(rid) {
			if apply {
				g.setRIDIfInChunk(c, chunkX, chunkZ, x, yy, z, g.spruceLogRID)
			}
		}
	}
	return true
}

type hugeTreeSpec struct {
	baseHeight       int
	extraRandomHeight int
	woodRID          uint32
	leavesRID        uint32
}

func (g *Overworld) hugeTreeHeight(r *mc112.Rand, spec hugeTreeSpec) int {
	h := int(r.Intn(3)) + spec.baseHeight
	if spec.extraRandomHeight > 1 {
		h += int(r.Intn(int32(spec.extraRandomHeight)))
	}
	return h
}

func (g *Overworld) hugeTreeIsSpaceAt(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, height int) bool {
	if y < 1 || y+height+1 > 256 {
		return false
	}
	for i := 0; i <= 1+height; i++ {
		j := 2
		if i == 0 {
			j = 1
		} else if i >= 1+height-2 {
			j = 2
		}
		for dx := -j; dx <= j; dx++ {
			for dz := -j; dz <= j; dz++ {
				yy := y + i
				if yy < 0 || yy >= 256 {
					return false
				}
				rid := g.blockRIDAt(c, preview, chunkX, chunkZ, x+dx, yy, z+dz)
				if !g.canGrowIntoRID(rid) {
					return false
				}
			}
		}
	}
	return true
}

func (g *Overworld) hugeTreeEnsureDirtsUnderneath(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, apply bool) bool {
	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !(soil == g.grassRID || soil == g.dirtRID || soil == g.coarseDirtRID || soil == g.podzolRID) || y < 2 {
		return false
	}
	g.setDirtAt(c, preview, chunkX, chunkZ, x, y-1, z, apply)
	g.setDirtAt(c, preview, chunkX, chunkZ, x+1, y-1, z, apply)
	g.setDirtAt(c, preview, chunkX, chunkZ, x, y-1, z+1, apply)
	g.setDirtAt(c, preview, chunkX, chunkZ, x+1, y-1, z+1, apply)
	return true
}

func (g *Overworld) hugeTreeEnsureGrowable(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, height int, apply bool) bool {
	return g.hugeTreeIsSpaceAt(c, preview, chunkX, chunkZ, x, y, z, height) && g.hugeTreeEnsureDirtsUnderneath(c, preview, chunkX, chunkZ, x, y, z, apply)
}

func (g *Overworld) hugeTreeGrowLeavesLayerStrict(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, centerX, centerY, centerZ int, width int, leavesRID uint32, apply bool) {
	i := width * width
	for dx := -width; dx <= width+1; dx++ {
		for dz := -width; dz <= width+1; dz++ {
			l := dx - 1
			i1 := dz - 1
			if dx*dx+dz*dz <= i || l*l+i1*i1 <= i || dx*dx+i1*i1 <= i || l*l+dz*dz <= i {
				rid := g.blockRIDAt(c, preview, chunkX, chunkZ, centerX+dx, centerY, centerZ+dz)
				if rid == g.airRID || g.isLeaves(rid) {
					if apply {
						g.setRIDIfInChunk(c, chunkX, chunkZ, centerX+dx, centerY, centerZ+dz, leavesRID)
					}
				}
			}
		}
	}
}

func (g *Overworld) hugeTreeGrowLeavesLayer(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, centerX, centerY, centerZ int, width int, leavesRID uint32, apply bool) {
	i := width * width
	for dx := -width; dx <= width; dx++ {
		for dz := -width; dz <= width; dz++ {
			if dx*dx+dz*dz <= i {
				rid := g.blockRIDAt(c, preview, chunkX, chunkZ, centerX+dx, centerY, centerZ+dz)
				if rid == g.airRID || g.isLeaves(rid) {
					if apply {
						g.setRIDIfInChunk(c, chunkX, chunkZ, centerX+dx, centerY, centerZ+dz, leavesRID)
					}
				}
			}
		}
	}
}

func (g *Overworld) genWorldGenMegaPine(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, useBaseHeight bool, apply bool) bool {
	// WorldGenMegaPineTree (and huge-tree base).
	spec := hugeTreeSpec{
		baseHeight:       13,
		extraRandomHeight: 15,
		woodRID:          g.spruceLogRID,
		leavesRID:        g.spruceLeavesRID,
	}

	height := g.hugeTreeHeight(r, spec)
	if !g.hugeTreeEnsureGrowable(c, preview, chunkX, chunkZ, x, y, z, height, apply) {
		return false
	}

	// createCrown
	i := int(r.Intn(5))
	if useBaseHeight {
		i += spec.baseHeight
	} else {
		i += 3
	}
	j := 0
	topY := y + height
	for yy := topY - i; yy <= topY; yy++ {
		l := topY - yy
		i1 := int(math.Floor(float64(l)/float64(i)*3.5)) // MathHelper.floor
		add := 0
		if l > 0 && i1 == j && (yy&1) == 0 {
			add = 1
		}
		g.hugeTreeGrowLeavesLayerStrict(c, preview, chunkX, chunkZ, x, yy, z, i1+add, spec.leavesRID, apply)
		j = i1
	}

	// trunk 2x2
	for j2 := 0; j2 < height; j2++ {
		yy := y + j2
		for _, off := range [][2]int{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
			xx, zz := x+off[0], z+off[1]
			rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz)
			if rid == g.airRID || g.isLeaves(rid) {
				if apply {
					g.setRIDIfInChunk(c, chunkX, chunkZ, xx, yy, zz, spec.woodRID)
				}
			}
		}
	}

	// generateSaplings: podzol circles.
	g.placeMegaPinePodzol(c, preview, chunkX, chunkZ, x, y, z, r, apply)
	return true
}

func (g *Overworld) placeMegaPinePodzol(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, apply bool) {
	// Port of WorldGenMegaPineTree.generateSaplings.
	g.placePodzolCircle(c, preview, chunkX, chunkZ, x-1, y, z-1, apply)
	g.placePodzolCircle(c, preview, chunkX, chunkZ, x+2, y, z-1, apply)
	g.placePodzolCircle(c, preview, chunkX, chunkZ, x-1, y, z+2, apply)
	g.placePodzolCircle(c, preview, chunkX, chunkZ, x+2, y, z+2, apply)

	for i := 0; i < 5; i++ {
		j := int(r.Intn(64))
		k := j % 8
		l := j / 8
		if k == 0 || k == 7 || l == 0 || l == 7 {
			g.placePodzolCircle(c, preview, chunkX, chunkZ, x-3+k, y, z-3+l, apply)
		}
	}
}

func (g *Overworld) placePodzolCircle(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, centerX, centerY, centerZ int, apply bool) {
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if absInt(dx) != 2 || absInt(dz) != 2 {
				g.placePodzolAt(c, preview, chunkX, chunkZ, centerX+dx, centerY, centerZ+dz, apply)
			}
		}
	}
}

func (g *Overworld) placePodzolAt(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, apply bool) {
	for dy := 2; dy >= -3; dy-- {
		yy := y + dy
		rid := g.blockRIDAt(c, preview, chunkX, chunkZ, x, yy, z)
		if rid == g.grassRID || rid == g.dirtRID || rid == g.coarseDirtRID {
			if apply {
				g.setRIDIfInChunk(c, chunkX, chunkZ, x, yy, z, g.podzolRID)
			}
			break
		}
		if rid != g.airRID && dy < 0 {
			break
		}
	}
}

func (g *Overworld) genWorldGenCanopy(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, apply bool) bool {
	// WorldGenCanopyTree.
	height := int(r.Intn(3)) + int(r.Intn(2)) + 6
	if y < 1 || y+height+1 >= 256 {
		return false
	}

	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !(soil == g.grassRID || soil == g.dirtRID || soil == g.coarseDirtRID || soil == g.podzolRID) {
		return false
	}

	if !g.canopyPlaceTreeOfHeight(c, preview, chunkX, chunkZ, x, y, z, height) {
		return false
	}

	// Dirt 2x2 under trunk.
	g.setDirtAt(c, preview, chunkX, chunkZ, x, y-1, z, apply)
	g.setDirtAt(c, preview, chunkX, chunkZ, x+1, y-1, z, apply)
	g.setDirtAt(c, preview, chunkX, chunkZ, x, y-1, z+1, apply)
	g.setDirtAt(c, preview, chunkX, chunkZ, x+1, y-1, z+1, apply)

	facing := randomHorizontalDirection(r)
	i1 := height - int(r.Intn(4))
	j1 := 2 - int(r.Intn(3))
	k1, l1 := x, z
	i2 := y + height - 1

	for j2 := 0; j2 < height; j2++ {
		if j2 >= i1 && j1 > 0 {
			k1 += dirDX(facing)
			l1 += dirDZ(facing)
			j1--
		}
		yy := y + j2
		rid := g.blockRIDAt(c, preview, chunkX, chunkZ, k1, yy, l1)
		if rid == g.airRID || g.isLeaves(rid) {
			// 2x2 logs.
			for _, off := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
				if apply {
					g.setRIDIfInChunk(c, chunkX, chunkZ, k1+off[0], yy, l1+off[1], g.darkOakLogRID)
				}
			}
		}
	}

	for i3 := -2; i3 <= 0; i3++ {
		for l3 := -2; l3 <= 0; l3++ {
			k4 := -1
			g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, k1+i3, i2+k4, l1+l3, apply)
			g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, 1+k1-i3, i2+k4, l1+l3, apply)
			g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, k1+i3, i2+k4, 1+l1-l3, apply)
			g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, 1+k1-i3, i2+k4, 1+l1-l3, apply)
			if (i3 > -2 || l3 > -1) && (i3 != -1 || l3 != -2) {
				k4 = 1
				g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, k1+i3, i2+k4, l1+l3, apply)
				g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, 1+k1-i3, i2+k4, l1+l3, apply)
				g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, k1+i3, i2+k4, 1+l1-l3, apply)
				g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, 1+k1-i3, i2+k4, 1+l1-l3, apply)
			}
		}
	}

	if r.Bool() {
		g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, k1, i2+2, l1, apply)
		g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, k1+1, i2+2, l1, apply)
		g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, k1+1, i2+2, l1+1, apply)
		g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, k1, i2+2, l1+1, apply)
	}

	for j3 := -3; j3 <= 4; j3++ {
		for i4 := -3; i4 <= 4; i4++ {
			if (j3 != -3 || i4 != -3) && (j3 != -3 || i4 != 4) && (j3 != 4 || i4 != -3) && (j3 != 4 || i4 != 4) && (absInt(j3) < 3 || absInt(i4) < 3) {
				g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, k1+j3, i2, l1+i4, apply)
			}
		}
	}

	for k3 := -1; k3 <= 2; k3++ {
		for j4 := -1; j4 <= 2; j4++ {
			if (k3 < 0 || k3 > 1 || j4 < 0 || j4 > 1) && r.Intn(3) <= 0 {
				l4 := int(r.Intn(3)) + 2
				for i5 := 0; i5 < l4; i5++ {
					if apply {
						g.setRIDIfInChunk(c, chunkX, chunkZ, x+k3, i2-i5-1, z+j4, g.darkOakLogRID)
					}
				}
				for j5 := -1; j5 <= 1; j5++ {
					for l2 := -1; l2 <= 1; l2++ {
						g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, k1+k3+j5, i2, l1+j4+l2, apply)
					}
				}
				for k5 := -2; k5 <= 2; k5++ {
					for l5 := -2; l5 <= 2; l5++ {
						if absInt(k5) != 2 || absInt(l5) != 2 {
							g.canopyPlaceLeafAt(c, preview, chunkX, chunkZ, k1+k3+k5, i2-1, l1+j4+l5, apply)
						}
					}
				}
			}
		}
	}

	return true
}

func (g *Overworld) canopyPlaceTreeOfHeight(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, height int) bool {
	for l := 0; l <= height+1; l++ {
		i1 := 1
		if l == 0 {
			i1 = 0
		}
		if l >= height-1 {
			i1 = 2
		}
		for dx := -i1; dx <= i1; dx++ {
			for dz := -i1; dz <= i1; dz++ {
				rid := g.blockRIDAt(c, preview, chunkX, chunkZ, x+dx, y+l, z+dz)
				if !g.canGrowIntoRID(rid) {
					return false
				}
			}
		}
	}
	return true
}

func (g *Overworld) canopyPlaceLeafAt(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, apply bool) {
	rid := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z)
	if rid != g.airRID {
		return
	}
	if apply {
		g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, g.darkOakLeavesRID)
	}
}

func (g *Overworld) genWorldGenSavanna(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, apply bool) bool {
	// WorldGenSavannaTree.
	i := int(r.Intn(3)) + int(r.Intn(3)) + 5
	flag := true
	if y < 1 || y+i+1 > 256 {
		return false
	}

	for yy := y; yy <= y+1+i; yy++ {
		k := 1
		if yy == y {
			k = 0
		}
		if yy >= y+1+i-2 {
			k = 2
		}
		for xx := x - k; xx <= x+k && flag; xx++ {
			for zz := z - k; zz <= z+k && flag; zz++ {
				if yy < 0 || yy >= 256 {
					flag = false
					break
				}
				rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz)
				if !g.canGrowIntoRID(rid) {
					flag = false
				}
			}
		}
	}
	if !flag {
		return false
	}

	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !(soil == g.grassRID || soil == g.dirtRID || soil == g.coarseDirtRID || soil == g.podzolRID) || y >= 256-i-1 {
		return false
	}
	g.setDirtAt(c, preview, chunkX, chunkZ, x, y-1, z, apply)

	facing := randomHorizontalDirection(r)
	k2 := i - int(r.Intn(4)) - 1
	l2 := 3 - int(r.Intn(3))
	i3, j1 := x, z
	k1 := 0

	for l1 := 0; l1 < i; l1++ {
		yy := y + l1
		if l1 >= k2 && l2 > 0 {
			i3 += dirDX(facing)
			j1 += dirDZ(facing)
			l2--
		}
		rid := g.blockRIDAt(c, preview, chunkX, chunkZ, i3, yy, j1)
		if rid == g.airRID || g.isLeaves(rid) {
			if apply {
				g.setRIDIfInChunk(c, chunkX, chunkZ, i3, yy, j1, g.acaciaLogRID)
			}
			k1 = yy
		}
	}

	// leaf canopy around top.
	for dx := -3; dx <= 3; dx++ {
		for dz := -3; dz <= 3; dz++ {
			if absInt(dx) != 3 || absInt(dz) != 3 {
				g.savannaPlaceLeafAt(c, preview, chunkX, chunkZ, i3+dx, k1, j1+dz, apply)
			}
		}
	}

	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			g.savannaPlaceLeafAt(c, preview, chunkX, chunkZ, i3+dx, k1+1, j1+dz, apply)
		}
	}
	g.savannaPlaceLeafAt(c, preview, chunkX, chunkZ, i3+2, k1+1, j1, apply)
	g.savannaPlaceLeafAt(c, preview, chunkX, chunkZ, i3-2, k1+1, j1, apply)
	g.savannaPlaceLeafAt(c, preview, chunkX, chunkZ, i3, k1+1, j1+2, apply)
	g.savannaPlaceLeafAt(c, preview, chunkX, chunkZ, i3, k1+1, j1-2, apply)

	i3, j1 = x, z
	facing2 := randomHorizontalDirection(r)
	if facing2 != facing {
		l3 := k2 - int(r.Intn(2)) - 1
		k4 := 1 + int(r.Intn(3))
		k1 = 0
		for l4 := l3; l4 < i && k4 > 0; k4-- {
			if l4 >= 1 {
				yy := y + l4
				i3 += dirDX(facing2)
				j1 += dirDZ(facing2)
				rid := g.blockRIDAt(c, preview, chunkX, chunkZ, i3, yy, j1)
				if rid == g.airRID || g.isLeaves(rid) {
					if apply {
						g.setRIDIfInChunk(c, chunkX, chunkZ, i3, yy, j1, g.acaciaLogRID)
					}
					k1 = yy
				}
			}
			l4++
		}

		if k1 > 0 {
			for dx := -2; dx <= 2; dx++ {
				for dz := -2; dz <= 2; dz++ {
					if absInt(dx) != 2 || absInt(dz) != 2 {
						g.savannaPlaceLeafAt(c, preview, chunkX, chunkZ, i3+dx, k1, j1+dz, apply)
					}
				}
			}

			for dx := -1; dx <= 1; dx++ {
				for dz := -1; dz <= 1; dz++ {
					g.savannaPlaceLeafAt(c, preview, chunkX, chunkZ, i3+dx, k1+1, j1+dz, apply)
				}
			}
		}
	}

	return true
}

func (g *Overworld) savannaPlaceLeafAt(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, apply bool) {
	rid := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z)
	if rid == g.airRID || g.isLeaves(rid) {
		if apply {
			g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, g.acaciaLeavesRID)
		}
	}
}

func (g *Overworld) genWorldGenMegaJungle(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, apply bool) bool {
	// WorldGenMegaJungle.
	spec := hugeTreeSpec{
		baseHeight:       10,
		extraRandomHeight: 20,
		woodRID:          g.jungleLogRID,
		leavesRID:        g.jungleLeavesRID,
	}
	height := g.hugeTreeHeight(r, spec)
	if !g.hugeTreeEnsureGrowable(c, preview, chunkX, chunkZ, x, y, z, height, apply) {
		return false
	}

	// createCrown
	for j := -2; j <= 0; j++ {
		g.hugeTreeGrowLeavesLayerStrict(c, preview, chunkX, chunkZ, x, y+height+j, z, 2+1-j, spec.leavesRID, apply)
	}

	for j := y + height - 2 - int(r.Intn(4)); j > y+height/2; j -= 2 + int(r.Intn(4)) {
		f := float64(r.Float32()) * (math.Pi * 2)
		k := x + int(float64(0.5)+math.Cos(f)*4.0)
		l := z + int(float64(0.5)+math.Sin(f)*4.0)

		for i1 := 0; i1 < 5; i1++ {
			k = x + int(float64(1.5)+math.Cos(f)*float64(i1))
			l = z + int(float64(1.5)+math.Sin(f)*float64(i1))
			if apply {
				g.setRIDIfInChunk(c, chunkX, chunkZ, k, j-3+i1/2, l, spec.woodRID)
			}
		}

		j2 := 1 + int(r.Intn(2))
		j1 := j
		for k1 := j - j2; k1 <= j1; k1++ {
			l1 := k1 - j1
			g.hugeTreeGrowLeavesLayer(c, preview, chunkX, chunkZ, k, k1, l, 1-l1, spec.leavesRID, apply)
		}
	}

	for i2 := 0; i2 < height; i2++ {
		yy := y + i2
		// 2x2 trunk.
		base := [][2]int{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
		for _, off := range base {
			xx, zz := x+off[0], z+off[1]
			rid := g.blockRIDAt(c, preview, chunkX, chunkZ, xx, yy, zz)
			if g.canGrowIntoRID(rid) {
				if apply {
					g.setRIDIfInChunk(c, chunkX, chunkZ, xx, yy, zz, spec.woodRID)
				}
				if i2 > 0 {
					switch off {
					case [2]int{0, 0}:
						g.megaJunglePlaceVine(c, preview, chunkX, chunkZ, xx-1, yy, zz, cube.East, r, apply)
						g.megaJunglePlaceVine(c, preview, chunkX, chunkZ, xx, yy, zz-1, cube.South, r, apply)
					case [2]int{1, 0}:
						g.megaJunglePlaceVine(c, preview, chunkX, chunkZ, xx+1, yy, zz, cube.West, r, apply)
						g.megaJunglePlaceVine(c, preview, chunkX, chunkZ, xx, yy, zz-1, cube.South, r, apply)
					case [2]int{1, 1}:
						g.megaJunglePlaceVine(c, preview, chunkX, chunkZ, xx+1, yy, zz, cube.West, r, apply)
						g.megaJunglePlaceVine(c, preview, chunkX, chunkZ, xx, yy, zz+1, cube.North, r, apply)
					case [2]int{0, 1}:
						g.megaJunglePlaceVine(c, preview, chunkX, chunkZ, xx-1, yy, zz, cube.East, r, apply)
						g.megaJunglePlaceVine(c, preview, chunkX, chunkZ, xx, yy, zz+1, cube.North, r, apply)
					}
				}
			}
		}
	}

	return true
}

func (g *Overworld) megaJunglePlaceVine(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, attach cube.Direction, r *mc112.Rand, apply bool) {
	if r.Intn(3) > 0 && g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) == g.airRID {
		g.placeVines(c, chunkX, chunkZ, x, y, z, attach, apply)
	}
}

func (g *Overworld) placeVines(c *chunk.Chunk, chunkX, chunkZ int, x, y, z int, attach cube.Direction, apply bool) {
	if !apply {
		return
	}
	v := block.Vines{}.WithAttachment(attach, true)
	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, world.BlockRuntimeID(v))
}

func (g *Overworld) addHangingVines(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, attach cube.Direction, apply bool) {
	g.placeVines(c, chunkX, chunkZ, x, y, z, attach, apply)
	i := 4
	for yy := y - 1; yy >= 0 && i > 0; yy-- {
		if g.blockRIDAt(c, preview, chunkX, chunkZ, x, yy, z) != g.airRID {
			break
		}
		g.placeVines(c, chunkX, chunkZ, x, yy, z, attach, apply)
		i--
	}
}

func (g *Overworld) placeCocoa(c *chunk.Chunk, chunkX, chunkZ int, x, y, z int, facing cube.Direction, age int, apply bool) {
	if !apply {
		return
	}
	if age < 0 {
		age = 0
	}
	if age > 2 {
		age = 2
	}
	cocoaRID := world.BlockRuntimeID(block.CocoaBean{Facing: facing, Age: age})
	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, cocoaRID)
}

func (g *Overworld) isVinesRID(rid uint32) bool {
	bl, ok := world.BlockByRuntimeID(rid)
	if !ok {
		return false
	}
	_, ok = bl.(block.Vines)
	return ok
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func randomHorizontalDirection(r *mc112.Rand) cube.Direction {
	switch r.Intn(4) {
	case 0:
		return cube.South
	case 1:
		return cube.West
	case 2:
		return cube.North
	default:
		return cube.East
	}
}

func oppositeDir(d cube.Direction) cube.Direction {
	switch d {
	case cube.North:
		return cube.South
	case cube.South:
		return cube.North
	case cube.East:
		return cube.West
	case cube.West:
		return cube.East
	default:
		return d
	}
}

func dirDX(d cube.Direction) int {
	switch d {
	case cube.East:
		return 1
	case cube.West:
		return -1
	default:
		return 0
	}
}

func dirDZ(d cube.Direction) int {
	switch d {
	case cube.South:
		return 1
	case cube.North:
		return -1
	default:
		return 0
	}
}
