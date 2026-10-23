package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func init() { registerModernStructureBuilder("swamp_hut", buildModernSwampHut) }

const (
	swampHutWitch uint8 = 1 << iota
	swampHutCat
)

type modernSwampHutPiece struct {
	box       structure.Box
	direction string
	seed      int64
}

func (p modernSwampHutPiece) Bounds() structure.Box { return p.box }
func (p modernSwampHutPiece) Place(clip structure.Bounds, env structure.ProcessorEnvironment) (structure.Placement, error) {
	out := structure.Placement{}
	var temporary structure.StructurePieceState
	if env.PieceState == nil {
		env.PieceState = &temporary
	}
	ready, err := updateScatteredGroundHeight(&p.box, clip, env)
	if err != nil || !ready {
		return out, err
	}
	journal := newJournalStructureWorld(env, clip, &out)
	put := func(x, y, z int32, name string, props map[string]any) {
		journal.placeSourcePiece(p.worldPos(x, y, z), world.BlockState{Name: "minecraft:" + name, Properties: props}, p.direction)
	}
	box := func(x0, y0, z0, x1, y1, z1 int32, name string, props map[string]any) {
		state := sourcePieceState(world.BlockState{Name: "minecraft:" + name, Properties: props}, p.direction)
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				for z := z0; z <= z1; z++ {
					journal.placeSourceState(p.worldPos(x, y, z), state)
				}
			}
		}
	}
	box(1, 1, 1, 5, 1, 7, "spruce_planks", nil)
	box(1, 4, 2, 5, 4, 7, "spruce_planks", nil)
	box(2, 1, 0, 4, 1, 0, "spruce_planks", nil)
	box(2, 2, 2, 3, 3, 2, "spruce_planks", nil)
	box(1, 2, 3, 1, 3, 6, "spruce_planks", nil)
	box(5, 2, 3, 5, 3, 6, "spruce_planks", nil)
	box(2, 2, 7, 4, 3, 7, "spruce_planks", nil)
	for _, pos := range [4][3]int32{{1, 0, 2}, {5, 0, 2}, {1, 0, 7}, {5, 0, 7}} {
		box(pos[0], pos[1], pos[2], pos[0], 3, pos[2], "oak_log", nil)
	}
	put(2, 3, 2, "oak_fence", nil)
	put(3, 3, 7, "oak_fence", nil)
	put(1, 3, 4, "air", nil)
	put(5, 3, 4, "air", nil)
	put(5, 3, 5, "air", nil)
	put(1, 3, 5, "potted_red_mushroom", nil)
	put(3, 2, 6, "crafting_table", nil)
	put(4, 2, 6, "cauldron", nil)
	put(1, 2, 1, "oak_fence", nil)
	put(5, 2, 1, "oak_fence", nil)
	stair := func(facing, shape string) map[string]any {
		return map[string]any{"facing": facing, "half": "bottom", "shape": shape, "waterlogged": false}
	}
	box(0, 4, 1, 6, 4, 1, "spruce_stairs", stair("north", "straight"))
	box(0, 4, 2, 0, 4, 7, "spruce_stairs", stair("east", "straight"))
	box(6, 4, 2, 6, 4, 7, "spruce_stairs", stair("west", "straight"))
	box(0, 4, 8, 6, 4, 8, "spruce_stairs", stair("south", "straight"))
	put(0, 4, 1, "spruce_stairs", stair("north", "outer_right"))
	put(6, 4, 1, "spruce_stairs", stair("north", "outer_left"))
	put(0, 4, 8, "spruce_stairs", stair("south", "outer_left"))
	put(6, 4, 8, "spruce_stairs", stair("south", "outer_right"))

	// fillColumnDown checks the initial clip once, then writes downward using
	// live state even below that clip. It stops above minY+1, not at minY.
	for _, z := range [2]int32{2, 7} {
		for _, x := range [2]int32{1, 5} {
			pos := p.worldPos(x, -1, z)
			if !journal.inside(pos[0], pos[1], pos[2]) {
				continue
			}
			for pos[1] > int32(env.Range.Min())+1 && sourceStructureReplaceable(journal.blockName(pos)) {
				journal.setUnclipped(pos, world.BlockState{Name: "minecraft:oak_log"})
				pos[1]--
			}
		}
	}

	pos := p.worldPos(2, 2, 5)
	flags := env.PieceState
	if journal.inside(pos[0], pos[1], pos[2]) {
		if flags.EntitiesSpawned != swampHutWitch|swampHutCat && env.LevelRandom == nil {
			return out, fmt.Errorf("swamp hut mob finalization requires WorldGenRegion random")
		}
		if flags.EntitiesSpawned&swampHutWitch == 0 {
			flags.EntitiesSpawned |= swampHutWitch
			out.Entities = append(out.Entities, structure.Entity{
				Pos: [3]float64{float64(pos[0]) + 0.5, float64(pos[1]), float64(pos[2]) + 0.5}, BlockPos: pos, Seed: p.seed,
				NBT: scatteredMobSpawnNBT("minecraft:witch", 26, env.LevelRandom),
			})
		}
		if flags.EntitiesSpawned&swampHutCat == 0 {
			flags.EntitiesSpawned |= swampHutCat
			data := scatteredMobSpawnNBT("minecraft:cat", 10, env.LevelRandom)
			// The containing valid hut matches CATS_SPAWN_AS_BLACK at priority 1:
			// even this singleton selection calls Util.getRandomSafe.nextInt(1).
			env.LevelRandom.NextInt(1)
			sound := [2]string{"minecraft:classic", "minecraft:royal"}[env.LevelRandom.NextInt(2)]
			data["variant"], data["sound_variant"], data["CollarColor"] = "minecraft:all_black", sound, int32(14)
			out.Entities = append(out.Entities, structure.Entity{
				Pos: [3]float64{float64(pos[0]) + 0.5, float64(pos[1]), float64(pos[2]) + 0.5}, BlockPos: pos, Seed: p.seed,
				NBT: data,
			})
		}
	}
	return out, nil
}

