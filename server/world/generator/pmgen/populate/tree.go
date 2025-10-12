package populate

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/pmgen/rand"
)

type Tree struct {
	BaseAmount int
	Type       TreeType
}

func (t Tree) Populate(w *world.World, pos world.ChunkPos, _ *chunk.Chunk, r *rand.Random) {
	amount := r.Int31n(2) + int32(t.BaseAmount)
	<-w.Exec(func(tx *world.Tx) {
		for i := int32(0); i < amount; i++ {
			x := int(r.Range(pos[0]*16, pos[0]*16+15))
			z := int(r.Range(pos[1]*16, pos[1]*16+15))
			if y, ok := t.highestWorkableBlock(tx, x, z); ok {
				treeType := t.Type
				if birch, ok := treeType.(BirchTree); ok && r.Int31n(39) == 0 {
					birch.Super = true
					treeType = birch
				}
				treeType.Grow(tx, cube.Pos{x, y, z}, r)
			}
		}
	})
}

func (t Tree) highestWorkableBlock(tx *world.Tx, x, z int) (int, bool) {
	for y := 127; y >= 0; y-- {
		b := tx.Block(cube.Pos{x, y - 1, z})
		if (b == block.Dirt{} || b == block.Grass{}) {
			return y, true
		} else if (b != block.Air{}) {
			return 0, false
		}
	}
	return 0, false
}

var overridable = map[world.Block]struct{}{
	block.Air{}:    {},
	block.Leaves{}: {},
}

type TreeType interface {
	Grow(tx *world.Tx, pos cube.Pos, r *rand.Random)
}

type SpruceTree struct{}

func (SpruceTree) Grow(tx *world.Tx, pos cube.Pos, r *rand.Random) {
	if !canGrow(tx, pos, 10) {
		return
	}
	treeHeight := int(r.Int31n(4) + 6)

	topSize := treeHeight - int(1+r.Int31n(2))
	lr := 2 + int(r.Int31n(2))

	trunk(tx, pos, block.SpruceWood(), treeHeight-int(r.Int31n(3)))

	radius := int(r.Int31n(2))
	minR, maxR := 0, 1

	for y := 0; y <= topSize; y++ {
		yy := pos[1] + treeHeight - y
		for x := pos[0] - radius; x <= pos[0]+radius; x++ {
			xOff := abs(x - pos[0])
			for z := pos[2] - radius; z <= pos[2]+radius; z++ {
				zOff := abs(z - pos[2])
				if xOff == radius && zOff == radius && radius > 0 {
					continue
				}

				p := cube.Pos{x, yy, z}
				if b := tx.Block(p); (b.Model() != model.Solid{}) {
					tx.SetBlock(p, block.Leaves{Wood: block.SpruceWood()}, nil)
				}
			}
		}

		if radius >= maxR {
			radius = minR
			minR = 1
			if maxR++; maxR > lr {
				maxR = lr
			}
		} else {
			radius++
		}
	}
}

type OakTree struct{}

func (OakTree) Grow(tx *world.Tx, pos cube.Pos, r *rand.Random) {
	if !canGrow(tx, pos, 7) {
		return
	}
	treeHeight := int(r.Int31n(3)) + 4
	basicTop(tx, pos, r, block.Leaves{Wood: block.OakWood()}, treeHeight)
	trunk(tx, pos, block.OakWood(), treeHeight-1)
}

type BirchTree struct {
	Super bool
}

func (b BirchTree) Grow(tx *world.Tx, pos cube.Pos, r *rand.Random) {
	if !canGrow(tx, pos, 7) {
		return
	}
	treeHeight := int(r.Int31n(3)) + 5
	if b.Super {
		treeHeight += 5
	}
	basicTop(tx, pos, r, block.Leaves{Wood: block.BirchWood()}, treeHeight)
	trunk(tx, pos, block.BirchWood(), treeHeight-1)
}

func basicTop(tx *world.Tx, pos cube.Pos, r *rand.Random, leaves block.Leaves, treeHeight int) {
	for yy := pos[1] - 3 + treeHeight; yy <= pos[1]+treeHeight; yy++ {
		yOff := yy - (pos[1] + treeHeight)
		mid := 1 - yOff/2
		for xx := pos[0] - mid; xx <= pos[0]+mid; xx++ {
			xOff := abs(xx - pos[0])
			for zz := pos[2] - mid; zz <= pos[2]+mid; zz++ {
				zOff := abs(zz - pos[2])
				if xOff == mid && zOff == mid && (yOff == 0 || r.Int31n(2) == 0) {
					continue
				}
				if (tx.Block(cube.Pos{xx, yy, zz}).Model() != model.Solid{}) {
					tx.SetBlock(cube.Pos{xx, yy, zz}, leaves, nil)
				}
			}
		}
	}
}

func trunk(tx *world.Tx, pos cube.Pos, wood block.WoodType, trunkHeight int) {
	tx.SetBlock(pos.Sub(cube.Pos{0, 1}), block.Dirt{}, nil)

	for y := 0; y < trunkHeight; y++ {
		b := tx.Block(pos.Add(cube.Pos{0, y}))
		if _, ok := overridable[b]; ok {
			tx.SetBlock(pos.Add(cube.Pos{0, y}), block.Log{Wood: wood}, nil)
		}
	}
}

func canGrow(tx *world.Tx, pos cube.Pos, treeHeight int) bool {
	radiusToCheck := 0
	for yy := 0; yy < treeHeight+3; yy++ {
		if yy == 1 || yy == treeHeight {
			radiusToCheck++
		}
		for xx := -radiusToCheck; xx <= radiusToCheck; xx++ {
			for zz := -radiusToCheck; zz <= radiusToCheck; zz++ {
				if _, ok := overridable[tx.Block(cube.Pos{pos[0] + xx, pos[1] + yy, pos[2] + zz})]; !ok {
					return false
				}
			}
		}
	}
	return true
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
