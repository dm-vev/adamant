package overworld

import (
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
	mcbiome "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/biome"
)

func isMesaBiome(id mcbiome.ID) bool {
	switch id {
	case mcbiome.Badlands,
		mcbiome.BadlandsPlateau,
		mcbiome.WoodedBadlandsPlateau,
		mcbiome.ErodedBadlands,
		mcbiome.ModifiedBadlandsPlateau,
		mcbiome.ModifiedWoodedBadlandsPlateau:
		return true
	default:
		return false
	}
}

func (g *Overworld) initMesaBands() {
	// Matches the clay band generation in BiomeMesa.generateBands (Java 1.12), but using runtime IDs.
	for i := range g.mesaBands {
		g.mesaBands[i] = g.terracottaRID
	}

	r := mc112.NewRand(g.seed)

	orange := g.stainedTerracottaRIDs[1]
	yellow := g.stainedTerracottaRIDs[4]
	brown := g.stainedTerracottaRIDs[12]
	red := g.stainedTerracottaRIDs[14]
	white := g.stainedTerracottaRIDs[0]
	lightGrey := g.stainedTerracottaRIDs[8]

	for i := 0; i < 64; i++ {
		i += int(r.Intn(5)) + 1
		if i < 64 {
			g.mesaBands[i] = orange
		}
	}

	for k := 0; k < int(r.Intn(4))+2; k++ {
		start := int(r.Intn(64))
		length := int(r.Intn(3)) + 1
		for i := 0; i < length && start+i < 64; i++ {
			g.mesaBands[start+i] = yellow
		}
	}

	for k := 0; k < int(r.Intn(4))+2; k++ {
		start := int(r.Intn(64))
		length := int(r.Intn(3)) + 2
		for i := 0; i < length && start+i < 64; i++ {
			g.mesaBands[start+i] = brown
		}
	}

	for k := 0; k < int(r.Intn(4))+2; k++ {
		start := int(r.Intn(64))
		length := int(r.Intn(3)) + 1
		for i := 0; i < length && start+i < 64; i++ {
			g.mesaBands[start+i] = red
		}
	}

	count := int(r.Intn(3)) + 3
	pos := 0
	for i := 0; i < count; i++ {
		pos += int(r.Intn(16)) + 4
		if pos >= 64 {
			break
		}
		g.mesaBands[pos] = white
		if pos > 1 && r.Intn(2) == 0 {
			g.mesaBands[pos-1] = lightGrey
		}
		if pos < 63 && r.Intn(2) == 0 {
			g.mesaBands[pos+1] = lightGrey
		}
	}
}

func (g *Overworld) mesaBandRID(worldX, worldZ int, y int16) uint32 {
	// Vanilla varies band offset with a low-frequency perlin noise.
	// The factor 1/512 matches the overall scale used by BiomeMesa in Java 1.12.
	d := g.mesaBandNoise.GetValue(float64(worldX)/512.0, float64(worldZ)/512.0)
	off := int(d * 2.0)
	idx := (int(y) + off) % 64
	if idx < 0 {
		idx += 64
	}
	return g.mesaBands[idx]
}

func (g *Overworld) generateMesaTerrainColumn(c *chunk.Chunk, id mcbiome.ID, x, z uint8, worldX, worldZ int, noiseVal float64, r *mc112.Rand, minY, maxY int16) {
	// Approximates BiomeMesa.genTerrainBlocks using banded terracotta, with red sand or grass/dirt at the top.
	thickness := int32(noiseVal/3.0 + 3.0 + r.Float64()*0.25)
	layer := int32(-1)

	orangeTerracotta := g.stainedTerracottaRIDs[1]
	top := g.redSandRID
	fillerTop := orangeTerracotta
	switch id {
	case mcbiome.BadlandsPlateau, mcbiome.WoodedBadlandsPlateau, mcbiome.ModifiedBadlandsPlateau, mcbiome.ModifiedWoodedBadlandsPlateau:
		top = g.grassRID
		fillerTop = g.dirtRID
	}

	for y := maxY; y >= minY; y-- {
		if y <= int16(r.Intn(5)) {
			c.SetBlock(x, y, z, 0, g.bedrockRID)
			continue
		}

		current := c.Block(x, y, z, 0)
		if current == g.airRID {
			layer = -1
			continue
		}
		if current != g.stoneRID {
			continue
		}

		if layer == -1 {
			layer = thickness
			if layer < 0 {
				layer = 0
			}
			if int(y) >= javaSeaLevel-1 {
				c.SetBlock(x, y, z, 0, top)
			} else {
				c.SetBlock(x, y, z, 0, g.mesaBandRID(worldX, worldZ, y))
			}
			continue
		}

		if layer > 0 {
			layer--
			// A short section of "surface filler" directly under the top (orange terracotta or dirt).
			if int(y) >= javaSeaLevel-1 && thickness > 0 && (thickness-layer) <= 3 {
				c.SetBlock(x, y, z, 0, fillerTop)
				continue
			}
			c.SetBlock(x, y, z, 0, g.mesaBandRID(worldX, worldZ, y))
			continue
		}

	// Mesa biomes replace most near-surface stone with terracotta bands.
	c.SetBlock(x, y, z, 0, g.mesaBandRID(worldX, worldZ, y))
	}
}
