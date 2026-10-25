package overworld

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
	"github.com/google/uuid"
)

func init() { registerModernStructureBuilder("ocean_ruin", buildModernOceanRuin) }

type modernOceanRuinPiece struct {
	template     *structure.Template
	processors   []structure.Processor
	templateName string
	origin       [3]int32
	rotation     structure.Rotation
	integrity    float32
	warm, large  bool
	seed         int64
}

func (p modernOceanRuinPiece) Bounds() structure.Box {
	box := modernTemplateBounds(p.template, p.origin, p.rotation)
	return box
}

func (p modernOceanRuinPiece) Place(clip structure.Bounds, env structure.ProcessorEnvironment) (structure.Placement, error) {
	if env.BaseHeight == nil || env.BlockStateID == nil {
		return structure.Placement{}, fmt.Errorf("ocean ruin requires terrain height and live block-state queries")
	}
	if len(p.template.Palettes) == 0 {
		return structure.Placement{}, nil
	}
	origin := p.origin
	if env.PieceState != nil && env.PieceState.TemplatePositionYSet {
		origin[1] = env.PieceState.TemplatePositionY
	}
	// Source samples the frozen OCEAN_FLOOR_WG at the template position every
	// postProcess call, even after a prior cave adjustment.
	height := env.BaseHeight(origin[0], origin[2], true)
	corner := structure.TransformPosition([3]int32{p.template.Size[0] - 1, 0, p.template.Size[2] - 1}, structure.NoMirror, p.rotation)
	corner[0], corner[1], corner[2] = corner[0]+origin[0], origin[1], corner[2]+origin[2]
	topY := height - 1
	minY, area := int32(512), 0
	for z := min(origin[2], corner[2]); z <= max(origin[2], corner[2]); z++ {
		for x := min(origin[0], corner[0]); x <= max(origin[0], corner[0]); x++ {
			floorY := height - 1
			for floorY > int32(env.Range.Min())+1 {
				id, err := env.BlockStateID(x, floorY, z)
				if err != nil {
					return structure.Placement{}, err
				}
				name, ok := gamedata.BlockStateName(id)
				if !ok {
					return structure.Placement{}, fmt.Errorf("ocean ruin: unknown block state %d", id)
				}
				isWater := world.BlockStateHasWaterFluid(id)
				isIce, tagErr := gamedata.BlockTagContains("minecraft:ice", name)
				if tagErr != nil {
					return structure.Placement{}, tagErr
				}
				if name != "minecraft:air" && name != "minecraft:cave_air" && name != "minecraft:void_air" && !isWater && !isIce {
					break
				}
				floorY--
			}
			minY = min(minY, floorY)
			if floorY < topY-2 {
				area++
			}
		}
	}
	newY := height
	width := max(origin[0], corner[0]) - min(origin[0], corner[0])
	if topY-minY > 2 && area > int(width-2) {
		newY = minY + 1
	}
	origin[1] = newY
	placed, err := p.template.Place(0, origin, p.rotation, structure.NoMirror, clip, p.seed, p.processors...)
	if err != nil {
		return structure.Placement{}, err
	}
	if env.PieceState != nil {
		env.PieceState.TemplatePositionY, env.PieceState.TemplatePositionYSet = origin[1], true
		box := modernTemplateBounds(p.template, origin, p.rotation)
		env.PieceState.BoundingBoxYSet, env.PieceState.BoundingBoxMinY = true, box.Min[1]
	}
	return placed, nil
}

