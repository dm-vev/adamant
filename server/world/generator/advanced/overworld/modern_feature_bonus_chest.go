package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
)

func placeModernBonusChest(region *featureRegion, origin featurePos, random *java26.Random) bool {
	var xs, zs [16]int32
	for i := range xs {
		xs[i] = (origin.x>>4)<<4 + int32(i)
		zs[i] = (origin.z>>4)<<4 + int32(i)
	}
	// Util.toShuffledList runs each complete backward shuffle before the next.
	for _, positions := range []*[16]int32{&xs, &zs} {
		for i := int32(16); i > 1; i-- {
			j := random.NextInt(i)
			positions[i-1], positions[j] = positions[j], positions[i-1]
		}
	}
	for _, x := range xs {
		for _, z := range zs {
			p := featurePos{x, region.placementHeight(x, z, "MOTION_BLOCKING_NO_LEAVES"), z}
			name, properties := region.State(p)
			shape, err := javadata.BlockGenerationShape(name, properties)
			if err != nil {
				panic(err)
			}
			if !featureRegionAir(region, p) && !shape.CollisionEmpty {
				continue
			}
			chest := featureRequireState(region, "minecraft:chest")
			if region.Set(p, chest) {
				// Java draws only when the placed block has a RandomizableContainer actor.
				featureRecordJavaBlockEntity(region, p, map[string]any{"id": "minecraft:chest", "LootTable": "minecraft:chests/spawn_bonus_chest", "LootTableSeed": random.NextLong()})
			}
			torch := featureRequireState(region, "minecraft:torch")
			for _, d := range [...]featurePos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}} {
				torchPos := featurePos{p.x + d.x, p.y, p.z + d.z}
				name, properties := region.State(featurePos{torchPos.x, torchPos.y - 1, torchPos.z})
				support, err := javadata.BlockGenerationShape(name, properties)
				if err != nil {
					panic(err)
				}
				if support.Center&(1<<uint(cube.FaceUp)) != 0 {
					region.Set(torchPos, torch)
				}
			}
			return true
		}
	}
	return false
}
