package overworld

import (
	"github.com/df-mc/dragonfly/server/internal/vanilla"
)

var biomeTemperatureNoise = func() *vanilla.SimplexNoise {
	r := vanilla.NewLegacyRandom(1234)
	return vanilla.NewSimplexNoiseWithoutOffset(&r)
}()

var biomeFrozenTemperatureLayers = func() [3]*vanilla.SimplexNoise {
	r := vanilla.NewLegacyRandom(3456)
	return [3]*vanilla.SimplexNoise{
		vanilla.NewSimplexNoiseWithoutOffset(&r),
		vanilla.NewSimplexNoiseWithoutOffset(&r),
		vanilla.NewSimplexNoiseWithoutOffset(&r),
	}
}()

func (r *featureRegion) biomeTemperature(p featurePos) float32 {
	biome := normalizeResource(r.f.source.BiomeAt(p.x, p.y, p.z))
	return r.biomeTemperatureFor(biome, p)
}

func (r *featureRegion) biomeTemperatureFor(biome string, p featurePos) float32 {
	biome = normalizeResource(biome)
	base := float32(r.f.temperatures[biome])
	if r.f.biomes[biome].frozenTemperature {
		large := float32(0)
		large += float32(float32(0.14285715) * float32(biomeFrozenTemperatureLayers[0].Sample2D(float64(p.x)*0.05, float64(p.z)*0.05)))
		large += float32(float32(0.2857143) * float32(biomeFrozenTemperatureLayers[1].Sample2D(float64(p.x)*0.025, float64(p.z)*0.025)))
		large += float32(float32(0.5714286) * float32(biomeFrozenTemperatureLayers[2].Sample2D(float64(p.x)*0.0125, float64(p.z)*0.0125)))
		largeVariation := float32(large * 7)
		icePatches := float64(largeVariation) + float64(float32(biomeInfoNoise.Sample2D(float64(p.x)*0.2, float64(p.z)*0.2)))
		if icePatches < 0.3 && float32(biomeInfoNoise.Sample2D(float64(p.x)*0.09, float64(p.z)*0.09)) < 0.8 {
			base = 0.2
		}
	}
	seaLevel := r.f.source.SeaLevel()
	snowLevel := seaLevel + 17
	if p.y > snowLevel {
		x := float64(float32(p.x) / 8)
		z := float64(float32(p.z) / 8)
		v := float32(biomeTemperatureNoise.Sample2D(x, z)) * float32(8)
		heightVariation := (v + float32(p.y) - float32(snowLevel)) * float32(0.05)
		base -= heightVariation / 40
	}
	return base
}

func (r *featureRegion) biomeShouldFreeze(p featurePos, checkNeighbors bool) bool {
	biome := normalizeResource(r.f.source.BiomeAt(p.x, p.y, p.z))
	return r.biomeShouldFreezeFor(biome, p, checkNeighbors)
}

func (r *featureRegion) biomeShouldFreezeFor(biome string, p featurePos, checkNeighbors bool) bool {
	if r.biomeTemperatureFor(biome, p) >= 0.15 || p.y < r.MinY() || p.y > r.MaxY() {
		return false
	}
	if r.blockLight(p) >= 10 {
		return false
	}
	state := r.nativeState(p)
	blockName, _, ok := r.c.BlockRegistry().RuntimeIDToState(state.Block)
	if !ok || !isLiquidBlock(blockName) || featureFluidName(r, p, nil) != "water" {
		return false
	}
	if !checkNeighbors {
		return true
	}
	for _, d := range [...]featurePos{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}} {
		neighbor := featurePos{p.x + d.x, p.y, p.z + d.z}
		if !featureFluidTagContains("minecraft:water", featureFluidName(r, neighbor, nil)) {
			return true
		}
	}
	return false
}

func (r *featureRegion) blockLight(p featurePos) uint8 {
	if p.y < r.MinY() || p.y > r.MaxY() || !r.inTarget(p) {
		return 0
	}
	return r.c.SubChunk(int16(p.y)).BlockLight(uint8(p.x&15), uint8(p.y&15), uint8(p.z&15))
}

func isLiquidBlock(name string) bool {
	switch normalizeResource(name) {
	case "water", "flowing_water", "lava", "flowing_lava":
		return true
	default:
		return false
	}
}
