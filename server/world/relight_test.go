package world_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// checkFreshLight checks the light around pos is the light computed from
// scratch for the blocks as they are.
func checkFreshLight(t *testing.T, tx *world.Tx, step string, pos cube.Pos) {
	t.Helper()
	fresh := tx.FreshLight(pos)
	for x := pos[0] - 17; x <= pos[0]+17; x++ {
		for z := pos[2] - 17; z <= pos[2]+17; z++ {
			for y := pos[1] - 17; y <= pos[1]+17; y++ {
				p := cube.Pos{x, y, z}
				sky, bl := fresh(p)
				if got := tx.SkyLight(p); got != sky {
					t.Fatalf("%s: skylight at %v = %d, want %d as computed from scratch", step, p, got, sky)
				}
				if got := tx.BlockLight(p); got != bl {
					t.Fatalf("%s: block light at %v = %d, want %d as computed from scratch", step, p, got, bl)
				}
			}
		}
	}
}

// TestLightFollowsBlockChanges checks light is recomputed after blocks are
// placed, broken and flooded, and matches the light computed from scratch.
func TestLightFollowsBlockChanges(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		// Load and light the chunks around the room first.
		for x := -48; x <= 48; x += 16 {
			for z := -48; z <= 48; z += 16 {
				tx.Block(cube.Pos{x, 64, z})
			}
		}
		room := cube.Pos{14, 64, 3}
		for x := -4; x <= 4; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(room.Add(cube.Pos{x, -1, z}), block.Stone{}, nil)
				tx.SetBlock(room.Add(cube.Pos{x, 4, z}), block.Stone{}, nil)
				for y := 0; y < 4; y++ {
					if x == -4 || x == 4 || z == -4 || z == 4 {
						tx.SetBlock(room.Add(cube.Pos{x, y, z}), block.Stone{}, nil)
					}
				}
			}
		}
		checkFreshLight(t, tx, "closing the room", room)
		if got := tx.SkyLight(room); got != 0 {
			t.Fatalf("skylight inside a closed room = %d, want 0", got)
		}

		torch := room.Add(cube.Pos{3, 0, 0})
		tx.SetBlock(torch, block.Glowstone{}, nil)
		checkFreshLight(t, tx, "placing glowstone", torch)
		if got := tx.BlockLight(room); got != 12 {
			t.Fatalf("block light 3 blocks from glowstone = %d, want 12", got)
		}

		tx.SetBlock(room.Add(cube.Pos{4, 1, 0}), nil, nil)
		checkFreshLight(t, tx, "opening the wall across the chunk border", torch)

		tx.SetBlock(torch, nil, nil)
		checkFreshLight(t, tx, "breaking the glowstone", torch)
		if got := tx.BlockLight(room); got != 0 {
			t.Fatalf("block light after breaking the glowstone = %d, want 0", got)
		}

		tx.SetBlock(room.Add(cube.Pos{0, 4, 0}), nil, nil)
		checkFreshLight(t, tx, "opening the roof", room)
		if got := tx.SkyLight(room); got != 15 {
			t.Fatalf("skylight under an opening in the roof = %d, want 15", got)
		}

		tx.SetBlock(room.Add(cube.Pos{0, 10, 0}), block.Stone{}, nil)
		checkFreshLight(t, tx, "covering the opening from high up", room)

		tx.SetLiquid(room.Add(cube.Pos{1, 0, 1}), block.Water{Depth: 8, Still: true})
		checkFreshLight(t, tx, "pouring water", room)

		tx.SetBlock(room.Add(cube.Pos{0, 10, 0}), nil, nil)
		tx.SetBlock(room.Add(cube.Pos{1, 4, 1}), block.Leaves{}, nil)
		checkFreshLight(t, tx, "uncovering it and placing leaves", room)
	})
}

