package overworld

import (
	"fmt"
	"sort"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func init() { registerModernStructureBuilder("desert_pyramid", buildModernDesertPyramid) }

type modernDesertPyramid struct {
	box structure.Box
	dir int
}

func (p modernDesertPyramid) Bounds() structure.Box { return p.box }

func (p modernDesertPyramid) pos(x, y, z int32) [3]int32 {
	b := p.box
	switch p.dir {
	case 0: // north
		return [3]int32{b.Min[0] + x, b.Min[1] + y, b.Max[2] - z}
	case 1: // east
		return [3]int32{b.Min[0] + z, b.Min[1] + y, b.Min[2] + x}
	case 2: // south
		return [3]int32{b.Min[0] + x, b.Min[1] + y, b.Min[2] + z}
	default: // west
		return [3]int32{b.Max[0] - z, b.Min[1] + y, b.Min[2] + x}
	}
}

func (p modernDesertPyramid) Place(clip structure.Bounds, env structure.ProcessorEnvironment) (structure.Placement, error) {
	out := structure.Placement{Blocks: make([]structure.Block, 0, 12000)}
	if env.Random == nil || env.LevelRandom == nil || env.MotionBlockingNoLeavesHeight == nil {
		return out, fmt.Errorf("desert pyramid requires source placement, level random, and live heightmap")
	}
	var detached structure.StructurePieceState
	if env.PieceState == nil {
		env.PieceState = &detached
	}
	flags := env.PieceState
	if env.TransientStructureState || flags.Pyramid == nil {
		flags.Pyramid = clonePyramidRuntime(flags.Pyramid)
	}
	// Java evaluates -random.nextInt(3) on every call, even with cached HPos.
	offset := -env.Random.NextInt(3)
	if !flags.GroundHeightSet || flags.GroundHeight < 0 {
		lowest := int32(1 << 30)
		for z := p.box.Min[2]; z <= p.box.Max[2]; z++ {
			for x := p.box.Min[0]; x <= p.box.Max[0]; x++ {
				if y := env.MotionBlockingNoLeavesHeight(x, z); y < lowest {
					lowest = y
				}
			}
		}
		flags.GroundHeightSet, flags.GroundHeight = true, lowest
		flags.BoundingBoxYSet = true
		flags.BoundingBoxMinY = lowest + offset
	} else if !flags.BoundingBoxYSet {
		flags.BoundingBoxYSet = true
		flags.BoundingBoxMinY = p.box.Min[1]
	}
	p.box.Max[1] += flags.BoundingBoxMinY - p.box.Min[1]
	p.box.Min[1] = flags.BoundingBoxMinY
	journal := newJournalStructureWorld(env, clip, &out)
	inside := journal.inside
	put := func(x, y, z int32, name string, props map[string]any) {
		journal.placeSourcePiece(p.pos(x, y, z), world.BlockState{Name: "minecraft:" + name, Properties: props}, [4]string{"north", "east", "south", "west"}[p.dir])
	}
	isAir := func(pos [3]int32) bool {
		if !inside(pos[0], pos[1], pos[2]) {
			return true
		}
		switch journal.blockName(pos) {
		case "minecraft:air", "minecraft:cave_air", "minecraft:void_air":
			return true
		}
		return false
	}
	box := func(x0, y0, z0, x1, y1, z1 int32, edge, fill string, skipAir bool) {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				for z := z0; z <= z1; z++ {
					pos := p.pos(x, y, z)
					if skipAir && isAir(pos) {
						continue
					}
					name := fill
					if x == x0 || x == x1 || y == y0 || y == y1 || z == z0 || z == z1 {
						name = edge
					}
					put(x, y, z, name, nil)
				}
			}
		}
	}
	place := put
	stair := func(x, y, z int32, facing string) {
		place(x, y, z, "sandstone_stairs", map[string]any{"facing": facing, "half": "bottom", "shape": "straight", "waterlogged": false})
	}
	full := func(x0, y0, z0, x1, y1, z1 int32, name string) {
		box(x0, y0, z0, x1, y1, z1, name, name, false)
	}
	full(0, -4, 0, 20, 0, 20, "sandstone")
	for pos := int32(1); pos <= 9; pos++ {
		full(pos, pos, pos, 20-pos, pos, 20-pos, "sandstone")
		full(pos+1, pos, pos+1, 19-pos, pos, 19-pos, "air")
	}
	for x := int32(0); x < 21; x++ {
		for z := int32(0); z < 21; z++ {
			start := p.pos(x, -5, z)
			if !inside(start[0], start[1], start[2]) {
				continue
			}
			for y := start[1]; y > int32(env.Range.Min())+1; y-- {
				pos := [3]int32{start[0], y, start[2]}
				name := journal.blockName(pos)
				if name != "minecraft:air" && name != "minecraft:cave_air" && name != "minecraft:void_air" &&
					name != "minecraft:water" && name != "minecraft:lava" && name != "minecraft:glow_lichen" &&
					name != "minecraft:seagrass" && name != "minecraft:tall_seagrass" {
					break
				}
				journal.setUnclipped(pos, world.BlockState{Name: "minecraft:sandstone"})
			}
		}
	}

	// Entry towers, main stairs, and ornamental sandstone face.
	box(0, 0, 0, 4, 9, 4, "sandstone", "air", false)
	full(1, 10, 1, 3, 10, 3, "sandstone")
	for _, v := range []struct {
		x, y, z int32
		f       string
	}{{2, 10, 0, "north"}, {2, 10, 4, "south"}, {0, 10, 2, "east"}, {4, 10, 2, "west"}} {
		stair(v.x, v.y, v.z, v.f)
	}
	box(16, 0, 0, 20, 9, 4, "sandstone", "air", false)
	full(17, 10, 1, 19, 10, 3, "sandstone")
	for _, v := range []struct {
		x, y, z int32
		f       string
	}{{18, 10, 0, "north"}, {18, 10, 4, "south"}, {16, 10, 2, "east"}, {20, 10, 2, "west"}} {
		stair(v.x, v.y, v.z, v.f)
	}
	box(8, 0, 0, 12, 4, 4, "sandstone", "air", false)
	full(9, 1, 0, 11, 3, 4, "air")
	for _, v := range [][3]int32{{9, 1, 1}, {9, 2, 1}, {9, 3, 1}, {10, 3, 1}, {11, 3, 1}, {11, 2, 1}, {11, 1, 1}} {
		place(v[0], v[1], v[2], "cut_sandstone", nil)
	}
	box(4, 1, 1, 8, 3, 3, "sandstone", "air", false)
	full(4, 1, 2, 8, 2, 2, "air")
	box(12, 1, 1, 16, 3, 3, "sandstone", "air", false)
	full(12, 1, 2, 16, 2, 2, "air")
	full(5, 4, 5, 15, 4, 15, "sandstone")
	full(9, 4, 9, 11, 4, 11, "air")
	for _, v := range [][2]int32{{8, 8}, {12, 8}, {8, 12}, {12, 12}} {
		full(v[0], 1, v[1], v[0], 3, v[1], "cut_sandstone")
	}
	full(1, 1, 5, 4, 4, 11, "sandstone")
	full(16, 1, 5, 19, 4, 11, "sandstone")
	full(6, 7, 9, 6, 7, 11, "sandstone")
	full(14, 7, 9, 14, 7, 11, "sandstone")
	full(5, 5, 9, 5, 7, 11, "cut_sandstone")
	full(15, 5, 9, 15, 7, 11, "cut_sandstone")
	for _, v := range [][3]int32{{5, 5, 10}, {5, 6, 10}, {6, 6, 10}, {15, 5, 10}, {15, 6, 10}, {14, 6, 10}} {
		place(v[0], v[1], v[2], "air", nil)
	}
	full(2, 4, 4, 2, 6, 4, "air")
	full(18, 4, 4, 18, 6, 4, "air")
	for _, v := range [][3]int32{{2, 4, 5}, {2, 3, 4}, {18, 4, 5}, {18, 3, 4}} {
		stair(v[0], v[1], v[2], "north")
	}
	full(1, 1, 3, 2, 2, 3, "sandstone")
	full(18, 1, 3, 19, 2, 3, "sandstone")
	place(1, 1, 2, "sandstone", nil)
	place(19, 1, 2, "sandstone", nil)
	slab := map[string]any{"type": "bottom", "waterlogged": false}
	place(1, 2, 2, "sandstone_slab", slab)
	place(19, 2, 2, "sandstone_slab", slab)
	stair(2, 1, 2, "west")
	stair(18, 1, 2, "east")
	full(4, 3, 5, 4, 3, 17, "sandstone")
	full(16, 3, 5, 16, 3, 17, "sandstone")
	full(3, 1, 5, 4, 2, 16, "air")
	full(15, 1, 5, 16, 2, 16, "air")
	for z := int32(5); z <= 17; z += 2 {
		for _, x := range []int32{4, 16} {
			place(x, 1, z, "cut_sandstone", nil)
			place(x, 2, z, "chiseled_sandstone", nil)
		}
	}
	for _, v := range [][3]int32{{10, 0, 7}, {10, 0, 8}, {9, 0, 9}, {11, 0, 9}, {8, 0, 10}, {12, 0, 10}, {7, 0, 10}, {13, 0, 10}, {9, 0, 11}, {11, 0, 11}, {10, 0, 12}, {10, 0, 13}} {
		place(v[0], v[1], v[2], "orange_terracotta", nil)
	}
	place(10, 0, 10, "blue_terracotta", nil)
	for _, x := range []int32{0, 20} {
		for _, y := range []int32{2, 3, 4, 5, 6, 7, 8} {
			for _, z := range []int32{1, 2, 3} {
				name := "orange_terracotta"
				switch y {
				case 2, 3, 5, 8:
					if y == 8 || z != 2 {
						name = "cut_sandstone"
					}
				case 4, 6:
					if z == 2 {
						name = "chiseled_sandstone"
					}
				}
				place(x, y, z, name, nil)
			}
		}
	}
	for _, x := range []int32{2, 18} {
		for _, y := range []int32{2, 3, 4, 5, 6, 7, 8} {
			for _, dx := range []int32{-1, 0, 1} {
				name := "orange_terracotta"
				switch y {
				case 2, 3, 5, 8:
					if y == 8 || dx != 0 {
						name = "cut_sandstone"
					}
				case 4, 6:
					if dx == 0 {
						name = "chiseled_sandstone"
					}
				}
				place(x+dx, y, 0, name, nil)
			}
		}
	}
	full(8, 4, 0, 12, 6, 0, "cut_sandstone")
	place(8, 6, 0, "air", nil)
	place(12, 6, 0, "air", nil)
	place(9, 5, 0, "orange_terracotta", nil)
	place(10, 5, 0, "chiseled_sandstone", nil)
	place(11, 5, 0, "orange_terracotta", nil)

	// Central trap room, four loot chests, and exact cellar walls.
	full(8, -14, 8, 12, -11, 12, "cut_sandstone")
	full(8, -10, 8, 12, -10, 12, "chiseled_sandstone")
	full(8, -9, 8, 12, -9, 12, "cut_sandstone")
	full(8, -8, 8, 12, -1, 12, "sandstone")
	full(9, -11, 9, 11, -1, 11, "air")
	place(10, -11, 10, "stone_pressure_plate", nil)
	box(9, -13, 9, 11, -13, 11, "tnt", "air", false)
	for _, pos := range []struct {
		x, y, z int32
		name    string
	}{
		{8, -11, 10, "air"}, {8, -10, 10, "air"},
		{7, -10, 10, "chiseled_sandstone"}, {7, -11, 10, "cut_sandstone"},
		{12, -11, 10, "air"}, {12, -10, 10, "air"},
		{13, -10, 10, "chiseled_sandstone"}, {13, -11, 10, "cut_sandstone"},
		{10, -11, 8, "air"}, {10, -10, 8, "air"},
		{10, -10, 7, "chiseled_sandstone"}, {10, -11, 7, "cut_sandstone"},
		{10, -11, 12, "air"}, {10, -10, 12, "air"},
		{10, -10, 13, "chiseled_sandstone"}, {10, -11, 13, "cut_sandstone"},
	} {
		place(pos.x, pos.y, pos.z, pos.name, nil)
	}
	for _, chest := range []struct {
		x, z int32
		flag uint8
	}{
		{10, 8, 1 << 2}, {12, 10, 1 << 3}, {10, 12, 1 << 0}, {8, 10, 1 << 1},
	} {
		if flags.LootPlaced&chest.flag != 0 {
			continue
		}
		if journal.createSourceChest(p.pos(chest.x, -11, chest.z), "minecraft:chests/desert_pyramid", env.Random) {
			flags.LootPlaced |= chest.flag
		}
	}
	centerX, centerY, centerZ := int32(16), int32(-4), int32(13)
	variant := env.LevelRandom.NextBoolean()
	stair(13, -1, 17, "west")
	stair(14, -2, 17, "west")
	stair(15, -3, 17, "west")
	for _, v := range []struct {
		x, y, z int32
		name    string
	}{
		{12, 0, 17, "sand"}, {13, 0, 17, "sand"}, {14, 0, 17, "sand"}, {15, 0, 17, "sand"}, {16, 0, 17, "sand"},
		{14, -1, 17, "sand"}, {15, -1, 17, "sand"}, {16, -1, 17, "sand"}, {15, -2, 17, "sand"}, {16, -2, 17, "sand"}, {16, -3, 17, "sand"},
	} {
		name := v.name
		if v.x == 15 && v.y == -1 && !variant {
			name = "sandstone"
		}
		if v.x == 16 && v.y == -1 && variant {
			name = "sandstone"
		}
		if v.x == 16 && v.y == -2 {
			name = "sandstone"
		}
		place(v.x, v.y, v.z, name, nil)
	}
	// The source skips existing air when adding the cellar's cut/chiseled shell.
	for _, y := range []int32{-3, -2, -1} {
		material := "cut_sandstone"
		if y == -2 {
			material = "chiseled_sandstone"
		}
		if y == -1 {
			material = "cut_sandstone"
		}
		for _, box := range [][6]int32{
			{13, y, 10, 13, y, 15}, {19, y, 10, 19, y, 15},
			{13, y, 10, 19, y, 11}, {13, y, 16, 19, y, 16},
		} {
			for x := box[0]; x <= box[3]; x++ {
				for z := box[2]; z <= box[5]; z++ {
					pos := p.pos(x, y, z)
					if !isAir(pos) {
						put(x, y, z, material, nil)
					}
				}
			}
		}
	}
	for y := centerY + 1; y <= centerY+3; y++ {
		for x := centerX - 2; x <= centerX+2; x++ {
			for z := centerZ - 2; z <= centerZ+2; z++ {
				flags.Pyramid.SandPositions = append(flags.Pyramid.SandPositions, p.pos(x, y, z))
			}
		}
	}
	// placeSandBox records candidate positions; placeCollapsedRoof consumes the
	// independent WorldGenRegion RNG, then sets its positional roof candidate.
	for x := int32(14); x <= 18; x++ {
		for z := int32(11); z <= 15; z++ {
			name := "sand"
			if env.LevelRandom.NextFloat() < 0.33 {
				name = "sandstone"
			}
			place(x, 0, z, name, nil)
		}
	}
	roofOrigin := p.pos(14, 0, 11)
	roofSeed := vanilla.NewLegacyRandom(env.WorldSeed)
	roofRandom := roofSeed.ForkPositional().At(roofOrigin[0], roofOrigin[1], roofOrigin[2])
	roofX, roofZ := int32(14)+roofRandom.NextInt(5), int32(11)+roofRandom.NextInt(5)
	flags.Pyramid.RoofPosition = p.pos(roofX, 0, roofZ)
	place(16, -4, 13, "blue_terracotta", nil)
	for _, pos := range [][3]int32{
		{17, -4, 12}, {17, -4, 14}, {15, -4, 12}, {15, -4, 14},
		{18, -4, 13}, {14, -4, 13}, {16, -4, 15}, {16, -4, 11},
	} {
		place(pos[0], pos[1], pos[2], "orange_terracotta", nil)
	}
	place(19, -4, 13, "orange_terracotta", nil)
	for _, pos := range [][3]int32{{19, -3, 13}, {19, -2, 13}} {
		flags.Pyramid.SandPositions = append(flags.Pyramid.SandPositions, p.pos(pos[0], pos[1], pos[2]))
	}
	place(20, -3, 13, "cut_sandstone", nil)
	place(20, -2, 13, "chiseled_sandstone", nil)
	place(13, -4, 13, "orange_terracotta", nil)
	for _, pos := range [][3]int32{{13, -3, 13}, {13, -2, 13}} {
		flags.Pyramid.SandPositions = append(flags.Pyramid.SandPositions, p.pos(pos[0], pos[1], pos[2]))
	}
	place(12, -3, 13, "cut_sandstone", nil)
	place(12, -2, 13, "chiseled_sandstone", nil)
	place(16, -4, 16, "orange_terracotta", nil)
	for _, pos := range [][3]int32{{16, -3, 16}, {16, -2, 16}} {
		flags.Pyramid.SandPositions = append(flags.Pyramid.SandPositions, p.pos(pos[0], pos[1], pos[2]))
	}
	place(16, -4, 10, "orange_terracotta", nil)
	for _, pos := range [][3]int32{{16, -3, 10}, {16, -2, 10}} {
		flags.Pyramid.SandPositions = append(flags.Pyramid.SandPositions, p.pos(pos[0], pos[1], pos[2]))
	}
	place(16, -3, 9, "cut_sandstone", nil)
	place(16, -2, 9, "chiseled_sandstone", nil)
	return out, nil
}
func clonePyramidRuntime(state *structure.PyramidPieceRuntimeState) *structure.PyramidPieceRuntimeState {
	if state == nil {
		return &structure.PyramidPieceRuntimeState{SandPositions: make([][3]int32, 0, 83)}
	}
	positions := make([][3]int32, len(state.SandPositions), len(state.SandPositions)+83)
	copy(positions, state.SandPositions)
	return &structure.PyramidPieceRuntimeState{
		SandPositions: positions,
		RoofPosition:  state.RoofPosition,
	}
}

