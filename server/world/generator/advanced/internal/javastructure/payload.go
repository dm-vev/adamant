package javastructure

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// BlockEntityPayloads converts placed block entities to absolute chunk payload records.
func (p Placement) BlockEntityPayloads() []chunk.BlockEntity {
	out := make([]chunk.BlockEntity, 0)
	for _, block := range p.Blocks {
		if len(block.NBT) == 0 && block.NativeBlock == nil {
			continue
		}
		var data map[string]any
		if block.NativeBlock != nil {
			native, ok := block.NativeBlock.(world.NBTer)
			if !ok {
				panic(fmt.Sprintf("native structure block %T has no NBT encoder", block.NativeBlock))
			}
			data = native.EncodeNBT()
			if len(block.NBT) != 0 && stateDerivedActor(block.NativeBlock) {
				converted, err := JavaBlockEntityPayload(block.State, block.Pos, block.NBT)
				if err != nil {
					panic(err)
				}
				for key, value := range data {
					// Banner pattern layers come from the source actor; position,
					// colour, pose and rotation come from the transformed state.
					if key == "Patterns" {
						if _, exists := converted[key]; exists {
							continue
						}
					}
					converted[key] = value
				}
				data = converted
			}
			if data == nil {
				data = make(map[string]any, 3)
			}
			data["x"], data["y"], data["z"] = block.Pos[0], block.Pos[1], block.Pos[2]
		} else {
			var err error
			data, err = JavaBlockEntityPayload(block.State, block.Pos, block.NBT)
			if err != nil {
				panic(err)
			}
		}
		out = append(out, chunk.BlockEntity{
			Pos:  cube.Pos{int(block.Pos[0]), int(block.Pos[1]), int(block.Pos[2])},
			Data: data,
		})
	}
	return out
}
