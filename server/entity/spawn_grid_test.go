package entity

import (
	"math/rand/v2"
	"testing"

	"github.com/dm-vev/adamant/server/world"
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
	t.Parallel()
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

func TestNearestPlayerDistSqrEdgeCases(t *testing.T) {
	t.Parallel()
	brute := func(players []world.Entity, pos mgl64.Vec3) (float64, bool) {
		best, ok := 0., false
		for _, p := range players {
			if d := p.Position().Sub(pos).LenSqr(); !ok || d < best {
				best, ok = d, true
			}
		}
		return best, ok
	}
	build := func(players []world.Entity) *spawnState {
		s := &spawnState{players: players, near: map[world.ChunkPos][]int{}, grid: map[[2]int32][]int{}}
		for i, p := range players {
			c := playerCellOf(p.Position()[0], p.Position()[2])
			s.grid[c] = append(s.grid[c], i)
		}
		return s
	}

	// Empty players.
	if d, ok := build(nil).nearestPlayerDistSqr(mgl64.Vec3{0, 64, 0}); ok || d != 0 {
		t.Fatalf("empty players: got (%v, %v), want (0, false)", d, ok)
	}

	// Single distant player, extreme Y delta, and exact cell edges.
	edgePlayers := []world.Entity{
		gridPlayer{pos: mgl64.Vec3{-128.0001, 310, 0}},
		gridPlayer{pos: mgl64.Vec3{128, -60, -128}},
		gridPlayer{pos: mgl64.Vec3{5000, 64, -5000}},
	}
	sEdge := build(edgePlayers)
	for _, pos := range []mgl64.Vec3{
		{0, 64, 0},
		{-128, -60, 0},
		{127.9999, 310, -128.0001},
		{10000, 64, 10000},
		{-128, 310, 0},
	} {
		want, wantOK := brute(edgePlayers, pos)
		if got, ok := sEdge.nearestPlayerDistSqr(pos); got != want || ok != wantOK {
			t.Fatalf("edge pos %v: got (%v, %v), want (%v, %v)", pos, got, ok, want, wantOK)
		}
	}

	// Large sparse world (60 players over 8000x8000 with wide Y spread) testing rings 0, 1, 2, and fallback.
	r := rand.New(rand.NewPCG(99, 101))
	sparse := make([]world.Entity, 60)
	for i := range sparse {
		sparse[i] = gridPlayer{pos: mgl64.Vec3{r.Float64()*8000 - 4000, r.Float64()*360 - 60, r.Float64()*8000 - 4000}}
	}
	sSparse := build(sparse)
	for range 500 {
		pos := mgl64.Vec3{r.Float64()*9000 - 4500, r.Float64()*360 - 60, r.Float64()*9000 - 4500}
		want, wantOK := brute(sparse, pos)
		if got, ok := sSparse.nearestPlayerDistSqr(pos); got != want || ok != wantOK {
			t.Fatalf("sparse pos %v: got (%v, %v), want (%v, %v)", pos, got, ok, want, wantOK)
		}
	}

	// Dense 100-player town (4 populated cells) testing direct populated-cell pruning.
	town := make([]world.Entity, 100)
	for i := range town {
		town[i] = gridPlayer{pos: mgl64.Vec3{r.Float64()*100 - 50, 64 + r.Float64()*20, r.Float64()*100 - 50}}
	}
	sTown := build(town)
	for range 200 {
		pos := mgl64.Vec3{r.Float64()*400 - 200, r.Float64()*200 - 40, r.Float64()*400 - 200}
		want, wantOK := brute(town, pos)
		if got, ok := sTown.nearestPlayerDistSqr(pos); got != want || ok != wantOK {
			t.Fatalf("town pos %v: got (%v, %v), want (%v, %v)", pos, got, ok, want, wantOK)
		}
	}
}

func BenchmarkNearestPlayerDistSqrSpread50(b *testing.B) {
	r := rand.New(rand.NewPCG(7, 11))
	players := make([]world.Entity, 50)
	for i := range players {
		// 50 players spread across generated terrain (4000x4000).
		players[i] = gridPlayer{pos: mgl64.Vec3{r.Float64()*4000 - 2000, 64 + r.Float64()*40, r.Float64()*4000 - 2000}}
	}
	s := &spawnState{players: players, near: map[world.ChunkPos][]int{}, grid: map[[2]int32][]int{}}
	for i, p := range players {
		c := playerCellOf(p.Position()[0], p.Position()[2])
		s.grid[c] = append(s.grid[c], i)
	}
	// Candidate spawn positions around players, including deep caves / high Y and ~100-140 block rings
	// where 3D distance >= 128 frequently triggers the old full-player scan fallback.
	queries := make([]mgl64.Vec3, 256)
	for i := range queries {
		p := players[i%len(players)].Position()
		dx := r.Float64()*280 - 140
		dz := r.Float64()*280 - 140
		dy := r.Float64()*140 - 100
		queries[i] = mgl64.Vec3{p[0] + dx, p[1] + dy, p[2] + dz}
	}
	b.ReportAllocs()
	b.ResetTimer()
	var sink float64
	for i := range b.N {
		d, _ := s.nearestPlayerDistSqr(queries[i&255])
		sink += d
	}
	if sink == 0 {
		b.Fatal("unexpected zero sum")
	}
}

func BenchmarkNearestPlayerDistSqrTown100(b *testing.B) {
	r := rand.New(rand.NewPCG(13, 17))
	players := make([]world.Entity, 100)
	for i := range players {
		players[i] = gridPlayer{pos: mgl64.Vec3{r.Float64()*100 - 50, 64, r.Float64()*100 - 50}}
	}
	s := &spawnState{players: players, near: map[world.ChunkPos][]int{}, grid: map[[2]int32][]int{}}
	for i, p := range players {
		c := playerCellOf(p.Position()[0], p.Position()[2])
		s.grid[c] = append(s.grid[c], i)
	}
	queries := make([]mgl64.Vec3, 256)
	for i := range queries {
		queries[i] = mgl64.Vec3{r.Float64()*260 - 130, r.Float64()*160 - 60, r.Float64()*260 - 130}
	}
	b.ReportAllocs()
	b.ResetTimer()
	var sink float64
	for i := range b.N {
		d, _ := s.nearestPlayerDistSqr(queries[i&255])
		sink += d
	}
	if sink == 0 {
		b.Fatal("unexpected zero sum")
	}
}
