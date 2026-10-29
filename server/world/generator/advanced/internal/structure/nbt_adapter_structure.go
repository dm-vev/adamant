package structure

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

func convertJigsaw(data map[string]any) (map[string]any, error) {
	out := map[string]any{
		"id": "JigsawBlock", "BlockEntityVersion": int32(0),
		"name": "minecraft:empty", "target": "minecraft:empty", "target_pool": "minecraft:empty",
		"final_state": "minecraft:air", "joint": "rollable",
		"placement_priority": int32(0), "selection_priority": int32(0),
		"x": data["x"], "y": data["y"], "z": data["z"],
	}
	for _, key := range [...]string{"name", "target", "final_state", "joint"} {
		if value, exists := data[key]; exists {
			out[key] = value
		}
	}
	if pool, exists := data["pool"]; exists {
		out["target_pool"] = pool
	}
	for _, key := range [...]string{"placement_priority", "selection_priority"} {
		if value, exists := data[key]; exists {
			number, valid := nbtInt(value)
			if !valid {
				return nil, fmt.Errorf("jigsaw %s has type %T, want integer", key, value)
			}
			out[key] = int32(number)
		}
	}
	return out, nil
}

func convertStructureBlock(state world.BlockState, data map[string]any) (map[string]any, error) {
	out := map[string]any{
		"id": "StructureBlock", "BlockEntityVersion": int32(0),
		"animationMode": uint8(v2193.AnimationModeNone), "animationSeconds": float32(0),
		"data": int32(v2193.StructureBlockData), "dataField": "", "structureName": "",
		"ignoreEntities": uint8(1), "includePlayers": uint8(0), "isPowered": uint8(0),
		"lastTouchedPlayerId": int64(-1), "integrity": float32(100), "seed": int64(0),
		"mirror": uint8(v2193.StructureMirrorNone), "rotation": uint8(v2193.StructureRotationNone),
		"redstoneSaveMode": int32(0), "removeBlocks": uint8(0), "showBoundingBox": uint8(1),
		"xStructureOffset": int32(0), "yStructureOffset": int32(1), "zStructureOffset": int32(0),
		"xStructureSize": int32(0), "yStructureSize": int32(0), "zStructureSize": int32(0),
		"x": data["x"], "y": data["y"], "z": data["z"],
	}
	for _, pair := range [...][2]string{{"name", "structureName"}, {"metadata", "dataField"}} {
		if value, exists := data[pair[0]]; exists {
			out[pair[1]] = value
		}
	}
	for _, pair := range [...][2]string{{"posX", "xStructureOffset"}, {"posY", "yStructureOffset"}, {"posZ", "zStructureOffset"}, {"sizeX", "xStructureSize"}, {"sizeY", "yStructureSize"}, {"sizeZ", "zStructureSize"}, {"seed", "seed"}} {
		if raw, exists := data[pair[0]]; exists {
			value, valid := nbtInt(raw)
			if !valid {
				return nil, fmt.Errorf("structure block %s has type %T, want integer", pair[0], raw)
			}
			if pair[0] == "seed" {
				out[pair[1]] = value
			} else {
				out[pair[1]] = int32(value)
			}
		}
	}
	for _, pair := range [...][2]string{{"ignoreEntities", "ignoreEntities"}, {"powered", "isPowered"}, {"showboundingbox", "showBoundingBox"}} {
		if raw, exists := data[pair[0]]; exists {
			value := int64(0)
			if boolean, ok := raw.(bool); ok {
				if boolean {
					value = 1
				}
			} else {
				var valid bool
				value, valid = nbtInt(raw)
				if !valid {
					return nil, fmt.Errorf("structure block %s has type %T, want byte", pair[0], raw)
				}
			}
			out[pair[1]] = uint8(value)
		}
	}
	if integrity, exists := data["integrity"]; exists {
		switch value := integrity.(type) {
		case float32:
			out["integrity"] = value * 100
		case float64:
			out["integrity"] = float32(value) * 100
		default:
			return nil, fmt.Errorf("structure block integrity has type %T, want float", integrity)
		}
	}
	mode, _ := data["mode"].(string)
	if mode == "" {
		mode, _ = state.Properties["mode"].(string)
	}
	switch strings.ToUpper(mode) {
	case "SAVE":
		out["data"] = int32(v2193.StructureBlockSave)
	case "LOAD":
		out["data"] = int32(v2193.StructureBlockLoad)
	case "CORNER":
		out["data"] = int32(v2193.StructureBlockCorner)
	}
	switch data["rotation"] {
	case "CLOCKWISE_90":
		out["rotation"] = uint8(v2193.StructureRotationRotate90)
	case "CLOCKWISE_180":
		out["rotation"] = uint8(v2193.StructureRotationRotate180)
	case "COUNTERCLOCKWISE_90":
		out["rotation"] = uint8(v2193.StructureRotationRotate270)
	}
	switch data["mirror"] {
	case "FRONT_BACK":
		out["mirror"] = uint8(v2193.StructureMirrorXAxis)
	case "LEFT_RIGHT":
		out["mirror"] = uint8(v2193.StructureMirrorZAxis)
	}
	canonical := map[string]any{}
	if author, exists := data["author"]; exists {
		canonical["author"] = author
	}
	for _, key := range [...]string{"strict", "showair"} {
		if raw, exists := data[key]; exists {
			var value int64
			if boolean, ok := raw.(bool); ok {
				if boolean {
					value = 1
				}
			} else {
				var valid bool
				value, valid = nbtInt(raw)
				if !valid {
					return nil, fmt.Errorf("structure block %s has type %T, want byte", key, raw)
				}
			}
			canonical[key] = uint8(value)
		}
	}
	if len(canonical) != 0 {
		out["adamant:java"] = canonical
	}
	return out, nil
}
