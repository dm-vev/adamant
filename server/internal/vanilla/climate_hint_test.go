package vanilla

import (
	"math/rand/v2"
	"testing"
)

// TestClimateHintKeepsBiomes checks that searching from the leaf of an
// earlier search returns the biome of an unhinted search, for walks through
// nearby climates, jumps between unrelated ones and climates on a coarse grid
// that lie on leaf borders, where equally near leaves tie.
func TestClimateHintKeepsBiomes(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	var climate [6]float32
	var hint ClimateHint
	for i := range 300000 {
		for axis := range climate {
			switch {
			case i >= 200000:
				climate[axis] = float32(r.IntN(41)-20) / 10
			case i%50 == 0:
				climate[axis] = r.Float32()*4 - 2
			default:
				climate[axis] = max(-2, min(2, climate[axis]+(r.Float32()-0.5)*0.05))
			}
		}
		if got, want := findOverworldBiomeIndex(climate, &hint), findOverworldBiomeIndex(climate, nil); got != want {
			t.Fatalf("climate %v: hinted search found %s, unhinted %s", climate, OverworldBiomeName(got), OverworldBiomeName(want))
		}
	}
}