func (p modernSwampHutPiece) worldPos(x, y, z int32) [3]int32 {
	b := p.box
	switch p.direction {
	case "north":
		return [3]int32{b.Min[0] + x, b.Min[1] + y, b.Max[2] - z}
	case "south":
		return [3]int32{b.Min[0] + x, b.Min[1] + y, b.Min[2] + z}
	case "west":
		return [3]int32{b.Max[0] - z, b.Min[1] + y, b.Min[2] + x}
	default:
		return [3]int32{b.Min[0] + z, b.Min[1] + y, b.Min[2] + x}
	}
}

func sourceStructureReplaceable(name string) bool {
	switch name {
	case "minecraft:air", "minecraft:cave_air", "minecraft:void_air", "minecraft:water", "minecraft:lava", "minecraft:glow_lichen", "minecraft:seagrass", "minecraft:tall_seagrass":
		return true
	}
	return false
}

func buildModernSwampHut(in modernStructureBuildInput) (modernStructureStart, bool, error) {
	if in.random == nil {
		return modernStructureStart{}, false, fmt.Errorf("swamp hut requires structure random")
	}
	direction := [4]string{"north", "east", "south", "west"}[in.random.NextInt(4)]
	minX, minZ := in.chunkX*16, in.chunkZ*16
	width, depth := int32(7), int32(9)
	if direction == "east" || direction == "west" {
		width, depth = depth, width
	}
	box := structure.Box{Min: [3]int32{minX, 64, minZ}, Max: [3]int32{minX + width - 1, 70, minZ + depth - 1}}
	piece := modernSwampHutPiece{box: box, direction: direction, seed: in.seed}
	return modernStructureStart{name: in.name, pieces: []modernStructurePiece{piece}, box: box, terrain: terrainAdjustment(stringValue(in.config["terrain_adaptation"]))}, true, nil
}
