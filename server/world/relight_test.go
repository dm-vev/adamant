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
