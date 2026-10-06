package bedrock

import "math"

// CaveDensity is the immutable cave portion of the modern Overworld density
// graph. Sample returns the native density units used by Bedrock (128 times
// MCP's unit density); terrain slides, interpolation, squeeze and beardifier
// are deliberately outside this sampler.
type CaveDensity struct {
	pillar, pillarRareness, pillarThickness                                       NormalNoise
	spaghetti2D, spaghetti2DElevation, spaghetti2DModulator, spaghetti2DThickness NormalNoise
	spaghetti3D1, spaghetti3D2, spaghetti3DRarity, spaghetti3DThickness           NormalNoise
	spaghettiRoughness, spaghettiRoughnessModulator                               NormalNoise
	caveEntrance, caveLayer, caveCheese                                           NormalNoise
}

// NewCaveDensity constructs the Overworld cave noises from their named
// Bedrock positional streams. Noise definitions and octave lists match
// NoiseData.bootstrap (MC 26.3); names include the minecraft: resource prefix.
func NewCaveDensity(seed int64) *CaveDensity {
	f := NewRandomFactory(seed)
	return &CaveDensity{
		pillar:                      f.Noise("minecraft:pillar", -7, 1, 1),
		pillarRareness:              f.Noise("minecraft:pillar_rareness", -8, 1),
		pillarThickness:             f.Noise("minecraft:pillar_thickness", -8, 1),
		spaghetti2D:                 f.Noise("minecraft:spaghetti_2d", -7, 1),
		spaghetti2DElevation:        f.Noise("minecraft:spaghetti_2d_elevation", -8, 1),
		spaghetti2DModulator:        f.Noise("minecraft:spaghetti_2d_modulator", -11, 1),
		spaghetti2DThickness:        f.Noise("minecraft:spaghetti_2d_thickness", -11, 1),
		spaghetti3D1:                f.Noise("minecraft:spaghetti_3d_1", -7, 1),
		spaghetti3D2:                f.Noise("minecraft:spaghetti_3d_2", -7, 1),
		spaghetti3DRarity:           f.Noise("minecraft:spaghetti_3d_rarity", -11, 1),
		spaghetti3DThickness:        f.Noise("minecraft:spaghetti_3d_thickness", -8, 1),
		spaghettiRoughness:          f.Noise("minecraft:spaghetti_roughness", -5, 1),
		spaghettiRoughnessModulator: f.Noise("minecraft:spaghetti_roughness_modulator", -8, 1),
		caveEntrance:                f.Noise("minecraft:cave_entrance", -7, 0.4, 0.5, 1),
		caveLayer:                   f.Noise("minecraft:cave_layer", -8, 1),
		caveCheese:                  f.Noise("minecraft:cave_cheese", -8, 0.5, 1, 2, 1, 2, 1, 0, 2, 0),
	}
}

const caveDensityScale = float32(128)

func caveAbs(value float32) float32 {
	return math.Float32frombits(math.Float32bits(value) & 0x7fffffff)
}

func caveClamp(value, low, high float32) float32 {
	return caveMin(caveMax(value, low), high)
}

