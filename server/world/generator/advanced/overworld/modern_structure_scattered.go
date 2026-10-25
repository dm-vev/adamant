package overworld

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

// mutableStructurePieceBounds reconstructs the source's moved piece without making
// the cached constructor graph mutable. StructureStart's cached bounds stay put.
func mutableStructurePieceBounds(piece modernStructurePiece, flags *structure.StructurePieceState) structure.Box {
	box := piece.Bounds()
	if flags == nil {
		return box
	}
	if flags.BoundingBoxYSet {
		box.Max[1] += flags.BoundingBoxMinY - box.Min[1]
		box.Min[1] = flags.BoundingBoxMinY
		return box
	}
	if !flags.GroundHeightSet {
		return box
	}
	switch piece.(type) {
	case modernJungleTemple, modernSwampHutPiece:
		box.Max[1] += flags.GroundHeight - box.Min[1]
		box.Min[1] = flags.GroundHeight
	}
	return box
}

// ScatteredFeaturePiece.updateAverageGroundHeight samples (x,64,z) only in
// the first intersecting clip. Negative HPos is not a cache hit in Java.
func updateScatteredGroundHeight(box *structure.Box, clip structure.Bounds, env structure.ProcessorEnvironment) (bool, error) {
	flags := env.PieceState
	if flags != nil && flags.GroundHeightSet {
		box.Max[1] += flags.GroundHeight - box.Min[1]
		box.Min[1] = flags.GroundHeight
		if flags.GroundHeight >= 0 {
			return true, nil
		}
	}
	if env.MotionBlockingNoLeavesHeight == nil {
		return false, fmt.Errorf("scattered piece requires live MOTION_BLOCKING_NO_LEAVES heightmap")
	}
	var total, count int32
	if clip.Min[1] <= 64 && clip.Max[1] >= 64 {
		for z := box.Min[2]; z <= box.Max[2]; z++ {
			for x := box.Min[0]; x <= box.Max[0]; x++ {
				if x >= clip.Min[0] && x <= clip.Max[0] && z >= clip.Min[2] && z <= clip.Max[2] {
					total += env.MotionBlockingNoLeavesHeight(x, z)
					count++
				}
			}
		}
	}
	if count == 0 {
		return false, nil
	}
	height := total / count
	box.Max[1] += height - box.Min[1]
	box.Min[1] = height
	if flags != nil {
		flags.GroundHeightSet, flags.GroundHeight = true, height
	}
	return true, nil
}

func sourcePieceState(state world.BlockState, direction string) world.BlockState {
	if direction == "north" || state.Properties == nil {
		return state
	}
	mirror, rotation := structure.NoMirror, structure.None
	if direction == "south" || direction == "west" {
		mirror = structure.LeftRight
	}
	if direction == "east" || direction == "west" {
		rotation = structure.Clockwise90
	}
	transformed := world.TransformState(state, mirror, rotation)
	// StairBlock.mirror only exchanges left/right shapes on the mirrored axis.
	if mirror != structure.NoMirror && strings.HasSuffix(state.Name, "_stairs") {
		if facing, _ := state.Properties["facing"].(string); facing == "east" || facing == "west" {
			if shape, ok := state.Properties["shape"]; ok {
				transformed.Properties["shape"] = shape
			}
		}
	}
	return transformed
}

func (w *journalStructureWorld) placeSourcePiece(pos [3]int32, state world.BlockState, direction string) bool {
	return w.placeSourceState(pos, sourcePieceState(state, direction))
}

func (w *journalStructureWorld) placeSourceState(pos [3]int32, state world.BlockState) bool {
	if !w.inside(pos[0], pos[1], pos[2]) {
		return false
	}
	written := w.set(pos[0], pos[1], pos[2], state)
	if sourcePieceShapeCheckBlock(state.Name) {
		w.placement.PostProcessing = append(w.placement.PostProcessing, cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])})
	}
	return written
}

func sourcePieceShapeCheckBlock(name string) bool {
	switch name {
	case "minecraft:nether_brick_fence", "minecraft:torch", "minecraft:wall_torch", "minecraft:oak_fence", "minecraft:spruce_fence", "minecraft:dark_oak_fence", "minecraft:pale_oak_fence", "minecraft:acacia_fence", "minecraft:birch_fence", "minecraft:jungle_fence", "minecraft:ladder", "minecraft:iron_bars":
		return true
	}
	return false
}

// Source createChest returns true after attempting a write, not after receiving
// a block entity. An occupied chest returns false and consumes no loot RNG.
func (w *journalStructureWorld) createSourceChest(pos [3]int32, table string, random *vanilla.Random) bool {
	if !w.inside(pos[0], pos[1], pos[2]) || w.blockName(pos) == "minecraft:chest" {
		return false
	}
	state := world.BlockState{Name: "minecraft:chest", Properties: map[string]any{"facing": w.sourceChestFacing(pos), "type": "single", "waterlogged": false}}
	if w.set(pos[0], pos[1], pos[2], state) {
		w.placement.Blocks[len(w.placement.Blocks)-1].NBT = map[string]any{"id": "minecraft:chest", "LootTable": table, "LootTableSeed": random.NextLong()}
	}
	return true
}

