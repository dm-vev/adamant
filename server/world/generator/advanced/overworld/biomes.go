package overworld

import (
	dfbiome "github.com/df-mc/dragonfly/server/world/biome"
	mcbiome "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/biome"
)

func (g *Overworld) initBiomeDefs() {
	// Default all unknown biomes to plains.
	def := biomeDef{
		baseHeight:      0.125,
		heightVariation: 0.05,
		topRID:          g.grassRID,
		fillerRID:       g.dirtRID,
		biomeID:         uint32(dfbiome.Plains{}.EncodeBiome()),
	}
	for i := range g.biomes {
		g.biomes[i] = def
	}

	for _, e := range []struct {
		id    mcbiome.ID
		depth float32
		scale float32
	}{
		{id: mcbiome.Ocean, depth: -1.000, scale: 0.100},
		{id: mcbiome.Plains, depth: 0.125, scale: 0.050},
		{id: mcbiome.Desert, depth: 0.125, scale: 0.050},
		{id: mcbiome.Mountains, depth: 1.000, scale: 0.500},
		{id: mcbiome.Forest, depth: 0.100, scale: 0.200},
		{id: mcbiome.Taiga, depth: 0.200, scale: 0.200},
		{id: mcbiome.Swamp, depth: -0.200, scale: 0.100},
		{id: mcbiome.River, depth: -0.500, scale: 0.000},
		{id: mcbiome.FrozenOcean, depth: -1.000, scale: 0.100},
		{id: mcbiome.FrozenRiver, depth: -0.500, scale: 0.000},
		{id: mcbiome.SnowyTundra, depth: 0.125, scale: 0.050},
		{id: mcbiome.SnowyMountains, depth: 0.450, scale: 0.300},
		{id: mcbiome.MushroomFields, depth: 0.200, scale: 0.300},
		{id: mcbiome.MushroomFieldShore, depth: 0.000, scale: 0.025},
		{id: mcbiome.Beach, depth: 0.000, scale: 0.025},
		{id: mcbiome.DesertHills, depth: 0.450, scale: 0.300},
		{id: mcbiome.WoodedHills, depth: 0.450, scale: 0.300},
		{id: mcbiome.TaigaHills, depth: 0.450, scale: 0.300},
		{id: mcbiome.MountainEdge, depth: 0.800, scale: 0.300},
		{id: mcbiome.Jungle, depth: 0.100, scale: 0.200},
		{id: mcbiome.JungleHills, depth: 0.450, scale: 0.300},
		{id: mcbiome.JungleEdge, depth: 0.100, scale: 0.200},
		{id: mcbiome.DeepOcean, depth: -1.800, scale: 0.100},
		{id: mcbiome.StoneShore, depth: 0.100, scale: 0.800},
		{id: mcbiome.SnowyBeach, depth: 0.000, scale: 0.025},
		{id: mcbiome.BirchForest, depth: 0.100, scale: 0.200},
		{id: mcbiome.BirchForestHills, depth: 0.450, scale: 0.300},
		{id: mcbiome.DarkForest, depth: 0.100, scale: 0.200},
		{id: mcbiome.SnowyTaiga, depth: 0.200, scale: 0.200},
		{id: mcbiome.SnowyTaigaHills, depth: 0.450, scale: 0.300},
		{id: mcbiome.GiantTreeTaiga, depth: 0.200, scale: 0.200},
		{id: mcbiome.GiantTreeTaigaHills, depth: 0.450, scale: 0.300},
		{id: mcbiome.WoodedMountains, depth: 1.000, scale: 0.500},
		{id: mcbiome.Savanna, depth: 0.125, scale: 0.050},
		{id: mcbiome.SavannaPlateau, depth: 1.500, scale: 0.025},
		{id: mcbiome.Badlands, depth: 0.100, scale: 0.200},
		{id: mcbiome.WoodedBadlandsPlateau, depth: 1.500, scale: 0.025},
		{id: mcbiome.BadlandsPlateau, depth: 1.500, scale: 0.025},

		{id: mcbiome.SunflowerPlains, depth: 0.125, scale: 0.050},
		{id: mcbiome.DesertM, depth: 0.225, scale: 0.250},
		{id: mcbiome.GravellyMountains, depth: 1.000, scale: 0.500},
		{id: mcbiome.FlowerForest, depth: 0.100, scale: 0.400},
		{id: mcbiome.TaigaMountains, depth: 0.300, scale: 0.400},
		{id: mcbiome.SwampHills, depth: -0.100, scale: 0.300},
		{id: mcbiome.IceSpikes, depth: 0.425, scale: 0.450},
		{id: mcbiome.ModifiedJungle, depth: 0.200, scale: 0.400},
		{id: mcbiome.ModifiedJungleEdge, depth: 0.200, scale: 0.400},
		{id: mcbiome.TallBirchForest, depth: 0.200, scale: 0.400},
		{id: mcbiome.TallBirchHills, depth: 0.550, scale: 0.500},
		{id: mcbiome.DarkForestHills, depth: 0.200, scale: 0.400},
		{id: mcbiome.SnowyTaigaMountains, depth: 0.300, scale: 0.400},
		{id: mcbiome.GiantSpruceTaiga, depth: 0.200, scale: 0.200},
		{id: mcbiome.GiantSpruceTaigaHills, depth: 0.200, scale: 0.200},
		{id: mcbiome.ModifiedGravellyMountains, depth: 1.000, scale: 0.500},
		{id: mcbiome.ShatteredSavanna, depth: 0.3625, scale: 1.225},
		{id: mcbiome.ShatteredSavannaPlateau, depth: 1.050, scale: 1.212},
		{id: mcbiome.ErodedBadlands, depth: 0.100, scale: 0.200},
		{id: mcbiome.ModifiedWoodedBadlandsPlateau, depth: 0.450, scale: 0.300},
		{id: mcbiome.ModifiedBadlandsPlateau, depth: 0.450, scale: 0.300},
	} {
		top, filler := g.surfaceForBiome(e.id)
		bid := bedrockBiomeID(e.id)
		g.biomes[int(e.id)] = biomeDef{
			baseHeight:      e.depth,
			heightVariation: e.scale,
			topRID:          top,
			fillerRID:       filler,
			biomeID:         bid,
		}
	}
}

