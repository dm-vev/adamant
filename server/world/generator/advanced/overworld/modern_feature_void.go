package overworld

import "github.com/df-mc/dragonfly/server/world"

func placeModernVoidStartPlatform(region *featureRegion, origin featurePos) bool {
	chunkX, chunkZ := origin.x>>4, origin.z>>4
	if absFeatureInt(chunkX) > 1 || absFeatureInt(chunkZ) > 1 {
		return true
	}
	stone, err := world.ResolveState(region.c.BlockRegistry(), world.BlockState{Name: "minecraft:stone"})
	if err != nil {
		panic(err)
	}
	cobble, err := world.ResolveState(region.c.BlockRegistry(), world.BlockState{Name: "minecraft:cobblestone"})
	if err != nil {
		panic(err)
	}
	for z := chunkZ << 4; z < (chunkZ<<4)+16; z++ {
		for x := chunkX << 4; x < (chunkX<<4)+16; x++ {
			if absFeatureInt(x-8) > 16 || absFeatureInt(z-8) > 16 {
				continue
			}
			state := stone
			if x == 8 && z == 8 {
				state = cobble
			}
			region.Set(featurePos{x, origin.y + 3, z}, state)
		}
	}
	return true
}