func (w *journalStructureWorld) createSourceDispenser(pos [3]int32, facing, direction, table string, random *vanilla.Random) bool {
	if !w.inside(pos[0], pos[1], pos[2]) || w.blockName(pos) == "minecraft:dispenser" {
		return false
	}
	state := world.BlockState{Name: "minecraft:dispenser", Properties: map[string]any{"facing": facing, "triggered": false}}
	if w.placeSourcePiece(pos, state, direction) {
		w.placement.Blocks[len(w.placement.Blocks)-1].NBT = map[string]any{"id": "minecraft:dispenser", "LootTable": table, "LootTableSeed": random.NextLong()}
	}
	return true
}

func (w *journalStructureWorld) blockName(pos [3]int32) string {
	name, _ := gamedata.BlockStateName(w.stateID(pos[0], pos[1], pos[2]))
	return name
}

func (w *journalStructureWorld) sourceChestFacing(pos [3]int32) string {
	// Direction.Plane.HORIZONTAL iteration order in Java is N,E,S,W.
	directions := [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}
	names := [4]string{"north", "east", "south", "west"}
	solid := -1
	for index, direction := range directions {
		neighbor := [3]int32{pos[0] + direction[0], pos[1], pos[2] + direction[1]}
		if w.blockName(neighbor) == "minecraft:chest" {
			return "north"
		}
		if w.shape(neighbor[0], neighbor[1], neighbor[2]).SolidRender {
			if solid >= 0 {
				solid = -1
				break
			}
			solid = index
		}
	}
	if solid >= 0 {
		return names[(solid+2)%4]
	}
	isSolid := func(index int) bool {
		direction := directions[index]
		return w.shape(pos[0]+direction[0], pos[1], pos[2]+direction[1]).SolidRender
	}
	lock := 0
	if isSolid(lock) {
		lock = (lock + 2) % 4
	}
	if isSolid(lock) {
		lock = (lock + 1) % 4
	}
	if isSolid(lock) {
		lock = (lock + 2) % 4
	}
	return names[lock]
}

// Mob.finalizeSpawn applies its bonus before choosing handedness. Keeping the
// modifier, rather than only advancing RNG, preserves the source actor state.
func scatteredMobSpawnNBT(id string, health float32, random *vanilla.Random) map[string]any {
	bonus := (random.NextDouble() - random.NextDouble()) * 0.11485000000000001
	leftHanded := byte(0)
	if random.NextFloat() < 0.05 {
		leftHanded = 1
	}
	return map[string]any{
		"id": id, "Health": health, "PersistenceRequired": true,
		"Rotation": []any{float32(0), float32(0)}, "LeftHanded": leftHanded,
		"attributes": []any{map[string]any{
			"id": "minecraft:follow_range", "base": float64(16),
			"modifiers": []any{map[string]any{
				"id": "minecraft:random_spawn_bonus", "amount": bonus, "operation": "add_multiplied_base",
			}},
		}},
	}
}

// Source heightmap structures validate their terrain column and generation stub
// before invoking the raw piece constructor. Only SinglePieceStructure checks
// its four first-occupied footprint corners against sea level.
func sourceHeightmapStructureAllowed(source ModernFeatureSource, kind string, config map[string]any, chunkX, chunkZ int32) (bool, error) {
	var width, depth int32
	switch kind {
	case "jungle_temple":
		width, depth = 12, 15
	case "desert_pyramid":
		width, depth = 21, 21
	case "swamp_hut", "igloo", "shipwreck", "ocean_ruin":
	default:
		return true, nil
	}
	x, z := chunkX*16+8, chunkZ*16+8
	if allowed, err := structureColumnBiomeAllowed(source, config, x, z); err != nil || !allowed {
		return false, err
	}
	if width != 0 {
		minX, minZ := chunkX*16, chunkZ*16
		for _, corner := range [4][2]int32{{minX, minZ}, {minX, minZ + depth}, {minX + width, minZ}, {minX + width, minZ + depth}} {
			if source.BaseHeight(corner[0], corner[1], false)-1 < source.SeaLevel() {
				return false, nil
			}
		}
	}
	oceanFloor := kind == "ocean_ruin" || kind == "shipwreck" && config["is_beached"] != true
	return structureStubBiomeAllowed(source, config, x, source.BaseHeight(x, z, oceanFloor)-1, z)
}

// structureColumnBiomeAllowed ports GenerationContext.couldValidBiomeExistInTerrainColumn:
// whether any biome in the column at x, z is one the structure may start in.
func structureColumnBiomeAllowed(source ModernFeatureSource, config map[string]any, x, z int32) (bool, error) {
	bounds := world.Overworld.Range()
	for quartY := int32(bounds[0]-1) >> 2; quartY <= int32(bounds[1])>>2; quartY++ {
		if allowed, err := structureBiomeAllowed(source, config, x>>2, quartY, z>>2); err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

// structureStubBiomeAllowed ports GenerationContext.isValidBiome, which every
// structure applies to the position of its generation stub.
func structureStubBiomeAllowed(source ModernFeatureSource, config map[string]any, x, y, z int32) (bool, error) {
	return structureBiomeAllowed(source, config, x>>2, y>>2, z>>2)
}

func structureBiomeAllowed(source ModernFeatureSource, config map[string]any, quartX, quartY, quartZ int32) (bool, error) {
	biome := normalizeResource(source.NoiseBiomeAt(quartX, quartY, quartZ))
	if !strings.Contains(biome, ":") {
		biome = "minecraft:" + biome
	}
	return gamedata.BiomeTagContains(stringValue(config["biomes"]), biome)
}
