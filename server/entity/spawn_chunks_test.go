package entity

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSpawningChunksMatchEveryChunk checks the chunks around players counted
// for the mob caps, and the chunks found to spawn mobs in with the players
// near each, are those found asking of every chunk around the players.
func TestSpawningChunksMatchEveryChunk(t *testing.T) {
	w := world.Config{Synchronous: true, Provider: world.NopProvider{}, Generator: world.NopGenerator{}, RandomTickSpeed: -1}.New()
	defer w.Close()
	r := rand.New(rand.NewPCG(5, 6))
	<-w.Exec(func(tx *world.Tx) {
		for layout := range 40 {
			var players []world.Entity
			spread := 50 + r.Float64()*800
			for range 1 + layout%25 {
				players = append(players, gridPlayer{pos: mgl64.Vec3{r.Float64()*spread - spread/2, 64, r.Float64()*spread - spread/2}})
			}
			// Some of the chunks around the players are loaded.
			for range 300 {
				p := players[r.IntN(len(players))].Position()
				tx.Block(cube.Pos{int(p[0]) + r.IntN(300) - 150, 0, int(p[2]) + r.IntN(300) - 150})
			}

			around := map[world.ChunkPos]bool{}
			for _, p := range players {
				c := chunkOf(p)
				for dx := int32(-8); dx <= 8; dx++ {
					for dz := int32(-8); dz <= 8; dz++ {
						around[world.ChunkPos{c[0] + dx, c[1] + dz}] = true
					}
				}
			}
			runs := chunkRunsAround(players, 8)
			count := 0
			for i, run := range runs {
				count += int(run.z1-run.z0) + 1
				if i > 0 && compareChunks(world.ChunkPos{runs[i-1].x, runs[i-1].z1}, world.ChunkPos{run.x, run.z0}) >= 0 {
					t.Fatalf("layout %d: run %v follows %v", layout, run, runs[i-1])
				}
			}
			if count != len(around) {
				t.Fatalf("layout %d: %d chunks around the players, want %d", layout, count, len(around))
			}

			s := &spawnState{tx: tx, players: players}
			got := s.spawningChunks(runs)
			var want []spawnChunk
			for pos := range around {
				var near []int
				x, z := float64(pos[0])*16+8, float64(pos[1])*16+8
				for i, p := range players {
					if dx, dz := x-p.Position()[0], z-p.Position()[2]; dx*dx+dz*dz < 128*128 {
						near = append(near, i)
					}
				}
				if len(near) > 0 && tx.EntityTicking(pos) {
					want = append(want, spawnChunk{pos: pos, near: near})
				}
			}
			slices.SortFunc(want, func(a, b spawnChunk) int { return compareChunks(a.pos, b.pos) })
			if len(got) != len(want) {
				t.Fatalf("layout %d: %d chunks to spawn in, want %d", layout, len(got), len(want))
			}
			for i := range got {
				near := slices.Sorted(slices.Values(got[i].near))
				if got[i].pos != want[i].pos || !slices.Equal(near, want[i].near) {
					t.Fatalf("layout %d: chunk %v with players %v, want %v with %v", layout, got[i].pos, near, want[i].pos, want[i].near)
				}
			}
		}
	})
}