func caveMin(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func caveMax(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func caveMappedNoise(value, minTarget, maxTarget float32) float32 {
	return float32((value+1)*float32((maxTarget-minTarget)*0.5) + minTarget)
}

func caveYGradient(y, from, to int32, start, end float32) float32 {
	return float32(start + (end-start)*caveClamp(float32(y-from)/float32(to-from), 0, 1))
}

// spaghettiRoughness is shared by entrance and underground cave subtraction.
func (c *CaveDensity) spaghettiRoughnessAt(x, y, z float32) float32 {
	mod := c.spaghettiRoughnessModulator.Sample(x, y, z)
	mod = float32(float32(mod+1) * 0.5)
	mod = float32(mod * 0.1)
	rough := float32(0.4 - caveAbs(c.spaghettiRoughness.Sample(x, y, z)))
	return float32(mod * rough)
}

func (c *CaveDensity) spaghetti3DAt(x, y, z float32) float32 {
	rarityInput := c.spaghetti3DRarity.Sample(float32(x*2), y, float32(z*2))
	var rarity float32
	switch {
	case rarityInput < -0.5:
		rarity = 0.75
	case rarityInput < 0:
		rarity = 1
	case rarityInput < 0.5:
		rarity = 1.5
	default:
		rarity = 2
	}
	invRarity := float32(1 / rarity)
	first := float32(c.spaghetti3D1.Sample(float32(x*invRarity), float32(y*invRarity), float32(z*invRarity)) * rarity)
	second := float32(c.spaghetti3D2.Sample(float32(x*invRarity), float32(y*invRarity), float32(z*invRarity)) * rarity)
	thickness := caveMappedNoise(c.spaghetti3DThickness.Sample(x, y, z), -0.065, -0.088)
	return caveClamp(float32(caveMax(caveAbs(first), caveAbs(second))+thickness), -1, 1)
}

func (c *CaveDensity) entrancesAt(x, y, z, roughness float32) float32 {
	spaghetti3D := c.spaghetti3DAt(x, y, z)
	big := float32(c.caveEntrance.Sample(float32(x*0.75), float32(y*0.5), float32(z*0.75)) + 0.37)
	big = float32(big + caveYGradient(int32(y), -10, 30, 0.3, 0))
	return caveMin(big, float32(roughness+spaghetti3D))
}

func (c *CaveDensity) spaghetti2DAt(x, y, z float32) float32 {
	rarityInput := c.spaghetti2DModulator.Sample(float32(x*2), y, float32(z*2))
	var rarity float32
	switch {
	case rarityInput < -0.75:
		rarity = 0.5
	case rarityInput < -0.5:
		rarity = 0.75
	case rarityInput < 0.5:
		rarity = 1
	case rarityInput < 0.75:
		rarity = 2
	default:
		rarity = 3
	}
	invRarity := float32(1 / rarity)
	spaghetti := caveAbs(float32(c.spaghetti2D.Sample(float32(x*invRarity), float32(y*invRarity), float32(z*invRarity)) * rarity))
	thickness := caveMappedNoise(c.spaghetti2DThickness.Sample(float32(x*2), y, float32(z*2)), -0.6, -1.3)
	elevation := caveMappedNoise(c.spaghetti2DElevation.Sample(x, 0, z), -8, 8)
	sloped := caveAbs(float32(elevation + float32(y*(-0.125))))
	layerRidged := float32(sloped + thickness)
	layerRidged = float32(layerRidged * layerRidged * layerRidged)
	// Bedrock's coefficient is one float32 ULP below 0.083.
	cave := float32(spaghetti + float32(thickness*0x1.53f7cep-4))
	return caveClamp(caveMax(cave, layerRidged), -1, 1)
}

// Sample evaluates cave density at an integer block position. slopedCheese is
// the upstream native (128x-MCP) terrain density. The surface/underground
// selector is at 200 native units (1.5625 MCP); the returned value excludes
// the global terrain slides and post-processing.
func (c *CaveDensity) Sample(x, y, z int32, slopedCheese float32) float32 {
	fx, fy, fz := float32(x), float32(y), float32(z)
	if slopedCheese < -150 {
		return slopedCheese
	}
	roughness := c.spaghettiRoughnessAt(fx, fy, fz)
	entrances := c.entrancesAt(fx, fy, fz, roughness)
	if slopedCheese < 200 {
		return caveMin(slopedCheese, float32(entrances*5*caveDensityScale))
	}

	layerNoise := c.caveLayer.Sample(fx, float32(fy*8), fz)
	layerized := float32(layerNoise * layerNoise * 4)
	cheese := caveClamp(float32(c.caveCheese.Sample(fx, float32(fy*0.6666667), fz)+0.27), -1, 1)
	top := float32(0)
	transition := float32(float32(slopedCheese-200) * 1.28)
	// Preserve the native unordered branch: NaN reaches the arithmetic.
	if !(transition > 1) {
		top = float32(0.5 - float32(transition*0.5))
	}
	base := float32(float32(cheese+top) + layerized)
	underground := caveMin(base, entrances)
	underground = caveMin(underground, float32(c.spaghetti2DAt(fx, fy, fz)+roughness))

	pillarNoise := c.pillar.Sample(float32(fx*25), float32(fy*0.3), float32(fz*25))
	pillarRarity := caveMappedNoise(c.pillarRareness.Sample(fx, fy, fz), 0, -2)
	pillarThickness := caveMappedNoise(c.pillarThickness.Sample(fx, fy, fz), 0, 1.1)
	// Round the cube before multiplying by pillar density.
	thickness64 := float64(pillarThickness)
	thicknessCube := float32(thickness64 * thickness64 * thickness64)
	pillar := float32(float32(float32(pillarNoise*2)+pillarRarity) * thicknessCube)
	if !(pillar > 0.03) {
		pillar = -math.MaxFloat32
	}
	return float32(caveMax(underground, pillar) * caveDensityScale)
}