func (p modernDesertPyramid) AfterPlace(clip structure.Bounds, env structure.ProcessorEnvironment, piecesBox structure.Box) (structure.Placement, error) {
	out := structure.Placement{}
	var empty structure.PyramidPieceRuntimeState
	runtime := &empty
	if env.PieceState != nil && env.PieceState.Pyramid != nil {
		runtime = env.PieceState.Pyramid
	}
	journal := newJournalStructureWorld(env, clip, &out)
	positionalSeed := vanilla.NewLegacyRandom(env.WorldSeed)
	positional := positionalSeed.ForkPositional().At(
		(piecesBox.Min[0]+piecesBox.Max[0])/2,
		(piecesBox.Min[1]+piecesBox.Max[1])/2,
		(piecesBox.Min[2]+piecesBox.Max[2])/2,
	)
	roof := runtime.RoofPosition
	if journal.inside(roof[0], roof[1], roof[2]) {
		pos := roof
		if journal.set(pos[0], pos[1], pos[2], world.BlockState{Name: "minecraft:suspicious_sand"}) {
			out.Blocks[len(out.Blocks)-1].NBT = map[string]any{
				"id": "minecraft:brushable_block", "LootTable": "minecraft:archaeology/desert_pyramid", "LootTableSeed": blockPosLong(pos),
			}
		}
	}
	candidates := append([][3]int32(nil), runtime.SandPositions...)
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a[1] != b[1] {
			return a[1] < b[1]
		}
		if a[2] != b[2] {
			return a[2] < b[2]
		}
		return a[0] < b[0]
	})
	unique := candidates[:0]
	for _, pos := range candidates {
		if len(unique) == 0 || unique[len(unique)-1] != pos {
			unique = append(unique, pos)
		}
	}
	for i := len(unique); i > 1; i-- {
		j := int(positional.NextInt(int32(i)))
		unique[i-1], unique[j] = unique[j], unique[i-1]
	}
	suspicious := int(positional.NextInt(3)) + 5
	if suspicious > len(unique) {
		suspicious = len(unique)
	}
	for i, pos := range unique {
		if !journal.inside(pos[0], pos[1], pos[2]) {
			continue
		}
		name := "sand"
		if i < suspicious {
			name = "suspicious_sand"
		}
		if journal.set(pos[0], pos[1], pos[2], world.BlockState{Name: "minecraft:" + name}) && i < suspicious {
			out.Blocks[len(out.Blocks)-1].NBT = map[string]any{
				"id": "minecraft:brushable_block", "LootTable": "minecraft:archaeology/desert_pyramid", "LootTableSeed": blockPosLong(pos),
			}
		}
	}
	return out, nil
}
func blockPosLong(pos [3]int32) int64 {
	return (int64(pos[0])&0x3ffffff)<<38 | (int64(pos[2])&0x3ffffff)<<12 | int64(pos[1])&0xfff
}

func buildModernDesertPyramid(in modernStructureBuildInput) (modernStructureStart, bool, error) {
	if in.random == nil {
		return modernStructureStart{}, false, fmt.Errorf("desert pyramid requires structure random")
	}
	x, z := in.chunkX*16, in.chunkZ*16
	direction := int(in.random.NextInt(4))
	box := structure.Box{Min: [3]int32{x, 64, z}, Max: [3]int32{x + 20, 78, z + 20}}
	piece := modernDesertPyramid{box: box, dir: direction}
	return modernStructureStart{name: in.name, pieces: []modernStructurePiece{piece}, box: box, terrain: terrainAdjustment(stringValue(in.config["terrain_adaptation"]))}, true, nil
}
