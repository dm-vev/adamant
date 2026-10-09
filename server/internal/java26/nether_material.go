package java26

// NetherMaterialNoise contains the seeded noises used by NetherMaterialRules
// and the positional random factories for its vanilla bedrock gradients.
type NetherMaterialNoise struct {
	gravelLayer, soulSandLayer, patch, surface *NormalNoise
	netherrack, netherWart, selector           *NormalNoise
	bedrockFloor, bedrockRoof, surfaceRandom   PositionalFactory
}

func NewNetherMaterialNoise(seed int64) *NetherMaterialNoise {
	root := NewLegacyRandom(seed)
	factory := root.ForkPositional()
	noise := func(name string, octave int, amplitudes ...float64) *NormalNoise {
		random := factory.FromHashOf("minecraft:" + name)
		return NewNormalNoise(&random, octave, amplitudes...)
	}
	floorRandom := factory.FromHashOf("minecraft:bedrock_floor")
	roofRandom := factory.FromHashOf("minecraft:bedrock_roof")
	surfaceRandom := factory
	return &NetherMaterialNoise{
		gravelLayer:   noise("gravel_layer", -8, 1, 1, 1, 1, 0, 0, 0, 0, 0.013333333333333334),
		soulSandLayer: noise("soul_sand_layer", -8, 1, 1, 1, 1, 0, 0, 0, 0, 0.013333333333333334),
		patch:         noise("patch", -5, 1, 0, 0, 0, 0, 0.013333333333333334),
		surface:       noise("surface", -6, 1, 1, 1),
		netherrack:    noise("netherrack", -3, 1, 0, 0, 0.35),
		netherWart:    noise("nether_wart", -3, 1, 0, 0, 0.9),
		selector:      noise("nether_state_selector", -4, 1),
		bedrockFloor:  floorRandom.ForkPositional(),
		bedrockRoof:   roofRandom.ForkPositional(),
		surfaceRandom: surfaceRandom,
	}
}

func (n *NetherMaterialNoise) GravelLayer(x, z int32) float32 {
	return n.gravelLayer.Sample(float64(x), 0, float64(z))
}

func (n *NetherMaterialNoise) SoulSandLayer(x, z int32) float32 {
	return n.soulSandLayer.Sample(float64(x), 0, float64(z))
}

func (n *NetherMaterialNoise) Patch(x, z int32) float32 {
	return n.patch.Sample(float64(x), 0, float64(z))
}

func (n *NetherMaterialNoise) Netherrack(x, z int32) float32 {
	return n.netherrack.Sample(float64(x), 0, float64(z))
}

func (n *NetherMaterialNoise) NetherWart(x, z int32) float32 {
	return n.netherWart.Sample(float64(x), 0, float64(z))
}

func (n *NetherMaterialNoise) Selector(x, z int32) float32 {
	return n.selector.Sample(float64(x), 0, float64(z))
}

// SurfaceDepth reproduces MaterialSystem.getSurfaceDepth: this varying depth
// controls the Nether's under-floor/under-ceiling material conditions.
func (n *NetherMaterialNoise) SurfaceDepth(x, z int32) int {
	noiseValue := float64(n.surface.Sample(float64(x), 0, float64(z)))
	random := n.surfaceRandom.At(x, 0, z)
	return int(noiseValue*2.75 + 3.0 + random.NextDouble()*0.25)
}

// BedrockFloor matches the verticalGradient condition in
// VanillaMaterialRules.bedrock_floor, including its coordinate-seeded roll.
func (n *NetherMaterialNoise) BedrockFloor(x, y, z int32) bool {
	if y <= 0 {
		return true
	}
	if y >= 5 {
		return false
	}
	probability := 1 - float64(y)/5
	random := n.bedrockFloor.At(x, y, z)
	return float64(random.NextFloat()) < probability
}

// BedrockRoof is the negated verticalGradient condition in
// VanillaMaterialRules.bedrock_roof (top block=127 for Nether noise height 128).
func (n *NetherMaterialNoise) BedrockRoof(x, y, z int32) bool {
	if y <= 122 {
		return false
	}
	if y >= 127 {
		return true
	}
	probability := 1 - float64(y-122)/5
	random := n.bedrockRoof.At(x, y, z)
	return float64(random.NextFloat()) >= probability
}
