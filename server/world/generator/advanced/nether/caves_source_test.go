package nether

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestNetherCaveReplaceabilityAndLavaLevel(t *testing.T) {
	g := NewNether(0)
	target := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
	target.SetBlock(8, 64, 8, 0, g.basaltRID)
	target.SetBlock(9, 64, 8, 0, g.bedrockRID)
	target.SetBlock(8, 31, 8, 0, g.basaltRID)
	target.SetBlock(8, 32, 8, 0, g.basaltRID)
	var mask [netherCaveMaskWords]uint64
	g.caves.carveEllipsoid(0, 0, target, 8.5, 64.5, 8.5, 2, 2, -0.7, &mask)
	g.caves.carveEllipsoid(0, 0, target, 8.5, 31.5, 8.5, 2, 2, -0.7, &mask)
	if got := target.Block(8, 64, 8, 0); got != g.airRID {
		t.Fatalf("replaceable basalt carved to %d, want air runtime ID %d", got, g.airRID)
	}
	if got := target.Block(9, 64, 8, 0); got != g.bedrockRID {
		t.Fatalf("uncarvable bedrock changed to %d, want bedrock runtime ID %d", got, g.bedrockRID)
	}
	if got := target.Block(8, 31, 8, 0); got != g.lavaStillRID {
		t.Fatalf("replaceable basalt below sea level carved to %d, want still lava runtime ID %d", got, g.lavaStillRID)
	}
	if got := target.Block(8, 32, 8, 0); got != g.airRID {
		t.Fatalf("replaceable basalt at sea level carved to %d, want air runtime ID %d", got, g.airRID)
	}
}

// Digests were captured by running mapped Java 26.3's configured NETHER_CAVE
// through NoiseBasedChunkGenerator's source-chunk loop and CarvingMask, then
// applying Nether's disabled-aquifer lava fluid rule to these native-range
// 0..127 target grids.
func TestNetherCaveCarverRegression(t *testing.T) {
	cases := []struct {
		seed   int64
		chunkX int
		chunkZ int
		want   string
	}{
		{0, 0, 0, "dd39b5f5d6902eecb8d6200830290ac963cf15312cb5c0a03da79c66111eb541"},
		{-1, -37, 41, "ff2e8d0e012ba3b6a8cf5c8cafa5705086f398a9a95db0dc65d76bffc20c5e4e"},
		{8675309, 1024, -2049, "4efe5b252d80568850dc16763ac652651d824384c3e8148685506987bfb90a8d"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("seed=%d_chunk=%d,%d", tc.seed, tc.chunkX, tc.chunkZ), func(t *testing.T) {
			g := NewNether(tc.seed)
			target := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					for y := int16(0); y < 128; y++ {
						target.SetBlock(x, y, z, 0, g.netherrackRID)
					}
				}
			}
			g.carveCaves(tc.chunkX, tc.chunkZ, target)
			var materials [128 * 16 * 16]byte
			for y := 0; y < 128; y++ {
				for z := 0; z < 16; z++ {
					for x := 0; x < 16; x++ {
						rid := target.Block(uint8(x), int16(y), uint8(z), 0)
						switch rid {
						case g.airRID:
							materials[(y*16+z)*16+x] = 'a'
						case g.lavaStillRID:
							materials[(y*16+z)*16+x] = 'l'
						case g.netherrackRID:
							materials[(y*16+z)*16+x] = 'n'
						default:
							materials[(y*16+z)*16+x] = 'o'
						}
					}
				}
			}
			got := fmt.Sprintf("%x", sha256.Sum256(materials[:]))
			if got != tc.want {
				t.Fatalf("material-position digest %s, want %s", got, tc.want)
			}
		})
	}
}
