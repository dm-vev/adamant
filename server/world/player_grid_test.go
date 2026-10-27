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