func (p modernOceanRuinPiece) AfterTemplatePlacement(clip structure.Bounds, env structure.ProcessorEnvironment, emit func(structure.Placement)) error {
	if env.Random == nil {
		return fmt.Errorf("ocean ruin markers require postProcess random")
	}
	if env.HasChestBlockEntity == nil || env.SetBlockEntityLootTable == nil || env.BlockStateID == nil {
		return fmt.Errorf("ocean ruin markers require live block-entity and state queries")
	}
	origin := p.origin
	if env.PieceState != nil && env.PieceState.TemplatePositionYSet {
		origin[1] = env.PieceState.TemplatePositionY
	}
	for _, source := range p.template.Palettes[0] {
		if source.State.Name != "minecraft:structure_block" || source.NBT == nil || source.NBT["mode"] != "DATA" {
			continue
		}
		marker, _ := source.NBT["metadata"].(string)
		if marker != "chest" && marker != "drowned" {
			continue
		}
		pos := structure.TransformPosition(source.Pos, structure.NoMirror, p.rotation)
		for axis := range 3 {
			pos[axis] += origin[axis]
		}
		if !insideStructureClip(pos, clip) {
			continue
		}
		out := structure.Placement{TemplateUpdateFlags: 2, TemplateKnownShape: true}
		switch marker {
		case "chest":
			fluid, err := env.BlockStateID(pos[0], pos[1], pos[2])
			if err != nil {
				return err
			}
			_, ok := gamedata.BlockStateName(fluid)
			if !ok {
				return fmt.Errorf("ocean ruin: unknown block state %d", fluid)
			}
			waterlogged := world.BlockStateHasWaterFluid(fluid)
			out.PostShapeBlocks = append(out.PostShapeBlocks, structure.Block{Pos: pos, State: world.BlockState{Name: "minecraft:chest", Properties: map[string]any{"facing": "north", "type": "single", "waterlogged": waterlogged}}, NBT: map[string]any{"id": "minecraft:chest"}})
			emit(out)
			if env.HasChestBlockEntity(pos[0], pos[1], pos[2]) {
				table := "minecraft:chests/underwater_ruin_small"
				if p.large {
					table = "minecraft:chests/underwater_ruin_big"
				}
				env.SetBlockEntityLootTable(pos[0], pos[1], pos[2], table, env.Random.NextLong())
			}
		case "drowned":
			if env.LevelRandom == nil || env.BiomeAt == nil {
				return fmt.Errorf("ocean ruin drowned requires live region RNG and biome context")
			}
			id := uuid.New()
			ownRandom := vanilla.NewLegacyRandom(int64(binary.LittleEndian.Uint64(id[8:])))
			biome := env.BiomeAt(pos[0], pos[1], pos[2])
			data, vehicle, err := entity.PrepareDrownedStructureSpawn(env.LevelRandom, &ownRandom, env.GenerationContext, biome)
			if err != nil {
				return err
			}
			spawn := structure.Entity{
				ID: id, Pos: [3]float64{float64(pos[0]) + .5, float64(pos[1]), float64(pos[2]) + .5},
				BlockPos: pos, NBT: data, Seed: p.seed, SpawnReason: "structure",
				SpawnFinalized: true, EntityRandom: &ownRandom,
			}
			if vehicle != nil {
				spawn = structure.Entity{
					ID: uuid.New(), Pos: spawn.Pos, BlockPos: pos, NBT: vehicle,
					Seed: p.seed, SpawnReason: "structure", SpawnFinalized: true,
					Passengers: []structure.Entity{spawn},
				}
			}
			out.PostShapeEntities = append(out.PostShapeEntities, spawn)
			emit(out)
			out = structure.Placement{TemplateUpdateFlags: 2, TemplateKnownShape: true}
			water := "minecraft:water"
			if pos[1] > env.SeaLevel {
				water = "minecraft:air"
			}
			out.PostShapeBlocks = append(out.PostShapeBlocks, structure.Block{Pos: pos, State: world.BlockState{Name: water}})
			emit(out)
		}
	}
	return nil
}
func oceanRuinProcessors(p modernOceanRuinPiece) ([]structure.Processor, error) {
	candidate, replacement, loot := "minecraft:gravel", "minecraft:suspicious_gravel", "minecraft:archaeology/ocean_ruin_cold"
	if p.warm {
		candidate, replacement, loot = "minecraft:sand", "minecraft:suspicious_sand", "minecraft:archaeology/ocean_ruin_warm"
	}
	inline := []structure.ProcessorSpec{
		{Type: "block_rot", Config: map[string]any{"integrity": float64(p.integrity)}},
		{Type: "block_ignore", Config: map[string]any{"blocks": []any{"minecraft:structure_block", "minecraft:air"}}},
		{Type: "capped", Config: map[string]any{
			"limit": float64(5),
			"delegate": map[string]any{"processor_type": "minecraft:rule", "rules": []any{map[string]any{
				"input_predicate":       map[string]any{"predicate_type": "minecraft:block_match", "block": candidate},
				"location_predicate":    map[string]any{"predicate_type": "minecraft:always_true"},
				"output_state":          replacement,
				"block_entity_modifier": map[string]any{"type": "minecraft:append_loot", "loot_table": loot},
			}}},
		}},
	}
	return structure.LoadProcessors(nil, inline, structure.ProcessorEnvironment{WorldSeed: p.seed})
}

