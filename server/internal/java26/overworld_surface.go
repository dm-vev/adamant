package java26

// OverworldMaterialNoises contains the Java 26.3 NormalNoise instances consumed
// by MaterialSystem and OverworldMaterialRules. Every entry is seeded by the
// RandomState positional factory using its canonical noise registry key.
type OverworldMaterialNoises struct {
	Random PositionalFactory
	Surface, SurfaceSecondary, ClayBandsOffset *NormalNoise
	BadlandsPillar, BadlandsPillarRoof, BadlandsSurface *NormalNoise
	IcebergPillar, IcebergPillarRoof, IcebergSurface *NormalNoise
	SulfurCaveGradient, Swamp, Calcite, Gravel *NormalNoise
	PowderSnow, PackedIce, Ice, SmallPatch *NormalNoise
	BiomeInfo *SimplexNoise
	FrozenTemperatureLayers [3]*SimplexNoise
}

// NewOverworldMaterialNoises builds the noise set used by the Java 26.3
// Overworld material system from NoiseData and Biome's static temperature noise.
func NewOverworldMaterialNoises(seed int64) *OverworldMaterialNoises {
	random := NewXoroshiroRandom(seed)
	factory := random.ForkPositional()
	noise := func(name string, firstOctave int, amplitudes ...float64) *NormalNoise {
		r := factory.FromHashOf("minecraft:" + name)
		return NewNormalNoise(&r, firstOctave, amplitudes...)
	}
	result := &OverworldMaterialNoises{
		Random: factory,
		Surface: noise("surface", -6, 1, 1, 1),
		SurfaceSecondary: noise("surface_secondary", -6, 1, 1, 0, 1),
		ClayBandsOffset: noise("clay_bands_offset", -8, 1),
		BadlandsPillar: noise("badlands_pillar", -2, 1, 1, 1, 1),
		BadlandsPillarRoof: noise("badlands_pillar_roof", -8, 1),
		BadlandsSurface: noise("badlands_surface", -6, 1, 1, 1),
		IcebergPillar: noise("iceberg_pillar", -6, 1, 1, 1, 1),
		IcebergPillarRoof: noise("iceberg_pillar_roof", -3, 1),
		IcebergSurface: noise("iceberg_surface", -6, 1, 1, 1),
		SulfurCaveGradient: noise("sulfur_cave_gradient", -5, 1, 0, 1),
		Swamp: noise("surface_swamp", -2, 1),
		Calcite: noise("calcite", -9, 1, 1, 1, 1),
		Gravel: noise("gravel", -8, 1, 1, 1, 1),
		PowderSnow: noise("powder_snow", -6, 1, 1, 1, 1),
		PackedIce: noise("packed_ice", -7, 1, 1, 1, 1),
		Ice: noise("ice", -4, 1, 1, 1, 1),
		SmallPatch: noise("small_patch", -3, 3),
		BiomeInfo: NewLegacySimplexNoise(2345),
	}
	frozenRandom := NewLegacyRandom(3456)
	result.FrozenTemperatureLayers = [3]*SimplexNoise{
		NewSimplexNoise(&frozenRandom),
		NewSimplexNoise(&frozenRandom),
		NewSimplexNoise(&frozenRandom),
	}
	return result
}
