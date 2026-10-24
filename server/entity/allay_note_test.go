package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestAllaysDeliverToNoteBlocks checks Allay.VibrationUser and
// AllayAi.getItemDepositPosition: an allay that hears a note block likes it
// and drops what it gathers on it while it keeps playing.
func TestAllaysDeliverToNoteBlocks(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	note := cube.Pos{6, 64, 0}
	var allay *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -12; x <= 12; x++ {
			for z := -12; z <= 12; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		tx.SetBlock(note, block.Note{}, nil)
		allay = tx.AddEntity(NewAllay(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}})).H()
		e, _ := allay.Entity(tx)
		b := e.(*Allay).b()
		b.held, b.inventory = item.NewStack(item.Diamond{}, 1), item.NewStack(item.Diamond{}, 2)
	})
	stepTicks(t, w, 2)
	heard := false
	for i := range 600 {
		if i%40 == 0 {
			<-w.Exec(func(tx *world.Tx) {
				tx.EmitGameEvent(world.GameEventNoteBlockPlay, note.Vec3Centre(), world.GameEventContext{})
			})
		}
		stepTicks(t, w, 1)
		dropped := false
		<-w.Exec(func(tx *world.Tx) {
			e, _ := allay.Entity(tx)
			a := e.(*Allay)
			heard = heard || a.b().likedPos != nil && a.b().likedPos.pos == note
			for it := range tx.Entities() {
				if ent, ok := it.(*Ent); ok && ent.H().Type() == ItemType && it.Position().Sub(note.Vec3Centre()).Len() < 3 {
					dropped = true
				}
			}
		})
		if dropped {
			if !heard {
				t.Fatal("allay dropped items at a note block it did not hear")
			}
			return
		}
	}
	t.Fatalf("allay did not drop its items at a note block it heard (heard %v)", heard)
}