func insideStructureClip(pos [3]int32, clip structure.Bounds) bool {
	return pos[0] >= clip.Min[0] && pos[0] <= clip.Max[0] && pos[1] >= clip.Min[1] && pos[1] <= clip.Max[1] && pos[2] >= clip.Min[2] && pos[2] <= clip.Max[2]
}

func buildModernOceanRuin(input modernStructureBuildInput) (modernStructureStart, bool, error) {
	if input.source == nil || input.random == nil {
		return modernStructureStart{}, false, fmt.Errorf("ocean ruin requires terrain source and generation random")
	}
	warm := normalizeResource(stringValue(input.config["biome_temp"])) != "cold"
	rotation := structure.Rotation(input.random.NextInt(4))
	large := input.random.NextFloat() <= float32(number(input.config["large_probability"]))
	baseIntegrity := float32(0.8)
	if large {
		baseIntegrity = 0.9
	}
	pieces := make([]modernStructurePiece, 0, 9)
	parent := [3]int32{input.chunkX * 16, 90, input.chunkZ * 16}
	appendPiece := func(origin [3]int32, rotation structure.Rotation, large bool, integrity float32) error {
		appendTemplate := func(name string, integrity float32) error {
			template, err := structure.Load("underwater_ruin/" + name)
			if err != nil {
				return err
			}
			piece := modernOceanRuinPiece{
				template: template, templateName: "minecraft:underwater_ruin/" + name,
				origin: origin, rotation: rotation, integrity: integrity, warm: warm, large: large, seed: input.seed,
			}
			piece.processors, err = oceanRuinProcessors(piece)
			if err != nil {
				return err
			}
			pieces = append(pieces, piece)
			return nil
		}
		if warm {
			names := []string{"warm_1", "warm_2", "warm_3", "warm_4", "warm_5", "warm_6", "warm_7", "warm_8"}
			if large {
				names = []string{"big_warm_4", "big_warm_5", "big_warm_6", "big_warm_7"}
			}
			return appendTemplate(names[input.random.NextInt(int32(len(names)))], integrity)
		}
		if large {
			suffix := [...]int{1, 2, 3, 8}[input.random.NextInt(4)]
			for _, item := range []struct {
				name      string
				integrity float32
			}{{fmt.Sprintf("big_brick_%d", suffix), integrity}, {fmt.Sprintf("big_cracked_%d", suffix), 0.7}, {fmt.Sprintf("big_mossy_%d", suffix), 0.5}} {
				if err := appendTemplate(item.name, item.integrity); err != nil {
					return err
				}
			}
			return nil
		}
		suffix := input.random.NextInt(8) + 1
		for _, item := range []struct {
			name      string
			integrity float32
		}{{fmt.Sprintf("brick_%d", suffix), integrity}, {fmt.Sprintf("cracked_%d", suffix), 0.7}, {fmt.Sprintf("mossy_%d", suffix), 0.5}} {
			if err := appendTemplate(item.name, item.integrity); err != nil {
				return err
			}
		}
		return nil
	}
	if err := appendPiece(parent, rotation, large, baseIntegrity); err != nil {
		return modernStructureStart{}, false, err
	}
	if large && input.random.NextFloat() <= float32(number(input.config["cluster_probability"])) {
		parentCorner := structure.TransformPosition([3]int32{15, 0, 15}, structure.NoMirror, rotation)
		parentCorner[0], parentCorner[2] = parentCorner[0]+parent[0], parentCorner[2]+parent[2]
		parentBottomLeft := [3]int32{min(parent[0], parentCorner[0]), 90, min(parent[2], parentCorner[2])}
		positions := make([][3]int32, 0, 8)
		addPosition := func(dx, dz int32) {
			positions = append(positions, [3]int32{parentBottomLeft[0] + dx, 90, parentBottomLeft[2] + dz})
		}
		addPosition(-16+1+input.random.NextInt(8), 16+1+input.random.NextInt(7))
		addPosition(-16+1+input.random.NextInt(8), 1+input.random.NextInt(7))
		addPosition(-16+1+input.random.NextInt(8), -16+4+input.random.NextInt(5))
		addPosition(1+input.random.NextInt(7), 16+1+input.random.NextInt(7))
		addPosition(1+input.random.NextInt(7), -16+4+input.random.NextInt(3))
		addPosition(16+1+input.random.NextInt(7), 16+3+input.random.NextInt(6))
		addPosition(16+1+input.random.NextInt(7), 1+input.random.NextInt(7))
		addPosition(16+1+input.random.NextInt(7), -16+4+input.random.NextInt(5))
		ruins := 4 + input.random.NextInt(5)
		parentBox := structure.Box{Min: parentBottomLeft, Max: [3]int32{max(parent[0], parentCorner[0]), 90, max(parent[2], parentCorner[2])}}
		for range ruins {
			index := input.random.NextInt(int32(len(positions)))
			position := positions[index]
			positions = append(positions[:index], positions[index+1:]...)
			nextRotation := structure.Rotation(input.random.NextInt(4))
			corner := structure.TransformPosition([3]int32{5, 0, 6}, structure.NoMirror, nextRotation)
			box := structure.Box{Min: [3]int32{min(position[0], position[0]+corner[0]), 90, min(position[2], position[2]+corner[2])}, Max: [3]int32{max(position[0], position[0]+corner[0]), 90, max(position[2], position[2]+corner[2])}}
			if modernBoxesIntersectXZ(parentBox, box) {
				continue
			}
			if err := appendPiece(position, nextRotation, false, 0.8); err != nil {
				return modernStructureStart{}, false, err
			}
		}
	}
	start := modernStructureStart{name: input.name, pieces: pieces, box: emptyStructureBox()}
	for _, piece := range pieces {
		start.box = unionStructureBox(start.box, piece.Bounds())
	}
	return start, true, nil
}