func (g *Overworld) surfaceForBiome(id mcbiome.ID) (top, filler uint32) {
	switch id {
	case mcbiome.Ocean, mcbiome.DeepOcean, mcbiome.FrozenOcean, mcbiome.River, mcbiome.FrozenRiver, mcbiome.Beach, mcbiome.SnowyBeach:
		return g.sandRID, g.sandRID
	case mcbiome.StoneShore:
		return g.stoneRID, g.stoneRID
	case mcbiome.Desert, mcbiome.DesertHills, mcbiome.DesertM:
		return g.sandRID, g.sandRID
	case mcbiome.Badlands, mcbiome.WoodedBadlandsPlateau, mcbiome.BadlandsPlateau, mcbiome.ErodedBadlands, mcbiome.ModifiedWoodedBadlandsPlateau, mcbiome.ModifiedBadlandsPlateau:
		return g.redSandRID, g.terracottaRID
	case mcbiome.MushroomFields, mcbiome.MushroomFieldShore:
		return g.myceliumRID, g.dirtRID
	case mcbiome.GiantTreeTaiga, mcbiome.GiantTreeTaigaHills, mcbiome.GiantSpruceTaiga, mcbiome.GiantSpruceTaigaHills:
		return g.podzolRID, g.dirtRID
	case mcbiome.GravellyMountains, mcbiome.ModifiedGravellyMountains:
		return g.gravelRID, g.stoneRID
	default:
		return g.grassRID, g.dirtRID
	}
}

