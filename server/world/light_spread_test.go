package world_test

import (
	"testing"
	"time"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// TestBatchedLightSpreadMatchesSequential loads generated terrain into a
// world, which spreads light in batches of classes run on all cores, and
// checks every sky and block light level against copies of the same chunks
// filled and spread one area after another until nothing changes any more.
func TestBatchedLightSpreadMatchesSequential(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	w := world.Config{Generator: lightTestGenerator{}, GeneratorWorkers: 8, RandomTickSpeed: -1, Provider: world.NopProvider{}}.New()
	defer w.Close()
	const radius = 4
	loader := world.NewLoader(radius, w, world.NopViewer{})
	<-w.Exec(func(tx *world.Tx) { loader.Move(tx, mgl64.Vec3{8, 80, 8}) })
	deadline := time.Now().Add(2 * time.Minute)
	var positions []world.ChunkPos
	for {
		positions = positions[:0]
		<-w.Exec(func(tx *world.Tx) {
			loader.Load(tx, 256)
			for x := int32(-radius); x <= radius; x++ {
				for z := int32(-radius); z <= radius; z++ {
					if tx.ChunkLoaded(world.ChunkPos{x, z}) {
						positions = append(positions, world.ChunkPos{x, z})
					}
				}
			}
		})
		if len(positions) >= 49 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(positions) < 49 {
		t.Fatalf("only %d chunks loaded", len(positions))
	}
	// Generation finishes in transactions of its own: wait until no more
	// chunks become ready, so that each ready chunk had its light queued.
	stable, last := 0, -1
	for stable < 10 && time.Now().Before(deadline) {
		count := 0
		<-w.Exec(func(tx *world.Tx) {
			for x := int32(-radius - 4); x <= radius+4; x++ {
				for z := int32(-radius - 4); z <= radius+4; z++ {
					if tx.ChunkLoaded(world.ChunkPos{x, z}) {
						count++
					}
				}
			}
		})
		if count == last {
			stable++
		} else {
			stable, last = 0, count
		}
		time.Sleep(50 * time.Millisecond)
	}
	live := map[world.ChunkPos]*chunk.Chunk{}
	reference := map[world.ChunkPos]*chunk.Chunk{}
	<-w.Exec(func(tx *world.Tx) {
		// Reading light brings it up to date first. The chunks generated
		// around the loaded ones take part as well, so all ready chunks are
		// copied in the same transaction.
		tx.Light(cube.Pos{8, 80, 8})
		positions = positions[:0]
		for x := int32(-radius - 4); x <= radius+4; x++ {
			for z := int32(-radius - 4); z <= radius+4; z++ {
				pos := world.ChunkPos{x, z}
				if !tx.ChunkLoaded(pos) {
					continue
				}
				c := tx.ReadyChunk(pos)
				live[pos] = c
				reference[pos] = c.Clone()
				positions = append(positions, pos)
			}
		}
	})
	for pos, c := range reference {
		chunk.LightArea([]*chunk.Chunk{c}, int(pos[0]), int(pos[1])).Fill()
	}
	area := func(pos world.ChunkPos) ([]*chunk.Chunk, bool) {
		var cs []*chunk.Chunk
		for z := int32(-1); z <= 1; z++ {
			for x := int32(-1); x <= 1; x++ {
				c, ok := reference[world.ChunkPos{pos[0] + x, pos[1] + z}]
				if !ok {
					return nil, false
				}
				cs = append(cs, c)
			}
		}
		return cs, true
	}
	sum := func() (s uint64) {
		for _, pos := range positions {
			c, ok := reference[pos]
			if !ok {
				continue
			}
			for y := c.Range().Min(); y <= c.Range().Max(); y += 3 {
				for x := uint8(0); x < 16; x++ {
					for z := uint8(0); z < 16; z++ {
						s = s*31 + uint64(c.SkyLight(x, int16(y), z))<<4 + uint64(c.Light(x, int16(y), z))
					}
				}
			}
		}
		return s
	}
	before := sum()
	for pass := 0; ; pass++ {
		for _, pos := range positions {
			if cs, ok := area(pos); ok {
				chunk.LightArea(cs, int(pos[0])-1, int(pos[1])-1).Spread()
			}
		}
		after := sum()
		if after == before {
			break
		}
		if pass > 8 {
			t.Fatal("sequential spreading did not settle")
		}
		before = after
	}
	mismatches := 0
	<-w.Exec(func(tx *world.Tx) {
		for pos, want := range reference {
			got := live[pos]
			for y := want.Range().Min(); y <= want.Range().Max(); y++ {
				for x := uint8(0); x < 16; x++ {
					for z := uint8(0); z < 16; z++ {
						if got.SkyLight(x, int16(y), z) != want.SkyLight(x, int16(y), z) || got.Light(x, int16(y), z) != want.Light(x, int16(y), z) {
							if mismatches++; mismatches <= 5 {
								t.Errorf("chunk %v at %v %v %v: sky %v block light %v, want %v and %v", pos, x, y, z,
									got.SkyLight(x, int16(y), z), got.Light(x, int16(y), z), want.SkyLight(x, int16(y), z), want.Light(x, int16(y), z))
							}
						}
					}
				}
			}
		}
	})
	if mismatches > 0 {
		t.Fatalf("%d light levels differ", mismatches)
	}
}

// lightTestGenerator generates hills of stone with caves, glowstone and lava
// lighting them across chunk borders, and nothing that changes blocks after
// generation, so the light of its chunks only comes from filling and
// spreading.
type lightTestGenerator struct{}

func (lightTestGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	stone, _ := chunk.StateToRuntimeID("minecraft:stone", nil)
	glow, _ := chunk.StateToRuntimeID("minecraft:glowstone", nil)
	lava, _ := chunk.StateToRuntimeID("minecraft:lava", map[string]any{"liquid_depth": int32(0)})
	minY := int(c.Range().Min())
	for x := range uint8(16) {
		for z := range uint8(16) {
			wx, wz := int(pos[0])*16+int(x), int(pos[1])*16+int(z)
			height := 64 + (wx*7+wz*13)%11 - 5
			for y := minY; y < height; y++ {
				h := uint32(wx*73856093 ^ y*19349663 ^ wz*83492791)
				switch {
				case y > 20 && y < 40 && (wx+wz+y)%9 < 3:
					// Caves.
				case h%97 == 0:
					c.SetBlock(x, int16(y), z, 0, glow)
				case h%131 == 0:
					c.SetBlock(x, int16(y), z, 0, lava)
				default:
					c.SetBlock(x, int16(y), z, 0, stone)
				}
			}
		}
	}
	return world.GenerationResult{}
}

func (lightTestGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{8, 80, 8} }
