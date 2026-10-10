package overworld

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init() { registerModernStructureBuilder("stronghold", buildModernStronghold) }

type strongholdPiece struct {
	box                                    javastructure.Box
	kind, dir, door                        string
	depth                                  int
	leftLow, leftHigh, rightLow, rightHigh bool
	seed                                   int64
	variant                                int
	journal                                *journalStructureWorld
}

func(p strongholdPiece)Bounds()javastructure.Box{return p.box}
func (p strongholdPiece) Place(clip javastructure.Bounds, env javastructure.ProcessorEnvironment) (javastructure.Placement, error) {
	if env.Random == nil {
		return javastructure.Placement{}, fmt.Errorf("stronghold placement requires shared Java random")
	}
	if env.PieceState == nil {
		return javastructure.Placement{}, fmt.Errorf("stronghold placement requires persisted piece state")
	}
	out := javastructure.Placement{}
	journal := newJournalStructureWorld(env, clip, &out)
	p.journal = journal
	put := func(x, y, z int32, n string, props map[string]any) {
		journal.set(x, y, z, world.BlockState{Name: "minecraft:" + n, Properties: props})
	}
	b := p.box
	stateRandom := env.Random
	height := b.Max[1] - b.Min[1] + 1
	width := b.Max[0] - b.Min[0] + 1
	length := b.Max[2] - b.Min[2] + 1
	if p.dir == "east" || p.dir == "west" {
		width, length = length, width
	}
	if p.kind != "filler" {
		for y := int32(0); y < height; y++ {
			for x := int32(0); x < width; x++ {
				for z := int32(0); z < length; z++ {
					pos := strongholdLocal(b, p.dir, x, y, z)
					wx, wy, wz := pos[0], pos[1], pos[2]
					if p.kind != "portal" {
						name, ok := javadata.BlockStateName(journal.getBlockID(wx, wy, wz))
						if ok && (name == "minecraft:air" || name == "minecraft:cave_air" || name == "minecraft:void_air") {
							continue
						}
					}
					edge := x == 0 || x == width-1 || y == 0 || y == height-1 || z == 0 || z == length-1
					if edge {
						roll := stateRandom.NextFloat()
						block := "stone_bricks"
						if roll < 0.2 {
							block = "cracked_stone_bricks"
						} else if roll < 0.5 {
							block = "mossy_stone_bricks"
						} else if roll < 0.55 {
							block = "infested_stone_bricks"
						}
						put(wx, wy, wz, block, nil)
					} else {
						put(wx, wy, wz, "cave_air", nil)
					}
				}
			}
		}
	}
	if p.kind == "portal" {
		portalDoor := p
		portalDoor.door = "grates"
		strongholdDoorPlacement(portalDoor, clip, &out)
	} else if p.kind != "filler" {
		strongholdDoorPlacement(p, clip, &out)
	}
	switch p.kind {
	case "filler":
		for i := int32(0); i < int32(p.variant); i++ {
			for x := int32(0); x <= 4; x++ {
				strongholdPutLocal(p, clip, &out, x, 0, i, "stone_bricks", nil)
			}
			for y := int32(1); y <= 3; y++ {
				strongholdPutLocal(p, clip, &out, 0, y, i, "stone_bricks", nil)
				for x := int32(1); x <= 3; x++ {
					strongholdPutLocal(p, clip, &out, x, y, i, "cave_air", nil)
				}
				strongholdPutLocal(p, clip, &out, 4, y, i, "stone_bricks", nil)
			}
			for x := int32(0); x <= 4; x++ {
				strongholdPutLocal(p, clip, &out, x, 4, i, "stone_bricks", nil)
			}
		}
	case "start", "stairs":
		strongholdOpening(p, clip, &out, 1, 1, 4, 3, 3, 4)
		strongholdPutLocal(p, clip, &out, 2, 6, 1, "stone_bricks", nil)
		strongholdPutLocal(p, clip, &out, 1, 5, 1, "stone_bricks", nil)
		strongholdPutLocal(p, clip, &out, 1, 6, 1, "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdPutLocal(p, clip, &out, 1, 5, 2, "stone_bricks", nil)
		strongholdPutLocal(p, clip, &out, 1, 4, 3, "stone_bricks", nil)
		strongholdPutLocal(p, clip, &out, 1, 5, 3, "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdPutLocal(p, clip, &out, 2, 4, 3, "stone_bricks", nil)
		strongholdPutLocal(p, clip, &out, 3, 3, 3, "stone_bricks", nil)
		strongholdPutLocal(p, clip, &out, 3, 4, 3, "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdPutLocal(p, clip, &out, 3, 3, 2, "stone_bricks", nil)
		strongholdPutLocal(p, clip, &out, 3, 2, 1, "stone_bricks", nil)
		strongholdPutLocal(p, clip, &out, 3, 3, 1, "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdPutLocal(p, clip, &out, 2, 2, 1, "stone_bricks", nil)
		strongholdPutLocal(p, clip, &out, 1, 1, 1, "stone_bricks", nil)
		strongholdPutLocal(p, clip, &out, 1, 2, 1, "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdPutLocal(p, clip, &out, 1, 1, 2, "stone_bricks", nil)
		strongholdPutLocal(p, clip, &out, 1, 1, 3, "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
	case "straight_stairs":
		strongholdOpening(p, clip, &out, 1, 1, 7, 3, 3, 7)
		for i := int32(0); i < 6; i++ {
			for x := int32(1); x <= 3; x++ {
				strongholdPutLocal(p, clip, &out, x, 6-i, 1+i, "cobblestone_stairs", map[string]any{"facing": "south", "half": "bottom", "shape": "straight", "waterlogged": false})
			}
			if i < 5 {
				for x := int32(1); x <= 3; x++ {
					strongholdPutLocal(p, clip, &out, x, 5-i, 1+i, "stone_bricks", nil)
				}
			}
		}
	case "straight":
		strongholdOpening(p, clip, &out, 1, 1, 6, 3, 3, 6)
		for _, q := range []struct {
			x, y, z int32
			facing  string
		}{{1, 2, 1, "east"}, {3, 2, 1, "west"}, {1, 2, 5, "east"}, {3, 2, 5, "west"}} {
			if env.Random.NextFloat() < 0.1 {
				strongholdPutLocal(p, clip, &out, q.x, q.y, q.z, "wall_torch", map[string]any{"facing": strongholdDirection(p.dir, q.facing)})
			}
		}
		if p.leftLow {
			for y := int32(1); y <= 3; y++ {
				for z := int32(2); z <= 4; z++ {
					strongholdPutLocal(p, clip, &out, 0, y, z, "cave_air", nil)
				}
			}
		}
		if p.rightLow {
			for y := int32(1); y <= 3; y++ {
				for z := int32(2); z <= 4; z++ {
					strongholdPutLocal(p, clip, &out, 4, y, z, "cave_air", nil)
				}
			}
		}
	case "turn_left", "turn_right":
		cutX := int32(0)
		if (p.kind == "turn_left" && p.dir != "north" && p.dir != "east") || (p.kind == "turn_right" && (p.dir == "north" || p.dir == "east")) {
			cutX = 4
		}
		for y := int32(1); y <= 3; y++ {
			for z := int32(1); z <= 3; z++ {
				strongholdPutLocal(p, clip, &out, cutX, y, z, "cave_air", nil)
			}
		}
	case "prison":
		strongholdOpening(p, clip, &out, 1, 1, 10, 3, 3, 10)
		for _, z := range []int32{1, 3, 7, 9} {
			for y := int32(1); y <= 3; y++ {
				roll := env.Random.NextFloat()
				name := "stone_bricks"
				if roll < 0.2 {
					name = "cracked_stone_bricks"
				} else if roll < 0.5 {
					name = "mossy_stone_bricks"
				} else if roll < 0.55 {
					name = "infested_stone_bricks"
				}
				strongholdPutLocal(p, clip, &out, 4, y, z, name, nil)
			}
		}
		for y := int32(1); y <= 3; y++ {
			strongholdPutBars(p, clip, &out, 4, y, 4, true, true, false, false)
			strongholdPutBars(p, clip, &out, 4, y, 5, true, true, true, false)
			strongholdPutBars(p, clip, &out, 4, y, 6, true, true, false, false)
			for _, x := range []int32{5, 6, 7} {
				strongholdPutBars(p, clip, &out, x, y, 5, false, false, true, true)
			}
		}
		strongholdPutBars(p, clip, &out, 4, 3, 2, true, true, false, false)
		strongholdPutBars(p, clip, &out, 4, 3, 8, true, true, false, false)
		for _, z := range []int32{2, 8} {
			strongholdPutLocal(p, clip, &out, 4, 1, z, "iron_door", map[string]any{"facing": "west", "half": "lower", "hinge": "left", "open": false, "powered": false})
			strongholdPutLocal(p, clip, &out, 4, 2, z, "iron_door", map[string]any{"facing": "west", "half": "upper", "hinge": "left", "open": false, "powered": false})
		}
	case "library":
		height := int32(11)
		if b.Max[1]-b.Min[1]+1 <= 6 {
			height = 6
		}
		for y := int32(1); y <= 4; y++ {
			for x := int32(2); x <= 11; x++ {
				for z := int32(1); z <= 13; z++ {
					if env.Random.NextFloat() < 0.07 {
						strongholdPutLocal(p, clip, &out, x, y, z, "cobweb", nil)
					}
				}
			}
		}
		for d := int32(1); d <= 13; d++ {
			if (d-1)%4 == 0 {
				strongholdFixedBox(p, clip, &out, 1, 1, d, 1, 4, d, "oak_planks", nil)
				strongholdFixedBox(p, clip, &out, 12, 1, d, 12, 4, d, "oak_planks", nil)
				strongholdPutLocal(p, clip, &out, 2, 3, d, "wall_torch", map[string]any{"facing": "east"})
				strongholdPutLocal(p, clip, &out, 11, 3, d, "wall_torch", map[string]any{"facing": "west"})
				if height == 11 {
					strongholdFixedBox(p, clip, &out, 1, 6, d, 1, 9, d, "oak_planks", nil)
					strongholdFixedBox(p, clip, &out, 12, 6, d, 12, 9, d, "oak_planks", nil)
				}
			} else {
				strongholdFixedBox(p, clip, &out, 1, 1, d, 1, 4, d, "bookshelf", nil)
				strongholdFixedBox(p, clip, &out, 12, 1, d, 12, 4, d, "bookshelf", nil)
				if height == 11 {
					strongholdFixedBox(p, clip, &out, 1, 6, d, 1, 9, d, "bookshelf", nil)
					strongholdFixedBox(p, clip, &out, 12, 6, d, 12, 9, d, "bookshelf", nil)
				}
			}
		}
		for d := int32(3); d < 12; d += 2 {
			strongholdFixedBox(p, clip, &out, 3, 1, d, 4, 3, d, "bookshelf", nil)
			strongholdFixedBox(p, clip, &out, 6, 1, d, 7, 3, d, "bookshelf", nil)
			strongholdFixedBox(p, clip, &out, 9, 1, d, 10, 3, d, "bookshelf", nil)
		}
		if height == 11 {
			strongholdFixedBox(p, clip, &out, 1, 5, 1, 3, 5, 13, "oak_planks", nil)
			strongholdFixedBox(p, clip, &out, 10, 5, 1, 12, 5, 13, "oak_planks", nil)
			strongholdFixedBox(p, clip, &out, 4, 5, 1, 9, 5, 2, "oak_planks", nil)
			strongholdFixedBox(p, clip, &out, 4, 5, 12, 9, 5, 13, "oak_planks", nil)
			for _, q := range [][3]int32{{9, 5, 11}, {8, 5, 11}, {9, 5, 10}} {
				strongholdPutLocal(p, clip, &out, q[0], q[1], q[2], "oak_planks", nil)
			}
			for z := int32(3); z <= 11; z++ {
				strongholdPutFenceLocal(p, clip, &out, 3, 6, z, true, true, false, false)
			}
			for z := int32(3); z <= 9; z++ {
				strongholdPutFenceLocal(p, clip, &out, 10, 6, z, true, true, false, false)
			}
			for x := int32(4); x <= 9; x++ {
				strongholdPutFenceLocal(p, clip, &out, x, 6, 2, false, false, true, true)
			}
			for x := int32(4); x <= 7; x++ {
				strongholdPutFenceLocal(p, clip, &out, x, 6, 12, false, false, true, true)
			}
			strongholdPutFenceLocal(p, clip, &out, 3, 6, 2, true, false, true, false)
			strongholdPutFenceLocal(p, clip, &out, 3, 6, 12, false, true, true, false)
			strongholdPutFenceLocal(p, clip, &out, 10, 6, 2, true, false, false, true)
			for i := int32(0); i <= 2; i++ {
				strongholdPutFenceLocal(p, clip, &out, 8+i, 6, 12-i, false, true, false, true)
				if i != 2 {
					strongholdPutFenceLocal(p, clip, &out, 8+i, 6, 11-i, true, false, true, false)
				}
			}
			for y := int32(1); y <= 7; y++ {
				strongholdPutLocal(p, clip, &out, 10, y, 13, "ladder", map[string]any{"facing": strongholdDirection(p.dir, "south"), "waterlogged": false})
			}
			strongholdPutFenceLocal(p, clip, &out, 6, 9, 7, false, false, true, false)
			strongholdPutFenceLocal(p, clip, &out, 7, 9, 7, false, false, false, true)
			strongholdPutFenceLocal(p, clip, &out, 6, 8, 7, false, false, true, false)
			strongholdPutFenceLocal(p, clip, &out, 7, 8, 7, false, false, false, true)
			strongholdPutFenceLocal(p, clip, &out, 6, 7, 7, true, true, true, true)
			strongholdPutFenceLocal(p, clip, &out, 7, 7, 7, true, true, true, true)
			strongholdPutFenceLocal(p, clip, &out, 5, 7, 7, false, false, true, false)
			strongholdPutFenceLocal(p, clip, &out, 8, 7, 7, false, false, false, true)
			strongholdPutFenceLocal(p, clip, &out, 6, 7, 6, true, false, true, false)
			strongholdPutFenceLocal(p, clip, &out, 6, 7, 8, false, true, true, false)
			strongholdPutFenceLocal(p, clip, &out, 7, 7, 6, true, false, false, true)
			strongholdPutFenceLocal(p, clip, &out, 7, 7, 8, false, true, false, true)
			for _, q := range [][3]int32{{5, 8, 7}, {8, 8, 7}, {6, 8, 6}, {6, 8, 8}, {7, 8, 6}, {7, 8, 8}} {
				strongholdPutLocal(p, clip, &out, q[0], q[1], q[2], "torch", nil)
			}
		}
		strongholdPutLootLocal(p, clip, env, &out, 3, 3, 5, "minecraft:chests/stronghold_library", env.Random)
		if height == 11 {
			strongholdPutLocal(p, clip, &out, 12, 9, 1, "cave_air", nil)
			strongholdPutLootLocal(p, clip, env, &out, 12, 8, 1, "minecraft:chests/stronghold_library", env.Random)
		}
	case "portal":
		strongholdSelectedBox(p, clip, &out, env.Random, 1, 6, 1, 1, 6, 14)
		strongholdSelectedBox(p, clip, &out, env.Random, 9, 6, 1, 9, 6, 14)
		strongholdSelectedBox(p, clip, &out, env.Random, 2, 6, 1, 8, 6, 2)
		strongholdSelectedBox(p, clip, &out, env.Random, 2, 6, 14, 8, 6, 14)
		strongholdSelectedBox(p, clip, &out, env.Random, 1, 1, 1, 2, 1, 4)
		strongholdSelectedBox(p, clip, &out, env.Random, 8, 1, 1, 9, 1, 4)
		strongholdFixedBox(p, clip, &out, 1, 1, 1, 1, 1, 3, "lava", nil)
		strongholdFixedBox(p, clip, &out, 9, 1, 1, 9, 1, 3, "lava", nil)
		strongholdSelectedBox(p, clip, &out, env.Random, 3, 1, 8, 7, 1, 12)
		strongholdFixedBox(p, clip, &out, 4, 1, 9, 6, 1, 11, "lava", nil)
		for z := int32(3); z < 14; z += 2 {
			strongholdPutBars(p, clip, &out, 0, 3, z, true, true, false, false)
			strongholdPutBars(p, clip, &out, 0, 4, z, true, true, false, false)
			strongholdPutBars(p, clip, &out, 10, 3, z, true, true, false, false)
			strongholdPutBars(p, clip, &out, 10, 4, z, true, true, false, false)
		}
		for x := int32(2); x < 9; x += 2 {
			strongholdPutBars(p, clip, &out, x, 3, 15, false, false, true, true)
			strongholdPutBars(p, clip, &out, x, 4, 15, false, false, true, true)
		}
		strongholdSelectedBox(p, clip, &out, env.Random, 4, 1, 5, 6, 1, 7)
		strongholdSelectedBox(p, clip, &out, env.Random, 4, 2, 6, 6, 2, 7)
		strongholdSelectedBox(p, clip, &out, env.Random, 4, 3, 7, 6, 3, 7)
		for x := int32(4); x <= 6; x++ {
			for _, q := range [][3]int32{{x, 1, 4}, {x, 2, 5}, {x, 3, 6}} {
				strongholdPutLocal(p, clip, &out, q[0], q[1], q[2], "stone_brick_stairs", map[string]any{"facing": strongholdDirection(p.dir, "north"), "half": "bottom", "shape": "straight", "waterlogged": false})
			}
		}
		allEyes := true
		coords := [12][3]int32{{4, 3, 8}, {5, 3, 8}, {6, 3, 8}, {4, 3, 12}, {5, 3, 12}, {6, 3, 12}, {3, 3, 9}, {3, 3, 10}, {3, 3, 11}, {7, 3, 9}, {7, 3, 10}, {7, 3, 11}}
		facings := [12]string{"north", "north", "north", "south", "south", "south", "east", "east", "east", "west", "west", "west"}
		eyes := [12]bool{}
		for i := range 12 {
			eyes[i] = env.Random.NextFloat() > 0.9
			allEyes = allEyes && eyes[i]
		}
		for i, coord := range coords {
			strongholdPutLocal(p, clip, &out, coord[0], coord[1], coord[2], "end_portal_frame", map[string]any{"facing": strongholdDirection(p.dir, facings[i]), "eye": eyes[i]})
		}
		if allEyes {
			for x := int32(4); x <= 6; x++ {
				for z := int32(9); z <= 11; z++ {
					strongholdPutLocal(p, clip, &out, x, 3, z, "end_portal", nil)
				}
			}
		}
		if !env.PieceState.SpawnerPlaced {
			spawner := strongholdLocal(b, p.dir, 5, 3, 6)
			if journal.inside(spawner[0], spawner[1], spawner[2]) {
				env.PieceState.SpawnerPlaced = true
				if journal.set(spawner[0], spawner[1], spawner[2], world.BlockState{Name: "minecraft:spawner"}) {
					out.Blocks[len(out.Blocks)-1].NBT = map[string]any{"id": "minecraft:mob_spawner", "SpawnData": map[string]any{"entity": map[string]any{"id": "minecraft:silverfish"}}}
				}
			}
		}
	case "chest":
		strongholdOpening(p, clip, &out, 1, 1, 6, 3, 3, 6)
		strongholdFixedBox(p, clip, &out, 3, 1, 2, 3, 1, 4, "stone_bricks", nil)
		strongholdPutLocal(p, clip, &out, 3, 1, 1, "stone_brick_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdPutLocal(p, clip, &out, 3, 1, 5, "stone_brick_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdPutLocal(p, clip, &out, 3, 2, 2, "stone_brick_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdPutLocal(p, clip, &out, 3, 2, 4, "stone_brick_slab", map[string]any{"type": "bottom", "waterlogged": false})
		for z := int32(2); z <= 4; z++ {
			strongholdPutLocal(p, clip, &out, 2, 1, z, "stone_brick_slab", map[string]any{"type": "bottom", "waterlogged": false})
		}
		chestPos := strongholdLocal(b, p.dir, 3, 2, 3)
		if !env.PieceState.ChestPlaced && journal.inside(chestPos[0], chestPos[1], chestPos[2]) {
			env.PieceState.ChestPlaced = true
			strongholdPutLootLocal(p, clip, env, &out, 3, 2, 3, "minecraft:chests/stronghold_corridor", env.Random)
		}
	case "crossing":
		strongholdOpening(p, clip, &out, 4, 1, 10, 6, 3, 10)
		strongholdOpening(p, clip, &out, 0, 1, 4, 0, 3, 6)
		strongholdOpening(p, clip, &out, 10, 1, 4, 10, 3, 6)
		switch p.variant {
		case 0:
			for y := int32(1); y <= 3; y++ {
				strongholdPutLocal(p, clip, &out, 5, y, 5, "stone_bricks", nil)
			}
			for _, q := range []struct {
				x, y, z int32
				facing  string
			}{{4, 3, 5, "west"}, {6, 3, 5, "east"}, {5, 3, 4, "south"}, {5, 3, 6, "north"}} {
				strongholdPutLocal(p, clip, &out, q.x, q.y, q.z, "wall_torch", map[string]any{"facing": strongholdDirection(p.dir, q.facing)})
			}
			for _, q := range [][3]int32{{4, 1, 4}, {4, 1, 5}, {4, 1, 6}, {6, 1, 4}, {6, 1, 5}, {6, 1, 6}, {5, 1, 4}, {5, 1, 6}} {
				strongholdPutLocal(p, clip, &out, q[0], q[1], q[2], "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
			}
		case 1:
			for z := int32(3); z <= 7; z++ {
				strongholdPutLocal(p, clip, &out, 3, 1, z, "stone_bricks", nil)
				strongholdPutLocal(p, clip, &out, 7, 1, z, "stone_bricks", nil)
				strongholdPutLocal(p, clip, &out, z, 1, 3, "stone_bricks", nil)
				strongholdPutLocal(p, clip, &out, z, 1, 7, "stone_bricks", nil)
			}
			for y := int32(1); y <= 3; y++ {
				strongholdPutLocal(p, clip, &out, 5, y, 5, "stone_bricks", nil)
			}
			strongholdPutLocal(p, clip, &out, 5, 4, 5, "water", nil)
		case 2:
			for z := int32(1); z <= 9; z++ {
				strongholdPutLocal(p, clip, &out, 1, 3, z, "cobblestone", nil)
				strongholdPutLocal(p, clip, &out, 9, 3, z, "cobblestone", nil)
			}
			for x := int32(1); x <= 9; x++ {
				strongholdPutLocal(p, clip, &out, x, 3, 1, "cobblestone", nil)
				strongholdPutLocal(p, clip, &out, x, 3, 9, "cobblestone", nil)
			}
			for _, q := range [][3]int32{{5, 1, 4}, {5, 1, 6}, {5, 3, 4}, {5, 3, 6}, {4, 1, 5}, {6, 1, 5}, {4, 3, 5}, {6, 3, 5}} {
				strongholdPutLocal(p, clip, &out, q[0], q[1], q[2], "cobblestone", nil)
			}
			for y := int32(1); y <= 3; y++ {
				for _, x := range []int32{4, 6} {
					for _, z := range []int32{4, 6} {
						strongholdPutLocal(p, clip, &out, x, y, z, "cobblestone", nil)
					}
				}
			}
			strongholdPutLocal(p, clip, &out, 5, 3, 5, "wall_torch", map[string]any{"facing": strongholdDirection(p.dir, "north")})
			for z := int32(2); z <= 8; z++ {
				for _, x := range []int32{2, 3, 7, 8} {
					strongholdPutLocal(p, clip, &out, x, 3, z, "oak_planks", nil)
				}
				if z <= 3 || z >= 7 {
					for x := int32(4); x <= 6; x++ {
						strongholdPutLocal(p, clip, &out, x, 3, z, "oak_planks", nil)
					}
				}
			}
			for y := int32(1); y <= 3; y++ {
				strongholdPutLocal(p, clip, &out, 9, y, 3, "ladder", map[string]any{"facing": strongholdDirection(p.dir, "west"), "waterlogged": false})
			}
			strongholdPutLootLocal(p, clip, env, &out, 3, 4, 8, "minecraft:chests/stronghold_crossing", env.Random)
		}
	case "five":
		if p.leftLow {
			strongholdOpening(p, clip, &out, 0, 3, 1, 0, 5, 3)
		}
		if p.rightLow {
			strongholdOpening(p, clip, &out, 9, 3, 1, 9, 5, 3)
		}
		if p.leftHigh {
			strongholdOpening(p, clip, &out, 0, 5, 7, 0, 7, 9)
		}
		if p.rightHigh {
			strongholdOpening(p, clip, &out, 9, 5, 7, 9, 7, 9)
		}
		strongholdOpening(p, clip, &out, 5, 1, 10, 7, 3, 10)
		strongholdSelectedBox(p, clip, &out, env.Random, 1, 2, 1, 8, 2, 6)
		strongholdSelectedBox(p, clip, &out, env.Random, 4, 1, 5, 4, 4, 9)
		strongholdSelectedBox(p, clip, &out, env.Random, 8, 1, 5, 8, 4, 9)
		strongholdSelectedBox(p, clip, &out, env.Random, 1, 4, 7, 3, 4, 9)
		strongholdSelectedBox(p, clip, &out, env.Random, 1, 3, 5, 3, 3, 6)
		strongholdFixedBox(p, clip, &out, 1, 3, 4, 3, 3, 4, "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdFixedBox(p, clip, &out, 1, 4, 6, 3, 4, 6, "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdSelectedBox(p, clip, &out, env.Random, 5, 1, 7, 7, 1, 8)
		strongholdFixedBox(p, clip, &out, 5, 1, 9, 7, 1, 9, "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdFixedBox(p, clip, &out, 5, 2, 7, 7, 2, 7, "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdFixedBox(p, clip, &out, 4, 5, 7, 4, 5, 9, "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdFixedBox(p, clip, &out, 8, 5, 7, 8, 5, 9, "smooth_stone_slab", map[string]any{"type": "bottom", "waterlogged": false})
		strongholdFixedBox(p, clip, &out, 5, 5, 7, 7, 5, 9, "smooth_stone_slab", map[string]any{"type": "double", "waterlogged": false})
		strongholdPutLocal(p, clip, &out, 6, 5, 6, "wall_torch", map[string]any{"facing": "south"})
	}
	return out, nil
}

func strongholdFixedBox(p strongholdPiece, clip javastructure.Bounds, out *javastructure.Placement, x0, y0, z0, x1, y1, z1 int32, name string, props map[string]any) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				strongholdPutLocal(p, clip, out, x, y, z, name, props)
			}
		}
	}
}

func strongholdLocal(b javastructure.Box,dir string,x,y,z int32)[3]int32{
	switch dir{case "north":return [3]int32{b.Min[0]+x,b.Min[1]+y,b.Max[2]-z};case "south":return [3]int32{b.Min[0]+x,b.Min[1]+y,b.Min[2]+z};case "west":return [3]int32{b.Max[0]-z,b.Min[1]+y,b.Min[2]+x};default:return [3]int32{b.Min[0]+z,b.Min[1]+y,b.Min[2]+x}}
}
func strongholdOpening(p strongholdPiece, clip javastructure.Bounds, out *javastructure.Placement, x0, y0, z0, x1, y1, z1 int32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				strongholdPutLocal(p, clip, out, x, y, z, "cave_air", nil)
			}
		}
	}
}

func strongholdDirection(_, local string) string { return local }
func strongholdRotateProperties(dir, name string, properties map[string]any) map[string]any {
	if len(properties) == 0 || dir == "north" {
		return properties
	}
	rotated := make(map[string]any, len(properties))
	for key, value := range properties {
		if key == "facing" {
			if facing, ok := value.(string); ok {
				value = strongholdRotateDirection(dir, facing)
			}
		}
		if key == "hinge" && (dir == "south" || dir == "west") {
			if hinge, ok := value.(string); ok {
				if hinge == "left" {
					value = "right"
				} else if hinge == "right" {
					value = "left"
				}
			}
		}
		target := key
		if key == "north" || key == "east" || key == "south" || key == "west" {
			target = strongholdRotateDirection(dir, key)
		}
		rotated[target] = value
	}
	return rotated
}
func strongholdRotateDirection(dir, local string) string {
	if dir == "south" {
		switch local {
		case "north":
			return "south"
		case "south":
			return "north"
		default:
			return local
		}
	}
	if dir == "west" {
		switch local {
		case "north":
			return "west"
		case "south":
			return "east"
		case "east":
			return "south"
		case "west":
			return "north"
		}
	}
	order := [4]string{"north", "east", "south", "west"}
	start := 0
	for i, d := range order {
		if d == local {
			start = i
			break
		}
	}
	offset := 0
	if dir == "east" {
		offset = 1
	}
	return order[(start+offset)%len(order)]
}
func strongholdPutLocal(p strongholdPiece, clip javastructure.Bounds, out *javastructure.Placement, x, y, z int32, name string, props map[string]any) {
	pos := strongholdLocal(p.box, p.dir, x, y, z)
	if props == nil && (name == "water" || name == "lava") {
		props = map[string]any{"level": int32(0)}
	}
	props = strongholdRotateProperties(p.dir, name, props)
	if p.journal != nil {
		if p.journal.set(pos[0], pos[1], pos[2], world.BlockState{Name: "minecraft:" + name, Properties: props}) && strongholdShapeCheckBlock(name) {
			out.PostProcessing = append(out.PostProcessing, cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])})
		}
		return
	}
	if pos[0] < clip.Min[0] || pos[0] > clip.Max[0] || pos[1] < clip.Min[1] || pos[1] > clip.Max[1] || pos[2] < clip.Min[2] || pos[2] > clip.Max[2] {
		return
	}
	out.Blocks = append(out.Blocks, javastructure.Block{Pos: pos, State: world.BlockState{Name: "minecraft:" + name, Properties: props}})
}
func strongholdShapeCheckBlock(name string) bool {
	switch name {
	case "torch", "wall_torch", "oak_fence", "ladder", "iron_bars":
		return true
	default:
		return false
	}
}
func strongholdPutBars(p strongholdPiece, clip javastructure.Bounds, out *javastructure.Placement, x, y, z int32, north, south, east, west bool) {
	strongholdPutLocal(p, clip, out, x, y, z, "iron_bars", map[string]any{"north": north, "south": south, "east": east, "west": west, "waterlogged": false})
}
func strongholdPutFenceLocal(p strongholdPiece, clip javastructure.Bounds, out *javastructure.Placement, x, y, z int32, north, south, east, west bool) {
	strongholdPutLocal(p, clip, out, x, y, z, "oak_fence", map[string]any{"north": north, "south": south, "east": east, "west": west, "waterlogged": false})
}
func strongholdSelectedBox(p strongholdPiece,clip javastructure.Bounds,out *javastructure.Placement,random *java26.Random,x0,y0,z0,x1,y1,z1 int32){
	for y:=y0;y<=y1;y++{for x:=x0;x<=x1;x++{for z:=z0;z<=z1;z++{edge:=x==x0||x==x1||y==y0||y==y1||z==z0||z==z1;if !edge{strongholdPutLocal(p,clip,out,x,y,z,"cave_air",nil);continue};roll:=random.NextFloat();name:="stone_bricks";if roll<0.2{name="cracked_stone_bricks"}else if roll<0.5{name="mossy_stone_bricks"}else if roll<0.55{name="infested_stone_bricks"};strongholdPutLocal(p,clip,out,x,y,z,name,nil)}}}
}
func strongholdPutLootLocal(p strongholdPiece, clip javastructure.Bounds, env javastructure.ProcessorEnvironment, out *javastructure.Placement, x, y, z int32, loot string, random *java26.Random) {
	pos := strongholdLocal(p.box, p.dir, x, y, z)
	if pos[0] < clip.Min[0] || pos[0] > clip.Max[0] || pos[1] < clip.Min[1] || pos[1] > clip.Max[1] || pos[2] < clip.Min[2] || pos[2] > clip.Max[2] {
		return
	}
	if name, ok := javadata.BlockStateName(p.journal.stateID(pos[0], pos[1], pos[2])); ok && name == "minecraft:chest" {
		return
	}
	facing := "north"
	directions := [4]struct {
		dx, dz         int32
		name, opposite string
	}{{0, -1, "north", "south"}, {1, 0, "east", "west"}, {0, 1, "south", "north"}, {-1, 0, "west", "east"}}
	var solid *string
	for _, direction := range directions {
		neighbor := [3]int32{pos[0] + direction.dx, pos[1], pos[2] + direction.dz}
		name, ok := javadata.BlockStateName(p.journal.stateID(neighbor[0], neighbor[1], neighbor[2]))
		if ok && name == "minecraft:chest" {
			solid = nil
			break
		}
		if p.journal.shape(neighbor[0], neighbor[1], neighbor[2]).SolidRender {
			if solid != nil {
				solid = nil
				break
			}
			opposite := direction.opposite
			solid = &opposite
		}
	}
	if solid != nil {
		facing = *solid
	} else {
		lock := "north"
		solidAt := func(name string) bool {
			for _, d := range directions {
				if d.name == name {
					n := [3]int32{pos[0] + d.dx, pos[1], pos[2] + d.dz}
					return p.journal.shape(n[0], n[1], n[2]).SolidRender
				}
			}
			return false
		}
		if solidAt(lock) {
			lock = "south"
		}
		if solidAt(lock) {
			lock = "east"
		}
		if solidAt(lock) {
			lock = "west"
		}
		facing = lock
	}
	block := world.BlockState{Name: "minecraft:chest", Properties: map[string]any{"facing": facing, "type": "single", "waterlogged": false}}
	if p.journal != nil && p.journal.set(pos[0], pos[1], pos[2], block) {
		out.Blocks[len(out.Blocks)-1].NBT = map[string]any{"id": "minecraft:chest", "LootTable": loot, "LootTableSeed": random.NextLong()}
	}
}
func strongholdDoorPlacement(p strongholdPiece, clip javastructure.Bounds, out *javastructure.Placement) {
	footX := int32(1)
	footY := int32(1)
	if p.kind == "crossing" || p.kind == "library" || p.kind == "five" || p.kind == "portal" {
		footX = 4
	}
	if p.kind == "five" {
		footY = 3
	}
	if p.kind == "start" || p.kind == "stairs" || p.kind == "straight_stairs" {
		footY = 7
	}
	put := func(x, y, z int32, name string, props map[string]any) {
		strongholdPutLocal(p, clip, out, x+footX-1, y+footY-1, z, name, props)
	}
	if p.door == "opening" || p.door == "" {
		for y := int32(1); y <= 3; y++ {
			for x := int32(1); x <= 3; x++ {
				put(x, y, 0, "cave_air", nil)
			}
		}
		return
	}
	if p.door == "wood" || p.door == "iron" {
		put(1, 1, 0, "stone_bricks", nil)
		put(1, 2, 0, "stone_bricks", nil)
		put(1, 3, 0, "stone_bricks", nil)
		put(2, 3, 0, "stone_bricks", nil)
		put(3, 3, 0, "stone_bricks", nil)
		put(3, 2, 0, "stone_bricks", nil)
		put(3, 1, 0, "stone_bricks", nil)
	}
	switch p.door {
	case "wood":
		put(2, 1, 0, "oak_door", map[string]any{"facing": "north", "half": "lower", "hinge": "left", "open": false, "powered": false})
		put(2, 2, 0, "oak_door", map[string]any{"facing": "north", "half": "upper", "hinge": "left", "open": false, "powered": false})
	case "grates":
		put(2, 1, 0, "cave_air", nil)
		put(2, 2, 0, "cave_air", nil)
		put(1, 1, 0, "iron_bars", map[string]any{"east": false, "north": false, "south": false, "west": true, "waterlogged": false})
		put(1, 2, 0, "iron_bars", map[string]any{"east": false, "north": false, "south": false, "west": true, "waterlogged": false})
		put(1, 3, 0, "iron_bars", map[string]any{"east": true, "north": false, "south": false, "west": true, "waterlogged": false})
		put(2, 3, 0, "iron_bars", map[string]any{"east": true, "north": false, "south": false, "west": true, "waterlogged": false})
		put(3, 3, 0, "iron_bars", map[string]any{"east": true, "north": false, "south": false, "west": true, "waterlogged": false})
		put(3, 2, 0, "iron_bars", map[string]any{"east": true, "north": false, "south": false, "west": false, "waterlogged": false})
		put(3, 1, 0, "iron_bars", map[string]any{"east": true, "north": false, "south": false, "west": false, "waterlogged": false})
	case "iron":
		put(2, 1, 0, "iron_door", map[string]any{"facing": "north", "half": "lower", "hinge": "left", "open": false, "powered": false})
		put(2, 2, 0, "iron_door", map[string]any{"facing": "north", "half": "upper", "hinge": "left", "open": false, "powered": false})
		put(3, 2, 1, "stone_button", map[string]any{"face": "wall", "facing": "north", "powered": false})
		put(3, 2, -1, "stone_button", map[string]any{"face": "wall", "facing": "south", "powered": false})
	}
}