// TestLightFollowsChangesAcrossManyChunks changes blocks in chunks spanning
// more than one area is relit in, some of them next to each other, and
// checks the light around each matches the light computed from scratch.
func TestLightFollowsChangesAcrossManyChunks(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -192; x <= 192; x += 16 {
			for z := -48; z <= 48; z += 16 {
				tx.Block(cube.Pos{x, 64, z})
			}
		}
		var changed []cube.Pos
		for i := range 24 {
			pos := cube.Pos{-170 + i*15, 66 + i%4, i%3*9 - 9}
			tx.SetBlock(pos, block.Glowstone{}, nil)
			tx.SetBlock(pos.Add(cube.Pos{1, 1, 0}), block.Stone{}, nil)
			tx.SetBlock(pos.Add(cube.Pos{0, 2, 0}), block.Stone{}, nil)
			changed = append(changed, pos)
		}
		for _, pos := range changed {
			checkFreshLight(t, tx, "placing glowstone and stone", pos)
		}
		for i := 0; i < len(changed); i += 2 {
			tx.SetBlock(changed[i], nil, nil)
		}
		for _, pos := range changed {
			checkFreshLight(t, tx, "breaking every other glowstone", pos)
		}
	})
}

// relightTestStructure is a 20x5x20 slab of glowstone with a hollow centre.
type relightTestStructure struct{}

func (relightTestStructure) Dimensions() [3]int { return [3]int{20, 5, 20} }
func (relightTestStructure) At(x, y, z int, _ func(x, y, z int) world.Block) (world.Block, world.Liquid) {
	if x > 5 && x < 14 && z > 5 && z < 14 {
		return block.Air{}, nil
	}
	return block.Glowstone{}, nil
}

// TestLightFollowsBuiltStructures checks building a structure across chunks
// recomputes their light.
func TestLightFollowsBuiltStructures(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -64; x <= 64; x += 16 {
			for z := -64; z <= 64; z += 16 {
				tx.Block(cube.Pos{x, 64, z})
			}
		}
		tx.BuildStructure(cube.Pos{-10, 60, -10}, relightTestStructure{})
		checkFreshLight(t, tx, "building glowstone", cube.Pos{0, 62, 0})
		if got := tx.BlockLight(cube.Pos{0, 62, 0}); got == 0 {
			t.Fatalf("block light inside a glowstone ring = 0, want it lit")
		}
	})
}

// TestLightFollowsJavaRules checks light follows Java's light engine: water
// and leaves dim skylight a level a block, a roof of bottom slabs shuts it
// out with the faces it occludes, and glass lets it through undimmed.
func TestLightFollowsJavaRules(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -48; x <= 48; x += 16 {
			for z := -48; z <= 48; z += 16 {
				tx.Block(cube.Pos{x, 64, z})
			}
		}
		for x := -6; x <= 6; x++ {
			for z := -6; z <= 6; z++ {
				tx.SetBlock(cube.Pos{x, 59, z}, block.Stone{}, nil)
			}
		}
		// A pool five blocks deep, walled in.
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				for y := 60; y <= 64; y++ {
					if x == -2 || x == 2 || z == -2 || z == 2 {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					} else {
						tx.SetLiquid(cube.Pos{x, y, z}, block.Water{Depth: 8, Still: true})
					}
				}
			}
		}
		checkFreshLight(t, tx, "filling the pool", cube.Pos{0, 62, 0})
		for y, want := range map[int]uint8{65: 15, 64: 14, 62: 12, 60: 10} {
			if got := tx.SkyLight(cube.Pos{0, y, 0}); got != want {
				t.Fatalf("skylight at depth %d of a pool = %d, want %d", 65-y, got, want)
			}
		}

		// A closed room roofed with bottom slabs.
		room := cube.Pos{20, 60, 20}
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(room.Add(cube.Pos{x, -1, z}), block.Stone{}, nil)
				tx.SetBlock(room.Add(cube.Pos{x, 3, z}), block.Slab{Block: block.Planks{Wood: block.OakWood()}}, nil)
				for y := 0; y < 3; y++ {
					if x == -2 || x == 2 || z == -2 || z == 2 {
						tx.SetBlock(room.Add(cube.Pos{x, y, z}), block.Stone{}, nil)
					}
				}
			}
		}
		checkFreshLight(t, tx, "roofing the room with slabs", room)
		if got := tx.SkyLight(room); got != 0 {
			t.Fatalf("skylight under a roof of bottom slabs = %d, want 0", got)
		}
		tx.SetBlock(room.Add(cube.Pos{0, 3, 0}), block.Glass{}, nil)
		checkFreshLight(t, tx, "glazing the roof", room)
		if got := tx.SkyLight(room); got != 15 {
			t.Fatalf("skylight under glass = %d, want 15", got)
		}
		tx.SetBlock(room.Add(cube.Pos{0, 3, 0}), block.Leaves{Persistent: true}, nil)
		checkFreshLight(t, tx, "covering it with leaves", room)
		// The leaves dim it a level, and every block below them one more.
		if got := tx.SkyLight(room); got != 11 {
			t.Fatalf("skylight three blocks under leaves = %d, want 11", got)
		}
	})
}

