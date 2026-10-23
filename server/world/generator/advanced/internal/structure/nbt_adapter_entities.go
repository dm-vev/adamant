package structure

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// AdaptItemFrameEntities replaces Java item-frame entity descriptors with
// native Bedrock frame blocks and block-entity NBT.
func (p Placement) AdaptItemFrameEntities() (Placement, error) {
	out := Placement{
		Blocks:               make([]Block, 0, len(p.Blocks)+len(p.Entities)),
		PostProcessing:       append([]cube.Pos(nil), p.PostProcessing...),
		ScheduledTicks:       append([]chunk.ScheduledTick(nil), p.ScheduledTicks...),
		StructurePieceWrites: p.StructurePieceWrites,
		TemplateUpdateFlags:  p.TemplateUpdateFlags,
		TemplateKnownShape:   p.TemplateKnownShape,
		PostShapeBlocks:      append([]Block(nil), p.PostShapeBlocks...),
		PostShapeEntities:    append([]Entity(nil), p.PostShapeEntities...),
	}
	for _, placed := range p.Blocks {
		placed.NBT = cloneNBT(placed.NBT)
		out.Blocks = append(out.Blocks, placed)
	}
	out.Entities = make([]Entity, 0, len(p.Entities))
	for _, entity := range p.Entities {
		data := cloneNBT(entity.NBT)
		id, _ := data["id"].(string)
		id = strings.TrimPrefix(id, "minecraft:")
		if id != "item_frame" && id != "glow_item_frame" {
			entity.NBT = data
			out.Entities = append(out.Entities, entity)
			continue
		}
		facing, err := frameFacing(data["Facing"])
		if err != nil {
			return Placement{}, fmt.Errorf("item frame at %v: %w", entity.BlockPos, err)
		}
		frame := block.ItemFrame{Facing: facing.Opposite(), DropChance: 1, Glowing: id == "glow_item_frame"}
		if rotation, ok := nbtInt(data["ItemRotation"]); ok {
			frame.Rotations = int(rotation)
		}
		if chance, ok := number(data["ItemDropChance"]); ok {
			frame.DropChance = chance
		}
		if rawItem, ok := data["Item"].(map[string]any); ok {
			frame.Item, err = item.StackFromStructureNBT(rawItem)
			if err != nil {
				return Placement{}, fmt.Errorf("item frame at %v item: %w", entity.BlockPos, err)
			}
		}
		blockNBT := frame.EncodeNBT()
		out.Blocks = append(out.Blocks, Block{
			Pos:         entity.BlockPos,
			NBT:         blockNBT,
			NativeBlock: frame,
		})
	}
	return out, nil
}

func frameFacing(value any) (cube.Face, error) {
	facing, ok := nbtInt(value)
	if !ok {
		return 0, fmt.Errorf("missing or invalid Facing tag %T", value)
	}
	switch facing {
	case 0:
		return cube.FaceDown, nil
	case 1:
		return cube.FaceUp, nil
	case 2:
		return cube.FaceNorth, nil
	case 3:
		return cube.FaceSouth, nil
	case 4:
		return cube.FaceWest, nil
	case 5:
		return cube.FaceEast, nil
	default:
		return 0, fmt.Errorf("unsupported Facing value %d", facing)
	}
}

func nbtInt(value any) (int64, bool) {
	switch n := value.(type) {
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}