func bedrockBiomeID(id mcbiome.ID) uint32 {
	switch id {
	case mcbiome.Ocean:
		return uint32(dfbiome.Ocean{}.EncodeBiome())
	case mcbiome.Plains:
		return uint32(dfbiome.Plains{}.EncodeBiome())
	case mcbiome.Desert:
		return uint32(dfbiome.Desert{}.EncodeBiome())
	case mcbiome.Mountains:
		return uint32(dfbiome.WindsweptHills{}.EncodeBiome())
	case mcbiome.Forest:
		return uint32(dfbiome.Forest{}.EncodeBiome())
	case mcbiome.Taiga:
		return uint32(dfbiome.Taiga{}.EncodeBiome())
	case mcbiome.Swamp:
		return uint32(dfbiome.Swamp{}.EncodeBiome())
	case mcbiome.River:
		return uint32(dfbiome.River{}.EncodeBiome())
	case mcbiome.FrozenOcean:
		return uint32(dfbiome.FrozenOcean{}.EncodeBiome())
	case mcbiome.FrozenRiver:
		return uint32(dfbiome.FrozenRiver{}.EncodeBiome())
	case mcbiome.SnowyTundra:
		return uint32(dfbiome.SnowyPlains{}.EncodeBiome())
	case mcbiome.SnowyMountains:
		return uint32(dfbiome.SnowyMountains{}.EncodeBiome())
	case mcbiome.MushroomFields:
		return uint32(dfbiome.MushroomFields{}.EncodeBiome())
	case mcbiome.MushroomFieldShore:
		return uint32(dfbiome.MushroomFieldShore{}.EncodeBiome())
	case mcbiome.Beach:
		return uint32(dfbiome.Beach{}.EncodeBiome())
	case mcbiome.DesertHills:
		return uint32(dfbiome.DesertHills{}.EncodeBiome())
	case mcbiome.WoodedHills:
		return uint32(dfbiome.WoodedHills{}.EncodeBiome())
	case mcbiome.TaigaHills:
		return uint32(dfbiome.TaigaHills{}.EncodeBiome())
	case mcbiome.MountainEdge:
		return uint32(dfbiome.MountainEdge{}.EncodeBiome())
	case mcbiome.Jungle:
		return uint32(dfbiome.Jungle{}.EncodeBiome())
	case mcbiome.JungleHills:
		return uint32(dfbiome.JungleHills{}.EncodeBiome())
	case mcbiome.JungleEdge:
		return uint32(dfbiome.JungleEdge{}.EncodeBiome())
	case mcbiome.DeepOcean:
		return uint32(dfbiome.DeepOcean{}.EncodeBiome())
	case mcbiome.StoneShore:
		return uint32(dfbiome.StonyShore{}.EncodeBiome())
	case mcbiome.SnowyBeach:
		return uint32(dfbiome.SnowyBeach{}.EncodeBiome())
	case mcbiome.BirchForest:
		return uint32(dfbiome.BirchForest{}.EncodeBiome())
	case mcbiome.BirchForestHills:
		return uint32(dfbiome.BirchForestHills{}.EncodeBiome())
	case mcbiome.DarkForest:
		return uint32(dfbiome.DarkForest{}.EncodeBiome())
	case mcbiome.SnowyTaiga:
		return uint32(dfbiome.SnowyTaiga{}.EncodeBiome())
	case mcbiome.SnowyTaigaHills:
		return uint32(dfbiome.SnowyTaigaHills{}.EncodeBiome())
	case mcbiome.GiantTreeTaiga:
		return uint32(dfbiome.OldGrowthPineTaiga{}.EncodeBiome())
	case mcbiome.GiantTreeTaigaHills:
		return uint32(dfbiome.GiantTreeTaigaHills{}.EncodeBiome())
	case mcbiome.WoodedMountains:
		return uint32(dfbiome.WindsweptForest{}.EncodeBiome())
	case mcbiome.Savanna:
		return uint32(dfbiome.Savanna{}.EncodeBiome())
	case mcbiome.SavannaPlateau:
		return uint32(dfbiome.SavannaPlateau{}.EncodeBiome())
	case mcbiome.Badlands:
		return uint32(dfbiome.Badlands{}.EncodeBiome())
	case mcbiome.WoodedBadlandsPlateau:
		return uint32(dfbiome.WoodedBadlandsPlateau{}.EncodeBiome())
	case mcbiome.BadlandsPlateau:
		return uint32(dfbiome.BadlandsPlateau{}.EncodeBiome())
	case mcbiome.SunflowerPlains:
		return uint32(dfbiome.SunflowerPlains{}.EncodeBiome())
	case mcbiome.DesertM:
		return uint32(dfbiome.DesertLakes{}.EncodeBiome())
	case mcbiome.GravellyMountains:
		return uint32(dfbiome.WindsweptGravellyHills{}.EncodeBiome())
	case mcbiome.FlowerForest:
		return uint32(dfbiome.FlowerForest{}.EncodeBiome())
	case mcbiome.TaigaMountains:
		return uint32(dfbiome.TaigaMountains{}.EncodeBiome())
	case mcbiome.SwampHills:
		return uint32(dfbiome.SwampHills{}.EncodeBiome())
	case mcbiome.IceSpikes:
		return uint32(dfbiome.IceSpikes{}.EncodeBiome())
	case mcbiome.ModifiedJungle:
		return uint32(dfbiome.ModifiedJungle{}.EncodeBiome())
	case mcbiome.ModifiedJungleEdge:
		return uint32(dfbiome.ModifiedJungleEdge{}.EncodeBiome())
	case mcbiome.TallBirchForest:
		return uint32(dfbiome.OldGrowthBirchForest{}.EncodeBiome())
	case mcbiome.TallBirchHills:
		return uint32(dfbiome.TallBirchHills{}.EncodeBiome())
	case mcbiome.DarkForestHills:
		return uint32(dfbiome.DarkForestHills{}.EncodeBiome())
	case mcbiome.SnowyTaigaMountains:
		return uint32(dfbiome.SnowyTaigaMountains{}.EncodeBiome())
	case mcbiome.GiantSpruceTaiga:
		return uint32(dfbiome.OldGrowthSpruceTaiga{}.EncodeBiome())
	case mcbiome.GiantSpruceTaigaHills:
		return uint32(dfbiome.GiantSpruceTaigaHills{}.EncodeBiome())
	case mcbiome.ModifiedGravellyMountains:
		return uint32(dfbiome.GravellyMountainsPlus{}.EncodeBiome())
	case mcbiome.ShatteredSavanna:
		return uint32(dfbiome.WindsweptSavanna{}.EncodeBiome())
	case mcbiome.ShatteredSavannaPlateau:
		return uint32(dfbiome.ShatteredSavannaPlateau{}.EncodeBiome())
	case mcbiome.ErodedBadlands:
		return uint32(dfbiome.ErodedBadlands{}.EncodeBiome())
	case mcbiome.ModifiedWoodedBadlandsPlateau:
		return uint32(dfbiome.ModifiedWoodedBadlandsPlateau{}.EncodeBiome())
	case mcbiome.ModifiedBadlandsPlateau:
		return uint32(dfbiome.ModifiedBadlandsPlateau{}.EncodeBiome())
	default:
		return uint32(dfbiome.Plains{}.EncodeBiome())
	}
}