// TestPartialFacesOccludeTogether checks two partial faces block light where
// together they cover a face, as Shapes.faceShapeOccludes: the half faces of
// a bottom and a top slab side by side shut light out, while those of two
// bottom slabs leave the upper half open.
func TestPartialFacesOccludeTogether(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -48; x <= 48; x += 16 {
			for z := -48; z <= 48; z += 16 {
				tx.Block(cube.Pos{x, 64, z})
			}
		}
		for x := -2; x <= 3; x++ {
			for y := 63; y <= 65; y++ {
				for z := -1; z <= 1; z++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
				}
			}
		}
		slab := block.Slab{Block: block.Planks{Wood: block.OakWood()}}
		tx.SetBlock(cube.Pos{-1, 64, 0}, block.Glowstone{}, nil)
		tx.SetBlock(cube.Pos{0, 64, 0}, slab, nil)
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Slab{Block: slab.Block, Top: true}, nil)
		tx.SetBlock(cube.Pos{2, 64, 0}, nil, nil)
		checkFreshLight(t, tx, "a bottom and a top slab", cube.Pos{0, 64, 0})
		if got := tx.BlockLight(cube.Pos{0, 64, 0}); got != 14 {
			t.Fatalf("light in the bottom slab next to glowstone = %d, want 14", got)
		}
		if got := tx.BlockLight(cube.Pos{1, 64, 0}); got != 0 {
			t.Fatalf("light past a bottom and a top slab = %d, want 0", got)
		}
		tx.SetBlock(cube.Pos{1, 64, 0}, slab, nil)
		checkFreshLight(t, tx, "two bottom slabs", cube.Pos{0, 64, 0})
		if got := tx.BlockLight(cube.Pos{2, 64, 0}); got != 12 {
			t.Fatalf("light past two bottom slabs = %d, want 12", got)
		}
	})
}

// TestNetherAndEndHaveNoSkyLight checks DimensionType.hasSkyLight: the
// Nether and the End have no sky, so only blocks light them.
func TestNetherAndEndHaveNoSkyLight(t *testing.T) {
	for _, dim := range []world.Dimension{world.Nether, world.End} {
		w := world.Config{Dim: dim, Synchronous: true, RandomTickSpeed: -1}.New()
		<-w.Exec(func(tx *world.Tx) {
			pos := cube.Pos{0, 64, 0}
			if got := tx.SkyLight(pos); got != 0 {
				t.Errorf("%v: skylight in open air = %d, want 0", dim, got)
			}
			tx.SetBlock(pos.Side(cube.FaceEast), block.Glowstone{}, nil)
			if got := tx.Light(pos); got != 14 {
				t.Errorf("%v: light next to glowstone = %d, want 14", dim, got)
			}
			if got := tx.RawBrightness(pos.Add(cube.Pos{0, 0, 20})); got != 0 {
				t.Errorf("%v: brightness away from light = %d, want 0", dim, got)
			}
		})
		_ = w.Close()
	}
}
