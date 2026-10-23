package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func init() { registerModernStructureBuilder("igloo", buildModernIgloo) }

type modernIglooPiece struct {
	template *structure.Template
	box      structure.Box
	origin   [3]int32 // TemplateStructurePiece.templatePosition, before postProcess height adjustment.
	rotation structure.Rotation
	pivot    [3]int32
	seed     int64
	name     string
}

func (p modernIglooPiece) Bounds() structure.Box { return p.box }

func (p modernIglooPiece) Place(clip structure.Bounds, env structure.ProcessorEnvironment) (structure.Placement, error) {
	if env.BaseHeight == nil {
		return structure.Placement{}, fmt.Errorf("igloo postProcess requires WORLD_SURFACE_WG")
	}
	if env.Random == nil {
		return structure.Placement{}, fmt.Errorf("igloo postProcess requires structure random")
	}
	if len(p.template.Palettes) == 0 {
		return structure.Placement{}, nil
	}
	offset := [3]int32{}
	if p.name == "igloo/middle" {
		offset = [3]int32{2, -3, 4}
	} else if p.name == "igloo/bottom" {
		offset = [3]int32{0, -3, -2}
	}
	// IglooPieces.IglooPiece.postProcess samples the entrance relative to the
	// persisted templatePosition, then temporarily moves that position by H-91.
	entranceRelative := [3]int32{3 - offset[0], 0, -offset[2]}
	entrance := transformedTemplatePos(p.origin, entranceRelative, p.pivot, p.rotation)
	height := env.BaseHeight(entrance[0], entrance[2], false)
	origin := p.origin
	origin[1] += height - 91
	placeOrigin := templatePivotOrigin(origin, p.pivot, p.rotation)
	ignoreStructureBlocks := structure.Processor{Process: func(_ structure.ProcessContext, block structure.Block) (structure.Block, bool, error) {
		return block, block.State.Name != "minecraft:structure_block", nil
	}}
	placed, err := p.template.Place(0, placeOrigin, p.rotation, structure.NoMirror, clip, p.seed, ignoreStructureBlocks)
	if err != nil {
		return structure.Placement{}, err
	}
	for i := range placed.Blocks {
		placed.Blocks[i].LiquidSettings = structure.IgnoreWaterlogging
	}
	if env.PieceState != nil {
		box := modernTemplateBounds(p.template, placeOrigin, p.rotation)
		env.PieceState.BoundingBoxYSet = true
		env.PieceState.BoundingBoxMinY = box.Min[1]
	}
	return placed, nil
}

func (p modernIglooPiece) AfterTemplatePlacement(clip structure.Bounds, env structure.ProcessorEnvironment, emit func(structure.Placement)) error {
	if env.BaseHeight == nil {
		return fmt.Errorf("igloo postProcess requires WORLD_SURFACE_WG")
	}
	offset := [3]int32{}
	if p.name == "igloo/middle" {
		offset = [3]int32{2, -3, 4}
	} else if p.name == "igloo/bottom" {
		offset = [3]int32{0, -3, -2}
	}
	entrance := transformedTemplatePos(p.origin, [3]int32{3 - offset[0], 0, -offset[2]}, p.pivot, p.rotation)
	origin := p.origin
	origin[1] += env.BaseHeight(entrance[0], entrance[2], false) - 91
	placeOrigin := templatePivotOrigin(origin, p.pivot, p.rotation)
	inside := func(pos [3]int32) bool {
		return pos[0] >= clip.Min[0] && pos[0] <= clip.Max[0] && pos[1] >= clip.Min[1] && pos[1] <= clip.Max[1] && pos[2] >= clip.Min[2] && pos[2] <= clip.Max[2]
	}
	// StructureTemplate seeds only successfully placed, live randomizable actors.
	// Igloo templates contain only chest actors in that category; their empty
	// template inventory ignores the seed, but the draw precedes marker loot.
	for _, block := range p.template.Palettes[0] {
		if block.State.Name != "minecraft:chest" || len(block.NBT) == 0 {
			continue
		}
		pos := structure.TransformPosition(block.Pos, structure.NoMirror, p.rotation)
		for axis := range 3 {
			pos[axis] += placeOrigin[axis]
		}
		if !inside(pos) {
			continue
		}
		if env.HasChestBlockEntity == nil || env.Random == nil {
			return fmt.Errorf("igloo template chest requires live actor and random queries")
		}
		if env.HasChestBlockEntity(pos[0], pos[1], pos[2]) {
			env.Random.NextLong()
		}
	}
	for _, marker := range p.template.Palettes[0] {
		if marker.State.Name != "minecraft:structure_block" || marker.NBT == nil || marker.NBT["mode"] != "DATA" || marker.NBT["metadata"] != "chest" {
			continue
		}
		markerPos := structure.TransformPosition(marker.Pos, structure.NoMirror, p.rotation)
		for axis := range 3 {
			markerPos[axis] += placeOrigin[axis]
		}
		if !inside(markerPos) {
			continue
		}
		emit(structure.Placement{PostShapeBlocks: []structure.Block{{Pos: markerPos, State: world.BlockState{Name: "minecraft:air"}}}})
		chestPos := [3]int32{markerPos[0], markerPos[1] - 1, markerPos[2]}
		if env.HasChestBlockEntity == nil || env.SetBlockEntityLootTable == nil {
			return fmt.Errorf("igloo chest marker requires live chest block-entity callbacks")
		}
		if env.HasChestBlockEntity(chestPos[0], chestPos[1], chestPos[2]) {
			if env.Random == nil {
				return fmt.Errorf("igloo chest marker requires postProcess random")
			}
			env.SetBlockEntityLootTable(chestPos[0], chestPos[1], chestPos[2], "minecraft:chests/igloo_chest", env.Random.NextLong())
		}
	}
	if p.name == "igloo/top" {
		trapdoor := transformedTemplatePos(origin, [3]int32{3, 0, 5}, p.pivot, p.rotation)
		below := [3]int32{trapdoor[0], trapdoor[1] - 1, trapdoor[2]}
		if env.BlockStateID == nil {
			return fmt.Errorf("igloo top requires live block-state query")
		}
		id, queryErr := env.BlockStateID(below[0], below[1], below[2])
		if queryErr != nil {
			return queryErr
		}
		belowName, ok := gamedata.BlockStateName(id)
		if !ok {
			return fmt.Errorf("igloo top queried unknown block state %d", id)
		}
		if belowName != "minecraft:air" && belowName != "minecraft:cave_air" && belowName != "minecraft:void_air" && belowName != "minecraft:ladder" {
			emit(structure.Placement{PostShapeBlocks: []structure.Block{{Pos: trapdoor, State: world.BlockState{Name: "minecraft:snow_block"}}}})
		}
	}
	return nil
}

