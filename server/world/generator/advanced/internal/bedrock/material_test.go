package bedrock

import (
	"math"
	"testing"
)

// Captured from the solid-material instructions of BDS 1.26.52.3's voxel
// loop, including its native ore-gap sampler. No Java material reference.
func TestTerrainMaterialsNative(t *testing.T) {
	for _, test := range []struct {
		seed           int64
		x, y, z        int32
		selector, a, b uint32
		want           TerrainMaterial
	}{
		{0x12345678abcdef01, 30000000, 5, 1, 0x3ecccccd, 0, 0, MaterialStone},
		{0x12345678abcdef01, math.MinInt32, -70, math.MinInt32, 0xbf19999a, 0xbda3d70a, 0xbda3d70a, MaterialDeepslate},
		{0x12345678abcdef01, 0, 15, 30000000, 0x3f19999a, 0xbda3d709, 0, MaterialCopperOre},
		{0x12345678abcdef01, 27556281, 31, 905444, 0x3f3cd35a, 0xbd92d773, 0x3d6f34d6, MaterialRawCopper},
		{0x12345678abcdef01, 1, 1, 0, 0x3f4ccccd, 0xbda3d709, 0, MaterialGranite},
		{0x12345678abcdef01, math.MinInt32, -34, -16777217, 0xbf19999a, 0xbda3d709, 0xbda3d709, MaterialDeepslateIronOre},
		{0x12345678abcdef01, -5652678, -28, -29361524, 0xbf142c3c, 0xbc816f00, 0x3ccb295e, MaterialRawIron},
		{0x12345678abcdef01, math.MaxInt32, -33, -16777217, 0xbecccccd, 0xbda3d709, 0xbda3d709, MaterialTuff},
		{1, -1, -1, 0, 0x3ecccccd, 0xbda3d709, 0, MaterialDeepslate},
		{1, 0, 0, 0, 0x3f19999a, 0xbda3d709, 0, MaterialDeepslate},
		{1, 24374971, 1, 10827687, 0x3e3d07c8, 0xbd4a57a7, 0xbe0d9e84, MaterialStone},
		{1, math.MinInt32, 1, -1, 0xbf19999a, 0xbda3d709, 0, MaterialDeepslate},
		{1, -1, 7, -1, 0xbf19999a, 0, 0, MaterialStone},
		{1, 1, 7, -30000000, 0, 0xbda3d70a, 0, MaterialDeepslate},
		{1, -1, 8, 1, 0xbf19999a, 0x3da3d709, 0, MaterialStone},
		{0, 20665522, -32, 27130276, 0x7fc00000, 0x7fc00000, 0, MaterialTuff},
		{0, -14063717, -32, 25741373, 0xbf19999a, 0x7fc00000, 0, MaterialDeepslateIronOre},
	} {
		m := NewTerrainMaterials(test.seed)
		got := m.Sample(test.x, test.y, test.z, math.Float32frombits(test.selector), math.Float32frombits(test.a), math.Float32frombits(test.b))
		if got != test.want {
			t.Errorf("seed=%d xyz=%d,%d,%d: material=%d, want %d", test.seed, test.x, test.y, test.z, got, test.want)
		}
	}
}
