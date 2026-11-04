package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// crafterView is a client, or a viewer of a crafter, that records the
// states of the slots of crafters it is shown.
type crafterView struct {
	*clienttest.Recorder
	states [][2]any
}

func newCrafterView() *crafterView { return &crafterView{Recorder: clienttest.New()} }

func (v *crafterView) ViewSlotChange(int, item.Stack) {}
func (v *crafterView) ViewCrafterSlotState(slot int, disabled bool) {
	v.states = append(v.states, [2]any{slot, disabled})
}

func TestPlayerSetCrafterSlotDisabled(t *testing.T) {
	w, _ := clientTestWorld(t)
	c := newCrafterView()
	withClientPlayer(t, w, c, func(tx *world.Tx, p *Player) {
		crafter := block.NewCrafter()
		viewer := newCrafterView()
		crafter.AddViewer(viewer, tx, signPos)
		tx.SetBlock(signPos, crafter, nil)

		if err := p.SetCrafterSlotDisabled(signPos, 3, true); err != nil {
			t.Fatalf("SetCrafterSlotDisabled: %v", err)
		}
		if got := tx.Block(signPos).(block.Crafter); got.DisabledSlots != 1<<3 {
			t.Fatalf("the crafter has slots %09b disabled, want slot 3", got.DisabledSlots)
		}
		if len(viewer.states) != 1 || viewer.states[0] != [2]any{3, true} || len(c.states) != 0 {
			t.Fatalf("the viewer was shown %v and the client %v, want slot 3 disabled to the viewer", viewer.states, c.states)
		}

		// A toggle that changes nothing shows the client the slot again.
		if err := p.SetCrafterSlotDisabled(signPos, 3, true); err != nil {
			t.Fatalf("SetCrafterSlotDisabled: %v", err)
		}
		if len(c.states) != 1 || c.states[0] != [2]any{3, true} || len(viewer.states) != 1 {
			t.Fatalf("the client was shown %v and the viewer %v, want slot 3 disabled to the client", c.states, viewer.states)
		}
	})
}

func TestPlayerSetCrafterSlotDisabledRefuses(t *testing.T) {
	for _, c := range []struct {
		name string
		pos  cube.Pos
		slot int
	}{
		{"slot 9", signPos, 9},
		{"negative slot", signPos, -1},
		{"out of reach", farPos, 0},
		{"no crafter", signPos.Side(cube.FaceUp), 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, _ := clientTestWorld(t)
			view := newCrafterView()
			withClientPlayer(t, w, view, func(tx *world.Tx, p *Player) {
				tx.SetBlock(signPos, block.NewCrafter(), nil)
				tx.SetBlock(farPos, block.NewCrafter(), nil)
				if err := p.SetCrafterSlotDisabled(c.pos, c.slot, true); err == nil {
					t.Fatal("SetCrafterSlotDisabled succeeded")
				}
				for _, pos := range []cube.Pos{signPos, farPos} {
					if got := tx.Block(pos).(block.Crafter); got.DisabledSlots != 0 {
						t.Fatalf("a refused toggle disabled slots %09b", got.DisabledSlots)
					}
				}
				if len(view.states) != 0 {
					t.Fatalf("a refused toggle showed the client %v", view.states)
				}
			})
		})
	}
}