func templatePivotOrigin(position, pivot [3]int32, rotation structure.Rotation) [3]int32 {
	rotated := structure.TransformPosition(pivot, structure.NoMirror, rotation)
	return [3]int32{position[0] + pivot[0] - rotated[0], position[1] + pivot[1] - rotated[1], position[2] + pivot[2] - rotated[2]}
}

func transformedTemplatePos(position, relative, pivot [3]int32, rotation structure.Rotation) [3]int32 {
	origin := templatePivotOrigin(position, pivot, rotation)
	transformed := structure.TransformPosition(relative, structure.NoMirror, rotation)
	return [3]int32{origin[0] + transformed[0], origin[1] + transformed[1], origin[2] + transformed[2]}
}

func buildModernIgloo(in modernStructureBuildInput) (modernStructureStart, bool, error) {
	if in.random == nil {
		return modernStructureStart{}, false, fmt.Errorf("igloo requires structure random")
	}
	rotation := structure.Rotation(in.random.NextInt(4))
	base := [3]int32{in.chunkX * 16, 90, in.chunkZ * 16}
	pieces := make([]modernStructurePiece, 0, 12)
	if in.random.NextDouble() < 0.5 {
		depth := int32(in.random.NextInt(8) + 4)
		p, err := makeModernIglooPiece("igloo/bottom", base, rotation, depth*3, [3]int32{3, 6, 7}, [3]int32{0, -3, -2}, in.seed)
		if err != nil {
			return modernStructureStart{}, false, err
		}
		pieces = append(pieces, p)
		for i := int32(0); i < depth-1; i++ {
			p, err := makeModernIglooPiece("igloo/middle", base, rotation, i*3, [3]int32{1, 3, 1}, [3]int32{2, -3, 4}, in.seed)
			if err != nil {
				return modernStructureStart{}, false, err
			}
			pieces = append(pieces, p)
		}
	}
	p, err := makeModernIglooPiece("igloo/top", base, rotation, 0, [3]int32{3, 5, 5}, [3]int32{}, in.seed)
	if err != nil {
		return modernStructureStart{}, false, err
	}
	pieces = append(pieces, p)
	box := emptyStructureBox()
	for _, piece := range pieces {
		box = unionStructureBox(box, piece.Bounds())
	}
	return modernStructureStart{name: in.name, pieces: pieces, box: box, mutablePieces: true, terrain: terrainAdjustment(stringValue(in.config["terrain_adaptation"]))}, true, nil
}

func makeModernIglooPiece(name string, base [3]int32, rotation structure.Rotation, depth int32, pivot, offset [3]int32, seed int64) (modernIglooPiece, error) {
	t, err := structure.Load(name)
	if err != nil {
		return modernIglooPiece{}, err
	}
	position := [3]int32{base[0] + offset[0], base[1] + offset[1] - depth, base[2] + offset[2]}
	box := modernTemplateBounds(t, templatePivotOrigin(position, pivot, rotation), rotation)
	return modernIglooPiece{template: t, box: box, origin: position, rotation: rotation, pivot: pivot, seed: seed, name: name}, nil
}
