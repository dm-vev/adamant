package populate

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func inChunk(pos cube.Pos, chunkPos world.ChunkPos) bool {
	return int32(pos[0]>>4) == chunkPos[0] && int32(pos[2]>>4) == chunkPos[1]
}

var setOpts = &world.SetOpts{
	DisableBlockUpdates:       true,
	DisableLiquidDisplacement: true,
}
