package bedrock

// TerrainMaterial identifies a solid block selected by the Overworld density
// fill. Dimension generators resolve these to their own block registry IDs.
type TerrainMaterial uint8

const (
	MaterialStone TerrainMaterial = iota + 1
	MaterialDeepslate
	MaterialCopperOre
	MaterialRawCopper
	MaterialGranite
	MaterialDeepslateIronOre
	MaterialRawIron
	MaterialTuff
)

// TerrainMaterials selects base stone and large ore veins after density and
// aquifer evaluation have classified a voxel as solid. It is immutable.
type TerrainMaterials struct {
	depth, ore RandomFactory
	gap        NormalNoise
}

func NewTerrainMaterials(seed int64) *TerrainMaterials {
	factory := NewRandomFactory(seed)
	depth := factory.fromName([]byte("minecraft:depth_based_layer"))
	ore := factory.fromName([]byte("minecraft:ore"))
	return &TerrainMaterials{depth: depth.fork(), ore: ore.fork(), gap: factory.Noise("minecraft:ore_gap", -5, 1)}
}

// Sample consumes interpolated vein selector and ridge noises. Ore positional
// hashing rounds X/Z through float32, unlike the base-stone positional hash.
func (m *TerrainMaterials) Sample(x, y, z int32, selector, ridgeA, ridgeB float32) TerrainMaterial {
	base := MaterialStone
	if y < 0 {
		base = MaterialDeepslate
	} else if y < 8 {
		r := m.depth.at(x, y, z)
		if float32(r.next()>>40)*0x1p-24 < float32(1-float32(y)*0.125) {
			base = MaterialDeepslate
		}
	}

	minY, maxY := int32(-60), int32(-8)
	ore, raw, filler := MaterialDeepslateIronOre, MaterialRawIron, MaterialTuff
	if selector > 0 {
		minY, maxY = 0, 50
		ore, raw, filler = MaterialCopperOre, MaterialRawCopper, MaterialGranite
	}
	if y < minY || y > maxY {
		return base
	}
	distance := float32(min(y-minY, maxY-y)) / 20
	penalty := float32(0)
	if distance <= 1 {
		penalty = float32(float32(distance*0.2) - 0.2)
	}
	strength := caveAbs(selector)
	if float32(strength+penalty) < 0.4 {
		return base
	}
	r := m.ore.at(floor32(float32(x)), y, floor32(float32(z)))
	if float32(r.next()>>40)*0x1p-24 > 0.7 {
		return base
	}
	if !(caveMax(float32(caveAbs(ridgeA)-0.08), float32(caveAbs(ridgeB)-0.08)) < 0) {
		return base
	}
	richnessPosition := float32(strength-0.4) / float32(0.20000001788139343)
	richness := float32(0.1)
	if !(richnessPosition < 0) {
		richness = 0.3
		if !(richnessPosition > 1) {
			richness = float32(float32(richnessPosition*0.20000001788139343) + 0.1)
		}
	}
	if !(float32(r.next()>>40)*0x1p-24 < richness) {
		return filler
	}
	if !(m.gap.Sample(float32(x), float32(y), float32(z)) > -0.3) {
		return filler
	}
	if float32(r.next()>>40)*0x1p-24 < 0.02 {
		return raw
	}
	return ore
}
