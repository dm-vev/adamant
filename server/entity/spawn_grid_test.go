package entity

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// gridPlayer is a player at a position, for the player grid of spawning.
type gridPlayer struct {
	world.Entity
	pos mgl64.Vec3
}

func (p gridPlayer) Position() mgl64.Vec3 { return p.pos }

// TestSpawnPlayerGridMatchesEveryPlayer checks the players near a chunk and
// the distance to the nearest player found through the grid of spawning
// are those found asking every player.
func TestSpawnPlayerGridMatchesEveryPlayer(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for layout := range 50 {
		var players []world.Entity
		for range 1 + layout%30 {
			players = append(players, gridPlayer{pos: mgl64.Vec3{r.Float64()*2000 - 1000, 64, r.Float64()*2000 - 1000}})
		}
		s := &spawnState{players: players, near: map[world.ChunkPos][]int{}, grid: map[[2]int32][]int{}}
		for i, p := range players {
			c := playerCellOf(p.Position()[0], p.Position()[2])
			s.grid[c] = append(s.grid[c], i)
		}
		for range 200 {
			pos := mgl64.Vec3{r.Float64()*2400 - 1200, 64, r.Float64()*2400 - 1200}
			want, wantOK := 0., false
			for _, p := range players {
				if d := p.Position().Sub(pos).LenSqr(); !wantOK || d < want {
					want, wantOK = d, true
				}
			}
			if got, ok := s.nearestPlayerDistSqr(pos); got != want || ok != wantOK {
				t.Fatalf("layout %d: nearest player at %v squared %v, want %v", layout, pos, got, want)
			}
			chunk := world.ChunkPos{int32(pos[0]) >> 4, int32(pos[2]) >> 4}
			x, z := float64(chunk[0])*16+8, float64(chunk[1])*16+8
			wantNear := 0
			for _, p := range players {
				dx, dz := x-p.Position()[0], z-p.Position()[2]
				if dx*dx+dz*dz < 16384 {
					wantNear++
				}
			}
			if got := len(s.playersNear(chunk)); got != wantNear {
				t.Fatalf("layout %d: %d players near chunk %v, want %d", layout, got, chunk, wantNear)
			}
		}
	}
}
