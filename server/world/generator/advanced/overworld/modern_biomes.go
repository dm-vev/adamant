package overworld

import (
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
)

// Java climate indices map to the registered Bedrock biome IDs, not Java IDs.
var modernBiomeIDs = [vanilla.OverworldBiomeCount]uint32{
	uint32(biome.MushroomFields{}.EncodeBiome()), // mushroom_fields
	uint32(biome.DeepFrozenOcean{}.EncodeBiome()), // deep_frozen_ocean
	uint32(biome.FrozenOcean{}.EncodeBiome()), // frozen_ocean
	uint32(biome.DeepColdOcean{}.EncodeBiome()), // deep_cold_ocean
	uint32(biome.ColdOcean{}.EncodeBiome()), // cold_ocean
	uint32(biome.DeepOcean{}.EncodeBiome()), // deep_ocean
	uint32(biome.Ocean{}.EncodeBiome()), // ocean
	uint32(biome.DeepLukewarmOcean{}.EncodeBiome()), // deep_lukewarm_ocean
	uint32(biome.LukewarmOcean{}.EncodeBiome()), // lukewarm_ocean
	uint32(biome.WarmOcean{}.EncodeBiome()), // warm_ocean
	uint32(biome.StonyShore{}.EncodeBiome()), // stony_shore
	uint32(biome.Swamp{}.EncodeBiome()), // swamp
	uint32(biome.MangroveSwamp{}.EncodeBiome()), // mangrove_swamp
	uint32(biome.SnowySlopes{}.EncodeBiome()), // snowy_slopes
	uint32(biome.SnowyPlains{}.EncodeBiome()), // snowy_plains
	uint32(biome.SnowyBeach{}.EncodeBiome()), // snowy_beach
	uint32(biome.WindsweptGravellyHills{}.EncodeBiome()), // windswept_gravelly_hills
	uint32(biome.Grove{}.EncodeBiome()), // grove
	uint32(biome.WindsweptHills{}.EncodeBiome()), // windswept_hills
	uint32(biome.SnowyTaiga{}.EncodeBiome()), // snowy_taiga
	uint32(biome.WindsweptForest{}.EncodeBiome()), // windswept_forest
	uint32(biome.Taiga{}.EncodeBiome()), // taiga
	uint32(biome.Plains{}.EncodeBiome()), // plains
	uint32(biome.Meadow{}.EncodeBiome()), // meadow
	uint32(biome.Beach{}.EncodeBiome()), // beach
	uint32(biome.Forest{}.EncodeBiome()), // forest
	uint32(biome.OldGrowthSpruceTaiga{}.EncodeBiome()), // old_growth_spruce_taiga
	uint32(biome.FlowerForest{}.EncodeBiome()), // flower_forest
	uint32(biome.BirchForest{}.EncodeBiome()), // birch_forest
	uint32(biome.DarkForest{}.EncodeBiome()), // dark_forest
	uint32(biome.PaleGarden{}.EncodeBiome()), // pale_garden
	uint32(biome.SavannaPlateau{}.EncodeBiome()), // savanna_plateau
	uint32(biome.Savanna{}.EncodeBiome()), // savanna
	uint32(biome.Jungle{}.EncodeBiome()), // jungle
	uint32(biome.Badlands{}.EncodeBiome()), // badlands
	uint32(biome.Desert{}.EncodeBiome()), // desert
	uint32(biome.WoodedBadlandsPlateau{}.EncodeBiome()), // wooded_badlands
	uint32(biome.JaggedPeaks{}.EncodeBiome()), // jagged_peaks
	uint32(biome.StonyPeaks{}.EncodeBiome()), // stony_peaks
	uint32(biome.FrozenRiver{}.EncodeBiome()), // frozen_river
	uint32(biome.River{}.EncodeBiome()), // river
	uint32(biome.IceSpikes{}.EncodeBiome()), // ice_spikes
	uint32(biome.DappledForest{}.EncodeBiome()), // dappled_forest
	uint32(biome.OldGrowthPineTaiga{}.EncodeBiome()), // old_growth_pine_taiga
	uint32(biome.SunflowerPlains{}.EncodeBiome()), // sunflower_plains
	uint32(biome.OldGrowthBirchForest{}.EncodeBiome()), // old_growth_birch_forest
	uint32(biome.JungleEdge{}.EncodeBiome()), // sparse_jungle
	uint32(biome.BambooJungle{}.EncodeBiome()), // bamboo_jungle
	uint32(biome.ErodedBadlands{}.EncodeBiome()), // eroded_badlands
	uint32(biome.WindsweptSavanna{}.EncodeBiome()), // windswept_savanna
	uint32(biome.CherryGrove{}.EncodeBiome()), // cherry_grove
	uint32(biome.FrozenPeaks{}.EncodeBiome()), // frozen_peaks
	uint32(biome.DripstoneCaves{}.EncodeBiome()), // dripstone_caves
	uint32(biome.LushCaves{}.EncodeBiome()), // lush_caves
	uint32(biome.SulfurCaves{}.EncodeBiome()), // sulfur_caves
	uint32(biome.DeepDark{}.EncodeBiome()), // deep_dark
}

var modernBiomeNames = func() (names [vanilla.OverworldBiomeCount]string) {
	for i := range names {
		names[i] = vanilla.OverworldBiomeName(uint8(i))
	}
	return
}()
