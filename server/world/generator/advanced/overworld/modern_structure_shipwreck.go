package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func init() { registerModernStructureBuilder("shipwreck", buildModernShipwreck) }

var shipwreckPivot = [3]int32{4, 0, 15}

type modernShipwreckPiece struct {
	template       *structure.Template
	templateName   string
	box            structure.Box // immutable constructor bounding box
	origin         [3]int32
	rotation       structure.Rotation
	beached        bool
	oversized      bool
	heightAdjusted bool
	seed           int64
}

func (p modernShipwreckPiece) Bounds() structure.Box { return p.box }

func (p modernShipwreckPiece) Place(clip structure.Bounds, env structure.ProcessorEnvironment) (structure.Placement, error) {
	if env.BaseHeight == nil {
		return structure.Placement{}, fmt.Errorf("shipwreck postProcess requires frozen heightmap query")
	}
	state := env.PieceState
	var scratch structure.StructurePieceState
	if state == nil {
		state = &scratch
	}
	if !state.TemplatePositionYSet {
		state.TemplatePositionY = p.origin[1]
		state.TemplatePositionYSet = true
		state.HeightAdjusted = p.heightAdjusted
	}
	position := p.origin
	position[1] = state.TemplatePositionY
	heightAdjusted := state.HeightAdjusted || p.heightAdjusted
	if !heightAdjusted && !p.oversized {
		heightmapOceanFloor := !p.beached
		minY := int32(env.Range.Max()) + 1 // Source getMaxY is inclusive.
		var total int64
		count := int64(p.template.Size[0]) * int64(p.template.Size[2])
		if count == 0 {
			total = int64(env.BaseHeight(position[0], position[2], heightmapOceanFloor))
			count = 1
		} else {
			for dz := int32(0); dz < p.template.Size[2]; dz++ {
				for dx := int32(0); dx < p.template.Size[0]; dx++ {
					height := env.BaseHeight(position[0]+dx, position[2]+dz, heightmapOceanFloor)
					total += int64(height)
					if height < minY {
						minY = height
					}
				}
			}
		}
		position[1] = int32(total / count)
		if p.beached {
			if env.Random == nil {
				return structure.Placement{}, fmt.Errorf("beached shipwreck height adjustment requires postProcess random")
			}
			position[1] = minY - p.template.Size[1]/2 - env.Random.NextInt(3)
		}
		heightAdjusted = true
	}
	placeOrigin := templatePivotOrigin(position, shipwreckPivot, p.rotation)
	ignore := structure.Processor{Process: func(_ structure.ProcessContext, b structure.Block) (structure.Block, bool, error) {
		return b, b.State.Name != "minecraft:air" && b.State.Name != "minecraft:structure_block", nil
	}}
	placed, err := p.template.Place(0, placeOrigin, p.rotation, structure.NoMirror, clip, p.seed, ignore)
	if err != nil {
		return structure.Placement{}, err
	}
	state.TemplatePositionY = position[1]
	state.TemplatePositionYSet = true
	state.HeightAdjusted = heightAdjusted || p.oversized
	box := modernTemplateBounds(p.template, placeOrigin, p.rotation)
	state.BoundingBoxYSet = true
	state.BoundingBoxMinY = box.Min[1]
	return placed, nil
}

func (p modernShipwreckPiece) AfterTemplatePlacement(clip structure.Bounds, env structure.ProcessorEnvironment, _ func(structure.Placement)) error {
	if env.Random == nil {
		return fmt.Errorf("shipwreck postProcess requires structure random")
	}
	if env.PieceState == nil {
		return fmt.Errorf("shipwreck marker handling requires persisted piece state")
	}
	if env.HasChestBlockEntity == nil || env.SetBlockEntityLootTable == nil {
		return fmt.Errorf("shipwreck marker handling requires live block-entity callbacks")
	}
	position := p.origin
	position[1] = env.PieceState.TemplatePositionY
	placeOrigin := templatePivotOrigin(position, shipwreckPivot, p.rotation)
	inside := func(pos [3]int32) bool {
		return pos[0] >= clip.Min[0] && pos[0] <= clip.Max[0] && pos[1] >= clip.Min[1] && pos[1] <= clip.Max[1] && pos[2] >= clip.Min[2] && pos[2] <= clip.Max[2]
	}
	// StructureTemplate consumes a draw only after an actual RandomizableContainer
	// block entity has been placed successfully. The live actor query reproduces
	// that condition across clipped and repeated postProcess calls.
	for _, block := range p.template.Palettes[0] {
		if block.State.Name != "minecraft:chest" {
			continue
		}
		pos := structure.TransformPosition(block.Pos, structure.NoMirror, p.rotation)
		for axis := range 3 {
			pos[axis] += placeOrigin[axis]
		}
		if !inside(pos) {
			continue
		}
		if env.HasChestBlockEntity(pos[0], pos[1], pos[2]) {
			env.Random.NextLong()
		}
	}
	for _, source := range p.template.Palettes[0] {
		if source.State.Name != "minecraft:structure_block" || source.NBT == nil || source.NBT["mode"] != "DATA" {
			continue
		}
		marker, _ := source.NBT["metadata"].(string)
		var table string
		switch marker {
		case "map_chest":
			table = "minecraft:chests/shipwreck_map"
		case "treasure_chest":
			table = "minecraft:chests/shipwreck_treasure"
		case "supply_chest":
			table = "minecraft:chests/shipwreck_supply"
		default:
			continue
		}
		markerPos := structure.TransformPosition(source.Pos, structure.NoMirror, p.rotation)
		for axis := range 3 {
			markerPos[axis] += placeOrigin[axis]
		}
		if !inside(markerPos) {
			continue
		}
		// StructureTemplate ignored structure blocks during placement; Source
		// clips this marker but deliberately does not clip markerPos.below().
		chest := [3]int32{markerPos[0], markerPos[1] - 1, markerPos[2]}
		if env.HasChestBlockEntity(chest[0], chest[1], chest[2]) {
			env.SetBlockEntityLootTable(chest[0], chest[1], chest[2], table, env.Random.NextLong())
		}
	}
	return nil
}

