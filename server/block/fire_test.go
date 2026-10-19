package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// A supported soul flame must settle instead of indefinitely enqueueing itself
// and starving unrelated block updates behind it.
func TestSoulFireDoesNotStarveSupportUpdates(t *testing.T) {
	for _, base := range []world.Block{SoulSand{}, SoulSoil{}} {
		name, _ := base.EncodeBlock()
		t.Run(name, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			soulPos, otherPos := cube.Pos{4, 64, 4}, cube.Pos{10, 64, 4}
			<-w.Exec(func(tx *world.Tx) {
				opts := &world.SetOpts{DisableBlockUpdates: true}
				for dx := range 2 {
					for dz := range 2 {
						pos := soulPos.Add(cube.Pos{dx, 0, dz})
						tx.SetBlock(pos.Side(cube.FaceDown), base, opts)
						tx.SetBlock(pos, Fire{Type: NormalFire()}, opts)
					}
				}
				tx.SetBlock(otherPos.Side(cube.FaceDown), Stone{}, opts)
				tx.SetBlock(otherPos, Fire{Type: NormalFire()}, opts)
				tx.DoBlockUpdatesAround(soulPos)
			})
			w.AdvanceTick()
			<-w.Exec(func(tx *world.Tx) {
				fire, ok := tx.Block(soulPos).(Fire)
				if !ok || fire.Type != SoulFire() {
					t.Fatalf("normal flame over %s became %#v, want soul fire", name, tx.Block(soulPos))
				}
				tx.SetBlock(otherPos.Side(cube.FaceDown), nil, nil)
			})
			w.AdvanceTick()
			<-w.Exec(func(tx *world.Tx) {
				if _, ok := tx.Block(otherPos).(Air); !ok {
					t.Errorf("unsupported flame was starved by soul fire: %#v", tx.Block(otherPos))
				}
				if fire, ok := tx.Block(soulPos).(Fire); !ok || fire.Type != SoulFire() {
					t.Errorf("supported soul flame did not survive: %#v", tx.Block(soulPos))
				}
				tx.SetBlock(soulPos.Side(cube.FaceDown), nil, nil)
			})
			w.AdvanceTick()
			<-w.Exec(func(tx *world.Tx) {
				if _, ok := tx.Block(soulPos).(Air); !ok {
					t.Errorf("soul flame survived removal of its support: %#v", tx.Block(soulPos))
				}
			})
		})
	}
}
