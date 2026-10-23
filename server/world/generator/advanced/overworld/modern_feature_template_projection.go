package overworld

import (
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func applyTemplateProjectionBlocks(region *featureRegion, blocks []structure.Block, flags int) {
	registry := region.c.BlockRegistry()
	for _, entry := range blocks {
		pos := featurePos{entry.Pos[0], entry.Pos[1], entry.Pos[2]}
		if pos.y < region.MinY() || pos.y > region.MaxY() ||
			absFeatureInt(pos.x>>4-region.writeOriginX) > 1 ||
			absFeatureInt(pos.z>>4-region.writeOriginZ) > 1 {
			continue
		}
		state, err := entry.ResolveNative(registry, region.nativeState(pos))
		if err != nil {
			panic(err)
		}
		if !region.setProjectionWithFlags(pos, state, flags) {
			continue
		}
		region.recordTemplateActor(entry)
	}
}
