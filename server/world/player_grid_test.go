package world

import (
	"slices"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

// TestPlayersNearFindsPlayersWhereTheyAre checks Tx.PlayersNear finds every
// player within the distance asked, in the order Tx.Players finds them, and
// finds a player where it moved to once the world was told it moved.
func TestPlayersNearFindsPlayersWhereTheyAre(t *testing.T) {
	t.Parallel()
	w := Config{Synchronous: true}.New()
	defer w.Close()

	positions := []mgl64.Vec3{{0, 64, 0}, {300, 64, 0}, {-70, 64, 20}, {10, 64, -60}, {2000, 64, 2000}, {63, 64, 63}}
	handles := make([]*EntityHandle, len(positions))
	for i, pos := range positions {
		handles[i] = EntitySpawnOpts{Position: pos}.New(listedPlayerType{}, boundaryTickerConfig{ticks: new(int)})
	}
	near := func(tx *Tx, pos mgl64.Vec3, r float64) []*EntityHandle {
		var out []*EntityHandle
		for p := range tx.PlayersNear(pos, r) {
			out = append(out, p.H())
		}
		return out
	}
	within := func(pos mgl64.Vec3, r float64) []*EntityHandle {
		var out []*EntityHandle
		for _, h := range handles {
			if h.data.Pos.Sub(pos).Len() <= r {
				out = append(out, h)
			}
		}
		return out
	}
	check := func(tx *Tx, pos mgl64.Vec3, r float64) {
		t.Helper()
		got, want := near(tx, pos, r), within(pos, r)
		for _, h := range want {
			if !slices.Contains(got, h) {
				t.Fatalf("players within %v of %v: %v found, missing %v", r, pos, got, h.data.Pos)
			}
		}
		order := slices.Clone(got)
		slices.SortFunc(order, func(a, b *EntityHandle) int {
			return slices.Index(handles, a) - slices.Index(handles, b)
		})
		if !slices.Equal(got, order) {
			t.Fatalf("players near %v found out of order: %v", pos, got)
		}
	}
	<-w.Exec(func(tx *Tx) {
		for _, h := range handles {
			tx.AddEntity(h)
		}
		for _, pos := range []mgl64.Vec3{{0, 64, 0}, {64, 64, 64}, {-64, 64, 0}, {250, 64, 30}, {1990, 64, 1990}} {
			for _, r := range []float64{2, 32, 64, 100, 128, 700} {
				check(tx, pos, r)
			}
		}

		// A player teleported across the world is found where it went.
		handles[0].data.Pos = mgl64.Vec3{1000, 64, -1000}
		tx.PlayerMoved()
		if got := near(tx, mgl64.Vec3{0, 64, 0}, 32); slices.Contains(got, handles[0]) {
			t.Fatalf("moved player still found where it was: %v", got)
		}
		check(tx, mgl64.Vec3{1000, 64, -1000}, 16)

		// A player leaving is no longer found, and one coming is.
		ent, _ := handles[1].Entity(tx)
		tx.RemoveEntity(ent)
		if got := near(tx, mgl64.Vec3{300, 64, 0}, 32); len(got) != 0 {
			t.Fatalf("players found where the one that left was: %v", got)
		}
		tx.AddEntity(handles[1])
		if got := near(tx, mgl64.Vec3{300, 64, 0}, 32); !slices.Equal(got, []*EntityHandle{handles[1]}) {
			t.Fatalf("player that came back found as %v", got)
		}
	})
}

func TestPlayersNearEdgeCases(t *testing.T) {
	t.Parallel()
	w := Config{Synchronous: true}.New()
	defer w.Close()

	collect := func(tx *Tx, pos mgl64.Vec3, r float64) []*EntityHandle {
		var out []*EntityHandle
		for p := range tx.PlayersNear(pos, r) {
			out = append(out, p.H())
		}
		return out
	}

	<-w.Exec(func(tx *Tx) {
		// Empty world and negative radius.
		if got := collect(tx, mgl64.Vec3{0, 64, 0}, 32); len(got) != 0 {
			t.Fatalf("empty world returned %v", got)
		}
		if got := collect(tx, mgl64.Vec3{0, 64, 0}, -5); len(got) != 0 {
			t.Fatalf("negative radius returned %v", got)
		}

		// Exact distance boundary (r = 0, axis boundary, 3D Y boundary).
		exact := EntitySpawnOpts{Position: mgl64.Vec3{10, 64, 10}}.New(listedPlayerType{}, boundaryTickerConfig{ticks: new(int)})
		edgeX := EntitySpawnOpts{Position: mgl64.Vec3{26, 64, 10}}.New(listedPlayerType{}, boundaryTickerConfig{ticks: new(int)})
		highY := EntitySpawnOpts{Position: mgl64.Vec3{10, 96, 10}}.New(listedPlayerType{}, boundaryTickerConfig{ticks: new(int)})
		tx.AddEntity(exact)
		tx.AddEntity(edgeX)
		tx.AddEntity(highY)

		if got := collect(tx, mgl64.Vec3{10, 64, 10}, 0); !slices.Equal(got, []*EntityHandle{exact}) {
			t.Fatalf("r=0 got %v, want [%v]", got, exact)
		}
		if got := collect(tx, mgl64.Vec3{10, 64, 10}, 16); !slices.Equal(got, []*EntityHandle{exact, edgeX}) {
			t.Fatalf("r=16 with highY player got %v, want [exact, edgeX]", got)
		}
		if got := collect(tx, mgl64.Vec3{10, 64, 10}, 32); !slices.Equal(got, []*EntityHandle{exact, edgeX, highY}) {
			t.Fatalf("r=32 got %v, want all three in order", got)
		}

		// Re-entrancy: moving a player and triggering a grid rebuild inside yield must not corrupt iteration.
		var seen []*EntityHandle
		for p := range tx.PlayersNear(mgl64.Vec3{10, 64, 10}, 32) {
			seen = append(seen, p.H())
			if p.H() == exact {
				highY.data.Pos = mgl64.Vec3{500, 64, 500}
				tx.PlayerMoved()
				_ = collect(tx, mgl64.Vec3{500, 64, 500}, 16)
			}
		}
		if !slices.Equal(seen, []*EntityHandle{exact, edgeX, highY}) {
			t.Fatalf("re-entrant PlayersNear got %v", seen)
		}
	})

	// >128 players across multiple cells (spills stack buffer) must preserve exact Players() order.
	w2 := Config{Synchronous: true}.New()
	defer w2.Close()
	handles := make([]*EntityHandle, 150)
	for i := range handles {
		x := float64((i%15)*6) - 42
		z := float64((i/15)*6) - 27
		handles[i] = EntitySpawnOpts{Position: mgl64.Vec3{x, 64, z}}.New(listedPlayerType{}, boundaryTickerConfig{ticks: new(int)})
	}
	<-w2.Exec(func(tx *Tx) {
		for _, h := range handles {
			tx.AddEntity(h)
		}
		// r=128 hits the span > g.nCells branch and overflows the 128-element stack buffer.
		got := collect(tx, mgl64.Vec3{0, 64, 0}, 128)
		if !slices.Equal(got, handles) {
			t.Fatalf("150-player town query returned %d players or wrong order", len(got))
		}
	})
}

func benchmarkTownWorld(b *testing.B, count int) (*World, []*EntityHandle) {
	b.Helper()
	w := Config{Synchronous: true}.New()
	b.Cleanup(func() { _ = w.Close() })
	handles := make([]*EntityHandle, count)
	for i := range count {
		x := float64((i%10)*10) - 45
		z := float64((i/10)*10) - 45
		handles[i] = EntitySpawnOpts{Position: mgl64.Vec3{x, 64, z}}.New(listedPlayerType{}, boundaryTickerConfig{ticks: new(int)})
	}
	<-w.Exec(func(tx *Tx) {
		for _, h := range handles {
			tx.AddEntity(h)
		}
	})
	return w, handles
}

func BenchmarkPlayersNearTown100Radius16(b *testing.B) {
	w, _ := benchmarkTownWorld(b, 100)
	queryPos := mgl64.Vec3{0, 64, 0}
	<-w.Exec(func(tx *Tx) {
		// Warm the grid once.
		for range tx.PlayersNear(queryPos, 16) {
		}
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for range b.N {
			for range tx.PlayersNear(queryPos, 16) {
				n++
			}
		}
		if n == 0 {
			b.Fatal("expected nearby players")
		}
	})
}

func BenchmarkPlayersNearTown100Radius16Miss(b *testing.B) {
	w, _ := benchmarkTownWorld(b, 100)
	// Same 64x64 cell as town edge players (x in [0, 64)), but 17+ blocks away from all players.
	queryPos := mgl64.Vec3{62, 64, 62}
	<-w.Exec(func(tx *Tx) {
		for range tx.PlayersNear(queryPos, 16) {
		}
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for range b.N {
			for range tx.PlayersNear(queryPos, 16) {
				n++
			}
		}
		_ = n
	})
}

func BenchmarkPlayersNearTown100Radius128(b *testing.B) {
	w, _ := benchmarkTownWorld(b, 100)
	queryPos := mgl64.Vec3{0, 64, 0}
	<-w.Exec(func(tx *Tx) {
		for range tx.PlayersNear(queryPos, 128) {
		}
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for range b.N {
			for range tx.PlayersNear(queryPos, 128) {
				n++
			}
		}
		if n == 0 {
			b.Fatal("expected nearby players")
		}
	})
}

func BenchmarkPlayersByCellRebuild100(b *testing.B) {
	w, _ := benchmarkTownWorld(b, 100)
	<-w.Exec(func(tx *Tx) {
		w.playersByCell(tx)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			tx.PlayerMoved()
			g := w.playersByCell(tx)
			if g == nil {
				b.Fatal("expected playerGrid")
			}
		}
	})
}
