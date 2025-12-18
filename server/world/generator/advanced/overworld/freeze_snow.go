package overworld

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/biome"
)

// freezeAndSnow matches the final freeze/snow loop in ChunkGeneratorOverworld.populate (Java 1.12).
// It freezes exposed water and places a 1-layer snow layer where temperature permits.
func (g *Overworld) freezeAndSnow(chunkX, chunkZ int, c *chunk.Chunk, biomeIDs []int) {
	if len(biomeIDs) != 16*16 {
		return
	}

	minY, maxY := int16(c.Range().Min()), int16(c.Range().Max())
	if minY > 255 || maxY < 0 {
		return
	}

	chunkMinX, chunkMinZ := chunkX<<4, chunkZ<<4
	packedIceRID := world.BlockRuntimeID(block.PackedIce{})

	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := chunkMinX+lx, chunkMinZ+lz

			precipY := g.precipitationHeightInChunk(c, uint8(lx), uint8(lz))
			if precipY <= 0 || precipY > 255 {
				continue
			}

			biomeID := biome.ID(biomeIDs[lx+lz*16])
			temp := g.floatTemperature(biomeID, wx, precipY, wz)
			if temp >= 0.15 {
				continue
			}

			groundY := precipY - 1
			if groundY < 0 || groundY > 255 {
				continue
			}

			gy := int16(groundY)
			py := int16(precipY)
			if gy < minY || gy > maxY || py < minY || py > maxY {
				continue
			}

			ground := c.Block(uint8(lx), gy, uint8(lz), 0)
			if ground == g.waterRID && py <= maxY && c.Block(uint8(lx), py, uint8(lz), 0) == g.airRID {
				c.SetBlock(uint8(lx), gy, uint8(lz), 0, g.iceRID)
				ground = g.iceRID
			}

			if c.Block(uint8(lx), py, uint8(lz), 0) != g.airRID {
				continue
			}
			if ground == g.airRID || ground == g.waterRID || ground == g.lavaRID || ground == g.iceRID || ground == packedIceRID {
				continue
			}
			name, _, ok := chunk.RuntimeIDToState(ground)
			if ok && name == "minecraft:barrier" {
				continue
			}
			if !g.canSnowRestOn(ground) {
				continue
			}

			c.SetBlock(uint8(lx), py, uint8(lz), 0, g.snowLayerRID)
		}
	}
}

func (g *Overworld) precipitationHeightInChunk(c *chunk.Chunk, x, z uint8) int {
	top := int(c.HighestBlock(x, z))
	if top > 255 {
		top = 255
	}
	for y := top; y >= 0; y-- {
		rid := c.Block(x, int16(y), z, 0)
		if rid == g.airRID {
			continue
		}
		if name, _, ok := chunk.RuntimeIDToState(rid); ok && passableForPrecipitation(name) {
			continue
		}
		return y + 1
	}
	return 0
}

func passableForPrecipitation(name string) bool {
	switch name {
	case "minecraft:short_grass",
		"minecraft:tall_grass",
		"minecraft:large_fern",
		"minecraft:dandelion",
		"minecraft:poppy",
		"minecraft:blue_orchid",
		"minecraft:sunflower",
		"minecraft:deadbush",
		"minecraft:vine",
		"minecraft:red_mushroom",
		"minecraft:brown_mushroom",
		"minecraft:torch":
		return true
	default:
		return false
	}
}

func (g *Overworld) canSnowRestOn(below uint32) bool {
	// Approximation of BlockSnow.canPlaceBlockAt: allow on solid-ish blocks and on leaves.
	if g.isLeaves(below) {
		return true
	}
	if below == g.airRID || below == g.waterRID || below == g.lavaRID {
		return false
	}
	// Many non-full blocks shouldn't support snow layer (fences, torches, etc.). We filter a few common ones by name
	// to avoid obvious issues without needing full face-shape logic.
	if name, _, ok := chunk.RuntimeIDToState(below); ok {
		switch name {
		case "minecraft:torch",
			"minecraft:wall_torch",
			"minecraft:reeds",
			"minecraft:cactus",
			"minecraft:ladder",
			"minecraft:vine":
			return false
		}
	}
	return true
}

