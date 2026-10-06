package biome

import "image/color"

// DappledForest is the dappled forest biome.
type DappledForest struct{}

func (DappledForest) Temperature() float64 { return 0.6 }
func (DappledForest) Rainfall() float64    { return 0.6 }
func (DappledForest) Depth() float64       { return 0.1 }
func (DappledForest) Scale() float64       { return 0.2 }
func (DappledForest) WaterColour() color.RGBA {
	return color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}
}
func (DappledForest) Tags() []string {
	return []string{"animal", "dappled_forest", "forest", "has_structure_abandoned_camp", "monster", "overworld", "spawns_cold_variant_farm_animals"}
}
func (DappledForest) String() string   { return "dappled_forest" }
func (DappledForest) EncodeBiome() int { return 195 }
