package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

// modernMonumentMainBlocks reproduces MonumentBuilding.postProcess before its
// child pieces. Its output order follows Java's write order so overlapping
// shell, fluid, and pillar writes resolve identically.
func (p modernOceanMonumentPiece) modernMonumentMainBlocks(clip structure.Bounds, env structure.ProcessorEnvironment) ([]structure.Block, error) {
	if env.BaseBlock == nil {
		return nil, fmt.Errorf("ocean monument requires base-block query")
	}
	blocks := make([]structure.Block, 0, 18000)
	overrides := make(map[[3]int32]string, 18000)
	put := func(x, y, z int32, name string) {
		pos := p.worldPos(x, y, z)
		if pos[0] < clip.Min[0] || pos[0] > clip.Max[0] || pos[1] < clip.Min[1] || pos[1] > clip.Max[1] || pos[2] < clip.Min[2] || pos[2] > clip.Max[2] {
			return
		}
		blocks = append(blocks, structure.Block{Pos: pos, State: world.BlockState{Name: name}})
		overrides[pos] = name
	}
	box := func(x0, y0, z0, x1, y1, z1 int32, name string) {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				for z := z0; z <= z1; z++ {
					put(x, y, z, name)
				}
			}
		}
	}
	waterBox := func(x0, y0, z0, x1, y1, z1 int32) error {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				for z := z0; z <= z1; z++ {
					pos := p.worldPos(x, y, z)
					if pos[0] < clip.Min[0] || pos[0] > clip.Max[0] || pos[1] < clip.Min[1] || pos[1] > clip.Max[1] || pos[2] < clip.Min[2] || pos[2] > clip.Max[2] {
						continue
					}
					name, exists := overrides[pos]
					if !exists {
						name, _ = env.BaseBlock(pos[0], pos[1], pos[2]).EncodeBlock()
					}
     if monumentIsWater(name) || name == "minecraft:ice" || name == "minecraft:packed_ice" || name == "minecraft:blue_ice" {
						continue
					}
					if pos[1] >= 63 {
						put(x, y, z, "minecraft:air")
					} else {
						put(x, y, z, "minecraft:water")
					}
				}
			}
		}
		return nil
	}
	chunkIntersects := func(x0, z0, x1, z1 int32) bool {
		a, b := p.worldPos(x0, 0, z0), p.worldPos(x1, 0, z1)
		minX, maxX := min(a[0], b[0]), max(a[0], b[0])
		minZ, maxZ := min(a[2], b[2]), max(a[2], b[2])
		return minX <= clip.Max[0] && maxX >= clip.Min[0] && minZ <= clip.Max[2] && maxZ >= clip.Min[2]
	}

	if err := waterBox(0, 0, 0, 58, 25, 58); err != nil {
		return nil, err
	}
	generateWing := func(xoff int32, flipped bool) {
		if !chunkIntersects(xoff, 0, xoff+23, 20) {
			return
		}
		box(xoff, 0, 0, xoff+24, 0, 20, "minecraft:prismarine")
		_ = waterBox(xoff, 1, 0, xoff+24, 10, 20)
		for i := range int32(4) {
			box(xoff+i, i+1, i, xoff+i, i+1, 20, "minecraft:prismarine_bricks")
			box(xoff+i+7, i+5, i+7, xoff+i+7, i+5, 20, "minecraft:prismarine_bricks")
			box(xoff+17-i, i+5, i+7, xoff+17-i, i+5, 20, "minecraft:prismarine_bricks")
			box(xoff+24-i, i+1, i, xoff+24-i, i+1, 20, "minecraft:prismarine_bricks")
			box(xoff+i+1, i+1, i, xoff+23-i, i+1, i, "minecraft:prismarine_bricks")
			box(xoff+i+8, i+5, i+7, xoff+16-i, i+5, i+7, "minecraft:prismarine_bricks")
		}
		box(xoff+4, 4, 4, xoff+6, 4, 20, "minecraft:prismarine")
		box(xoff+7, 4, 4, xoff+17, 4, 6, "minecraft:prismarine")
		box(xoff+18, 4, 4, xoff+20, 4, 20, "minecraft:prismarine")
		box(xoff+11, 8, 11, xoff+13, 8, 20, "minecraft:prismarine")
		for _, z := range []int32{12, 15, 18} {
			put(xoff+12, 9, z, "minecraft:prismarine_bricks")
		}
		leftPos, rightPos := xoff+5, xoff+19
		if flipped {
			leftPos, rightPos = xoff+19, xoff+5
		}
		for z := int32(20); z >= 5; z -= 3 {
			put(leftPos, 5, z, "minecraft:prismarine_bricks")
		}
		for z := int32(19); z >= 7; z -= 3 {
			put(rightPos, 5, z, "minecraft:prismarine_bricks")
		}
		for i := range int32(4) {
			pos := xoff + 17 - i*3
			if flipped {
				pos = xoff + 24 - (17 - i*3)
			}
			put(pos, 5, 5, "minecraft:prismarine_bricks")
		}
		put(rightPos, 5, 5, "minecraft:prismarine_bricks")
		box(xoff+11, 1, 12, xoff+13, 7, 12, "minecraft:prismarine")
		box(xoff+12, 1, 11, xoff+12, 7, 13, "minecraft:prismarine")
	}
	generateWing(0, false)
	generateWing(33, true)

	if chunkIntersects(22, 5, 35, 17) {
		_ = waterBox(25, 0, 0, 32, 8, 20)
		for i := range int32(4) {
			z := 5 + i*4
			box(24, 2, z, 24, 4, z, "minecraft:prismarine_bricks")
			box(22, 4, z, 23, 4, z, "minecraft:prismarine_bricks")
			put(25, 5, z, "minecraft:prismarine_bricks")
			put(26, 6, z, "minecraft:prismarine_bricks")
			put(26, 5, z, "minecraft:sea_lantern")
			box(33, 2, z, 33, 4, z, "minecraft:prismarine_bricks")
			box(34, 4, z, 35, 4, z, "minecraft:prismarine_bricks")
			put(32, 5, z, "minecraft:prismarine_bricks")
			put(31, 6, z, "minecraft:prismarine_bricks")
			put(31, 5, z, "minecraft:sea_lantern")
			box(27, 6, z, 30, 6, z, "minecraft:prismarine")
		}
	}
	if chunkIntersects(15, 20, 42, 21) {
		box(15, 0, 21, 42, 0, 21, "minecraft:prismarine")
		_ = waterBox(26, 1, 21, 31, 3, 21)
		box(21, 12, 21, 36, 12, 21, "minecraft:prismarine")
		box(17, 11, 21, 40, 11, 21, "minecraft:prismarine")
		box(16, 10, 21, 41, 10, 21, "minecraft:prismarine")
		box(15, 7, 21, 42, 9, 21, "minecraft:prismarine")
		box(16, 6, 21, 41, 6, 21, "minecraft:prismarine")
		box(17, 5, 21, 40, 5, 21, "minecraft:prismarine")
		box(21, 4, 21, 36, 4, 21, "minecraft:prismarine")
		box(22, 3, 21, 26, 3, 21, "minecraft:prismarine")
		box(31, 3, 21, 35, 3, 21, "minecraft:prismarine")
		box(23, 2, 21, 25, 2, 21, "minecraft:prismarine")
		box(32, 2, 21, 34, 2, 21, "minecraft:prismarine")
		box(28, 4, 20, 29, 4, 21, "minecraft:prismarine_bricks")
		put(27, 3, 21, "minecraft:prismarine_bricks")
		put(30, 3, 21, "minecraft:prismarine_bricks")
		put(26, 2, 21, "minecraft:prismarine_bricks")
		put(31, 2, 21, "minecraft:prismarine_bricks")
		put(25, 1, 21, "minecraft:prismarine_bricks")
		put(32, 1, 21, "minecraft:prismarine_bricks")
		for i := range int32(7) {
			put(28-i, 6+i, 21, "minecraft:dark_prismarine")
			put(29+i, 6+i, 21, "minecraft:dark_prismarine")
		}
		for i := range int32(4) {
			put(28-i, 9+i, 21, "minecraft:dark_prismarine")
			put(29+i, 9+i, 21, "minecraft:dark_prismarine")
		}
		put(28, 12, 21, "minecraft:dark_prismarine")
		put(29, 12, 21, "minecraft:dark_prismarine")
		for i := range int32(3) {
			put(22-i*2, 8, 21, "minecraft:dark_prismarine")
			put(22-i*2, 9, 21, "minecraft:dark_prismarine")
			put(35+i*2, 8, 21, "minecraft:dark_prismarine")
			put(35+i*2, 9, 21, "minecraft:dark_prismarine")
		}
		_ = waterBox(15, 13, 21, 42, 15, 21)
		_ = waterBox(15, 1, 21, 15, 6, 21)
		_ = waterBox(16, 1, 21, 16, 5, 21)
		_ = waterBox(17, 1, 21, 20, 4, 21)
		_ = waterBox(21, 1, 21, 21, 3, 21)
		_ = waterBox(22, 1, 21, 22, 2, 21)
		_ = waterBox(23, 1, 21, 24, 1, 21)
		_ = waterBox(42, 1, 21, 42, 6, 21)
		_ = waterBox(41, 1, 21, 41, 5, 21)
		_ = waterBox(37, 1, 21, 40, 4, 21)
		_ = waterBox(36, 1, 21, 36, 3, 21)
		_ = waterBox(33, 1, 21, 34, 1, 21)
		_ = waterBox(35, 1, 21, 35, 2, 21)
	}
	if chunkIntersects(21, 21, 36, 36) {
		box(21, 0, 22, 36, 0, 36, "minecraft:prismarine")
		_ = waterBox(21, 1, 22, 36, 23, 36)
		for i := range int32(4) {
			box(21+i, 13+i, 21+i, 36-i, 13+i, 21+i, "minecraft:prismarine_bricks")
			box(21+i, 13+i, 36-i, 36-i, 13+i, 36-i, "minecraft:prismarine_bricks")
			box(21+i, 13+i, 22+i, 21+i, 13+i, 35-i, "minecraft:prismarine_bricks")
			box(36-i, 13+i, 22+i, 36-i, 13+i, 35-i, "minecraft:prismarine_bricks")
		}
		box(25, 16, 25, 32, 16, 32, "minecraft:prismarine")
		box(25, 17, 25, 25, 19, 25, "minecraft:prismarine_bricks")
		box(32, 17, 25, 32, 19, 25, "minecraft:prismarine_bricks")
		box(25, 17, 32, 25, 19, 32, "minecraft:prismarine_bricks")
		box(32, 17, 32, 32, 19, 32, "minecraft:prismarine_bricks")
		put(26, 20, 26, "minecraft:prismarine_bricks")
		put(27, 21, 27, "minecraft:prismarine_bricks")
		put(27, 20, 27, "minecraft:sea_lantern")
		put(26, 20, 31, "minecraft:prismarine_bricks")
		put(27, 21, 30, "minecraft:prismarine_bricks")
		put(27, 20, 30, "minecraft:sea_lantern")
		put(31, 20, 31, "minecraft:prismarine_bricks")
		put(30, 21, 30, "minecraft:prismarine_bricks")
		put(30, 20, 30, "minecraft:sea_lantern")
		put(31, 20, 26, "minecraft:prismarine_bricks")
		put(30, 21, 27, "minecraft:prismarine_bricks")
		put(30, 20, 27, "minecraft:sea_lantern")
		box(28, 21, 27, 29, 21, 27, "minecraft:prismarine")
		box(27, 21, 28, 27, 21, 29, "minecraft:prismarine")
		box(28, 21, 30, 29, 21, 30, "minecraft:prismarine")
		box(30, 21, 28, 30, 21, 29, "minecraft:prismarine")
	}
	if chunkIntersects(0, 21, 6, 58) {
		box(0, 0, 21, 6, 0, 57, "minecraft:prismarine")
		_ = waterBox(0, 1, 21, 6, 7, 57)
		box(4, 4, 21, 6, 4, 53, "minecraft:prismarine")
		for i := range int32(4) {
			box(i, i+1, 21, i, i+1, 57-i, "minecraft:prismarine_bricks")
		}
		for z := int32(23); z < 53; z += 3 {
			put(5, 5, z, "minecraft:prismarine_bricks")
		}
		put(5, 5, 52, "minecraft:prismarine_bricks")
		for i := range int32(4) {
			box(i, i+1, 21, i, i+1, 57-i, "minecraft:prismarine_bricks")
		}
		box(4, 1, 52, 6, 3, 52, "minecraft:prismarine")
		box(5, 1, 51, 5, 3, 53, "minecraft:prismarine")
	}
	if chunkIntersects(51, 21, 58, 58) {
		box(51, 0, 21, 57, 0, 57, "minecraft:prismarine")
		_ = waterBox(51, 1, 21, 57, 7, 57)
		box(51, 4, 21, 53, 4, 53, "minecraft:prismarine")
		for i := range int32(4) {
			box(57-i, i+1, 21, 57-i, i+1, 57-i, "minecraft:prismarine_bricks")
		}
		for z := int32(23); z < 53; z += 3 {
			put(52, 5, z, "minecraft:prismarine_bricks")
		}
		put(52, 5, 52, "minecraft:prismarine_bricks")
		box(51, 1, 52, 53, 3, 52, "minecraft:prismarine")
		box(52, 1, 51, 52, 3, 53, "minecraft:prismarine")
	}
	if chunkIntersects(0, 51, 57, 57) {
		box(7, 0, 51, 50, 0, 57, "minecraft:prismarine")
		_ = waterBox(7, 1, 51, 50, 10, 57)
		for i := range int32(4) {
			box(i+1, i+1, 57-i, 56-i, i+1, 57-i, "minecraft:prismarine_bricks")
		}
	}
	if chunkIntersects(7, 21, 13, 50) {
		box(7, 0, 21, 13, 0, 50, "minecraft:prismarine")
		_ = waterBox(7, 1, 21, 13, 10, 50)
		box(11, 8, 21, 13, 8, 53, "minecraft:prismarine")
		for i := range int32(4) {
			box(i+7, i+5, 21, i+7, i+5, 54, "minecraft:prismarine_bricks")
		}
		for z := int32(21); z <= 45; z += 3 {
			put(12, 9, z, "minecraft:prismarine_bricks")
		}
	}
	if chunkIntersects(44, 21, 50, 54) {
		box(44, 0, 21, 50, 0, 50, "minecraft:prismarine")
		_ = waterBox(44, 1, 21, 50, 10, 50)
		box(44, 8, 21, 46, 8, 53, "minecraft:prismarine")
		for i := range int32(4) {
			box(50-i, i+5, 21, 50-i, i+5, 54, "minecraft:prismarine_bricks")
		}
		for z := int32(21); z <= 45; z += 3 {
			put(45, 9, z, "minecraft:prismarine_bricks")
		}
	}
	if chunkIntersects(8, 44, 49, 54) {
		box(14, 0, 44, 43, 0, 50, "minecraft:prismarine")
		_ = waterBox(14, 1, 44, 43, 10, 50)
		for x := int32(12); x <= 45; x += 3 {
			put(x, 9, 45, "minecraft:prismarine_bricks")
			put(x, 9, 52, "minecraft:prismarine_bricks")
			if x == 12 || x == 18 || x == 24 || x == 33 || x == 39 || x == 45 {
				for _, z := range []int32{47, 50} { put(x, 9, z, "minecraft:prismarine_bricks") }
				for _, z := range []int32{45, 46, 51, 52} { put(x, 10, z, "minecraft:prismarine_bricks") }
				for _, z := range []int32{47, 50} { put(x, 11, z, "minecraft:prismarine_bricks") }
				for _, z := range []int32{48, 49} { put(x, 12, z, "minecraft:prismarine_bricks") }
			}
		}
		for i := range int32(3) {
			box(8+i, 5+i, 54, 49-i, 5+i, 54, "minecraft:prismarine")
		}
		box(11, 8, 54, 46, 8, 54, "minecraft:prismarine_bricks")
		box(14, 8, 44, 43, 8, 53, "minecraft:prismarine")
	}
	if chunkIntersects(14, 21, 20, 43) {
		box(14, 0, 21, 20, 0, 43, "minecraft:prismarine")
		_ = waterBox(14, 1, 22, 20, 14, 43)
		box(18, 12, 22, 20, 12, 39, "minecraft:prismarine")
		box(18, 12, 21, 20, 12, 21, "minecraft:prismarine_bricks")
		for i := range int32(4) {
			box(i+14, i+9, 21, i+14, i+9, 43-i, "minecraft:prismarine_bricks")
		}
		for z := int32(23); z <= 39; z += 3 {
			put(19, 13, z, "minecraft:prismarine_bricks")
		}
	}
	if chunkIntersects(37, 21, 43, 43) {
		box(37, 0, 21, 43, 0, 43, "minecraft:prismarine")
		_ = waterBox(37, 1, 22, 43, 14, 43)
		box(37, 12, 22, 39, 12, 39, "minecraft:prismarine")
		box(37, 12, 21, 39, 12, 21, "minecraft:prismarine_bricks")
		for i := range int32(4) {
			box(43-i, i+9, 21, 43-i, i+9, 43-i, "minecraft:prismarine_bricks")
		}
		for z := int32(23); z <= 39; z += 3 {
			put(38, 13, z, "minecraft:prismarine_bricks")
		}
	}
	if chunkIntersects(15, 37, 42, 43) {
		box(21, 0, 37, 36, 0, 43, "minecraft:prismarine")
		_ = waterBox(21, 1, 37, 36, 14, 43)
		box(21, 12, 37, 36, 12, 39, "minecraft:prismarine")
		for i := range int32(4) {
			box(15+i, i+9, 43-i, 42-i, i+9, 43-i, "minecraft:prismarine_bricks")
		}
		for x := int32(21); x <= 36; x += 3 {
			put(x, 13, 38, "minecraft:prismarine_bricks")
		}
	}

	for pillarX := range 7 {
		pillarZ := 0
		for pillarZ < 7 {
			if pillarZ == 0 && pillarX == 3 {
				pillarZ = 6
			}
			bx, bz := int32(pillarX*9), int32(pillarZ*9)
			for w := range 4 {
				for d := range 4 {
					x, z := bx+int32(w), bz+int32(d)
					put(x, 0, z, "minecraft:prismarine_bricks")
					pos := p.worldPos(x, -1, z)
					if pos[0] < clip.Min[0] || pos[0] > clip.Max[0] || pos[2] < clip.Min[2] || pos[2] > clip.Max[2] || pos[1] < clip.Min[1] || pos[1] > clip.Max[1] {
						continue
					}
					for y := int32(-1); ; y-- {
						pos = p.worldPos(x, y, z)
						if pos[1] <= clip.Min[1]+1 {
							break
						}
						name, exists := overrides[pos]
						if !exists {
							name, _ = env.BaseBlock(pos[0], pos[1], pos[2]).EncodeBlock()
						}
      if name != "minecraft:air" && !monumentIsWater(name) && name != "minecraft:lava" && name != "minecraft:glow_lichen" && name != "minecraft:seagrass" && name != "minecraft:tall_seagrass" {
							break
						}
						put(x, y, z, "minecraft:prismarine_bricks")
					}
				}
			}
			if pillarX != 0 && pillarX != 6 {
				pillarZ += 6
			} else {
				pillarZ++
			}
		}
	}
	for i := range int32(5) {
		_ = waterBox(-1-i, i*2, -1-i, -1-i, 23, 58+i)
		_ = waterBox(58+i, i*2, -1-i, 58+i, 23, 58+i)
		_ = waterBox(-i, i*2, -1-i, 57+i, 23, -1-i)
		_ = waterBox(-i, i*2, 58+i, 57+i, 23, 58+i)
	}
	return blocks, nil
}