func modernTemplateBounds(t *structure.Template, origin [3]int32, rotation structure.Rotation) structure.Box {
	return modernMirroredTemplateBounds(t, origin, structure.NoMirror, rotation)
}

// modernMirroredTemplateBounds is StructureTemplate.getBoundingBox of a
// template placed at origin with mirror and rotation.
func modernMirroredTemplateBounds(t *structure.Template, origin [3]int32, mirror structure.Mirror, rotation structure.Rotation) structure.Box {
	box := structure.Box{Min: [3]int32{math.MaxInt32, math.MaxInt32, math.MaxInt32}, Max: [3]int32{math.MinInt32, math.MinInt32, math.MinInt32}}
	for _, x := range [2]int32{0, t.Size[0] - 1} {
		for _, y := range [2]int32{0, t.Size[1] - 1} {
			for _, z := range [2]int32{0, t.Size[2] - 1} {
				pos := structure.TransformPosition([3]int32{x, y, z}, mirror, rotation)
				for axis := range 3 {
					pos[axis] += origin[axis]
					box.Min[axis] = min(box.Min[axis], pos[axis])
					box.Max[axis] = max(box.Max[axis], pos[axis])
				}
			}
		}
	}
	return box
}

func modernBoxesIntersectXZ(a, b structure.Box) bool {
	return a.Min[0] <= b.Max[0] && a.Max[0] >= b.Min[0] && a.Min[2] <= b.Max[2] && a.Max[2] >= b.Min[2]
}