func buildModernShipwreck(input modernStructureBuildInput) (modernStructureStart, bool, error) {
	if input.source == nil || input.random == nil {
		return modernStructureStart{}, false, fmt.Errorf("shipwreck requires terrain and generation random queries")
	}
	beached := input.config["is_beached"] == true
	var names []string
	if beached {
		names = []string{"with_mast", "sideways_full", "sideways_fronthalf", "sideways_backhalf", "rightsideup_full", "rightsideup_fronthalf", "rightsideup_backhalf", "with_mast_degraded", "rightsideup_full_degraded", "rightsideup_fronthalf_degraded", "rightsideup_backhalf_degraded"}
	} else {
		names = []string{"with_mast", "upsidedown_full", "upsidedown_fronthalf", "upsidedown_backhalf", "sideways_full", "sideways_fronthalf", "sideways_backhalf", "rightsideup_full", "rightsideup_fronthalf", "rightsideup_backhalf", "with_mast_degraded", "upsidedown_full_degraded", "upsidedown_fronthalf_degraded", "upsidedown_backhalf_degraded", "sideways_full_degraded", "sideways_fronthalf_degraded", "sideways_backhalf_degraded", "rightsideup_full_degraded", "rightsideup_fronthalf_degraded", "rightsideup_backhalf_degraded"}
	}
	rotation := structure.Rotation(input.random.NextInt(4))
	templateName := "shipwreck/" + names[input.random.NextInt(int32(len(names)))]
	template, err := structure.Load(templateName)
	if err != nil {
		return modernStructureStart{}, false, err
	}
	origin := [3]int32{input.chunkX * 16, 90, input.chunkZ * 16}
	piece := &modernShipwreckPiece{
		template: template, templateName: templateName, origin: origin, rotation: rotation, beached: beached,
		oversized: template.Size[0] > 32 || template.Size[1] > 32, seed: input.seed,
	}
	box := modernTemplateBounds(template, templatePivotOrigin(origin, shipwreckPivot, rotation), rotation)
	piece.box = box
	if piece.oversized {
		xSpan, zSpan := box.Max[0]-box.Min[0]+1, box.Max[2]-box.Min[2]+1
		// Structure.getCornerHeights always samples WORLD_SURFACE_WG. The
		// beached caller's argument order is the source's unusual minX,sizeX,
		// minZ,sizeZ call into getLowestY(minX,minZ,sizeX,sizeZ).
		minX, minZ, sampleX, sampleZ := box.Min[0], box.Min[2], xSpan, zSpan
		if beached {
			minZ, sampleX = xSpan, box.Min[2]
		}
		heights := [4]int32{
			input.source.BaseHeight(minX, minZ, false) - 1,
			input.source.BaseHeight(minX, minZ+sampleZ, false) - 1,
			input.source.BaseHeight(minX+sampleX, minZ, false) - 1,
			input.source.BaseHeight(minX+sampleX, minZ+sampleZ, false) - 1,
		}
		if beached {
			minY := min(heights[0], heights[1], heights[2], heights[3])
			piece.origin[1] = minY - template.Size[1]/2 - input.random.NextInt(3)
		} else {
			piece.origin[1] = int32((int64(heights[0]) + int64(heights[1]) + int64(heights[2]) + int64(heights[3])) / 4)
		}
		piece.heightAdjusted = true
	}
	return modernStructureStart{name: input.name, pieces: []modernStructurePiece{piece}, box: box, mutablePieces: true, terrain: structure.TerrainNone}, true, nil
}
