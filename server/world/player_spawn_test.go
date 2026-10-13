package world_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestPlayerSpawnPositionRespectsRadiusAndAvoidsWater(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		// A stone floor at y=0 whose western half is covered by water.
		for x := -12; x <= 12; x++ {
			for z := -12; z <= 12; z++ {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
				if x < 0 {
					tx.SetBlock(cube.Pos{x, 1, z}, block.Water{Still: true, Depth: 8}, nil)
				}
			}
		}
		spawn := cube.Pos{0, 5, 0}
		for range 200 {
			pos := tx.PlayerSpawnPosition(spawn)
			if pos[1] != 1 {
				t.Fatalf("spawned at y=%v, want standing on the floor at 1", pos[1])
			}
			if math.Abs(pos[0]-0.5) > 10 || math.Abs(pos[2]-0.5) > 10 {
				t.Fatalf("spawn %v outside respawn_radius 10", pos)
			}
			if pos[0] < 0 {
				t.Fatalf("spawned in water at %v", pos)
			}
		}
		if err := tx.World().SetGameRule("respawn_radius", int64(0)); err != nil {
			t.Fatal(err)
		}
		if pos := tx.PlayerSpawnPosition(spawn); pos != (mgl64.Vec3{0.5, 1, 0.5}) {
			t.Fatalf("respawn_radius 0 spawned at %v, want the spawn column", pos)
		}
	})
}