func (g *Overworld) floatTemperature(b biome.ID, x, y, z int) float64 {
	base := biomeBaseTemperature(b)
	if y > 64 {
		noise := g.temperatureNoise.GetValue(float64(x)/8.0, float64(z)/8.0) * 4.0
		return base - (noise+float64(y)-64.0)/600.0
	}
	return base
}

func biomeBaseTemperature(id biome.ID) float64 {
	// Values from Biome.registerBiomes() (Java 1.12). Biomes without an explicit temperature use the default 0.5F.
	switch id {
	case biome.Ocean:
		return 0.5
	case biome.Plains:
		return 0.8
	case biome.Desert:
		return 2.0
	case biome.Mountains:
		return 0.2
	case biome.Forest:
		return 0.7
	case biome.Taiga:
		return 0.25
	case biome.Swamp:
		return 0.8
	case biome.River:
		return 0.5
	case biome.FrozenOcean:
		return 0.0
	case biome.FrozenRiver:
		return 0.0
	case biome.SnowyTundra:
		return 0.0
	case biome.SnowyMountains:
		return 0.0
	case biome.MushroomFields:
		return 0.9
	case biome.MushroomFieldShore:
		return 0.9
	case biome.Beach:
		return 0.8
	case biome.DesertHills:
		return 2.0
	case biome.WoodedHills:
		return 0.7
	case biome.TaigaHills:
		return 0.25
	case biome.MountainEdge:
		return 0.2
	case biome.Jungle:
		return 0.95
	case biome.JungleHills:
		return 0.95
	case biome.JungleEdge:
		return 0.95
	case biome.DeepOcean:
		return 0.5
	case biome.StoneShore:
		return 0.2
	case biome.SnowyBeach:
		return 0.05
	case biome.BirchForest:
		return 0.6
	case biome.BirchForestHills:
		return 0.6
	case biome.DarkForest:
		return 0.7
	case biome.SnowyTaiga:
		return -0.5
	case biome.SnowyTaigaHills:
		return -0.5
	case biome.GiantTreeTaiga:
		return 0.3
	case biome.GiantTreeTaigaHills:
		return 0.3
	case biome.WoodedMountains:
		return 0.2
	case biome.Savanna:
		return 1.2
	case biome.SavannaPlateau:
		return 1.0
	case biome.Badlands:
		return 2.0
	case biome.WoodedBadlandsPlateau:
		return 2.0
	case biome.BadlandsPlateau:
		return 2.0
	case biome.SunflowerPlains:
		return 0.8
	case biome.DesertM:
		return 2.0
	case biome.GravellyMountains:
		return 0.2
	case biome.FlowerForest:
		return 0.7
	case biome.TaigaMountains:
		return 0.25
	case biome.SwampHills:
		return 0.8
	case biome.IceSpikes:
		return 0.0
	case biome.ModifiedJungle:
		return 0.95
	case biome.ModifiedJungleEdge:
		return 0.95
	case biome.TallBirchForest:
		return 0.6
	case biome.TallBirchHills:
		return 0.6
	case biome.DarkForestHills:
		return 0.7
	case biome.SnowyTaigaMountains:
		return -0.5
	case biome.GiantSpruceTaiga:
		return 0.25
	case biome.GiantSpruceTaigaHills:
		return 0.25
	case biome.ModifiedGravellyMountains:
		return 0.2
	case biome.ShatteredSavanna:
		return 1.1
	case biome.ShatteredSavannaPlateau:
		return 1.0
	case biome.ErodedBadlands:
		return 2.0
	case biome.ModifiedWoodedBadlandsPlateau:
		return 2.0
	case biome.ModifiedBadlandsPlateau:
		return 2.0
	default:
		return 0.5
	}
}

