package c2193

import (
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2169"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

func fixUpPlayerInventoryAction(in *v2169.PlayerInventoryAction, out *v2193.PlayerInventoryAction) {
	if !in.Actions.Set {
		return
	}
	out.Actions = make([]v2193.InventoryAction, len(in.Actions.Val))
	for i := range in.Actions.Val {
		out.Actions[i] = upInventoryAction(&in.Actions.Val[i])
	}
}

func fixDownPlayerInventoryAction(in *v2193.PlayerInventoryAction, out *v2169.PlayerInventoryAction) {
	if in.Actions == nil {
		return
	}
	actions := make([]v2169.InventoryAction, len(in.Actions))
	for i := range in.Actions {
		actions[i] = downInventoryAction(&in.Actions[i])
	}
	out.Actions = proto.Some(actions)
}

func fixUpDimensionDefinition(in *v2169.DimensionDefinition, out *v2193.DimensionDefinition) {
	out.MinimumY = in.Range[1]
	out.HeightRange = in.Range[0] - in.Range[1]
}

func fixDownDimensionDefinition(in *v2193.DimensionDefinition, out *v2169.DimensionDefinition) {
	out.Range = [2]int32{in.MinimumY + in.HeightRange, in.MinimumY}
}

func flatToHeightMap(in proto.Optional[[]int8]) proto.Optional[v2193.HeightMap] {
	if !in.Set {
		return proto.Optional[v2193.HeightMap]{}
	}
	var hm v2193.HeightMap
	for i := 0; i < len(in.Val) && i < 256; i++ {
		hm[i/16][i%16] = in.Val[i]
	}
	return proto.Some(hm)
}

func heightMapToFlat(in proto.Optional[v2193.HeightMap]) proto.Optional[[]int8] {
	if !in.Set {
		return proto.Optional[[]int8]{}
	}
	flat := make([]int8, 256)
	for z := range 16 {
		copy(flat[z*16:], in.Val[z][:])
	}
	return proto.Some(flat)
}

func fixUpSubChunkEntry(in *v2169.SubChunkEntry, out *v2193.SubChunkEntry) {
	out.HeightMapData = flatToHeightMap(in.HeightMapData)
	out.RenderHeightMapData = flatToHeightMap(in.RenderHeightMapData)
}

func fixDownSubChunkEntry(in *v2193.SubChunkEntry, out *v2169.SubChunkEntry) {
	out.HeightMapData = heightMapToFlat(in.HeightMapData)
	out.RenderHeightMapData = heightMapToFlat(in.RenderHeightMapData)
}
