package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
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
		// The client of the player views the crafter it opens, as the
		// session does.
		p.OpenBlockContainer(signPos, tx)
		if m := p.Menu(); m == nil || m.Kind() != menu.Crafter {
			t.Fatalf("the player has %v open, want the crafter", m)
		}

		if err := p.SetCrafterSlotDisabled(signPos, 3, true); err != nil {
			t.Fatalf("SetCrafterSlotDisabled: %v", err)
		}
		if got := tx.Block(signPos).(block.Crafter); got.DisabledSlots != 1<<3 {
			t.Fatalf("the crafter has slots %09b disabled, want slot 3", got.DisabledSlots)
		}
		if len(viewer.states) != 1 || viewer.states[0] != [2]any{3, true} || len(c.states) != 1 || c.states[0] != [2]any{3, true} {
			t.Fatalf("the viewer was shown %v and the client %v, want slot 3 disabled to both", viewer.states, c.states)
		}

		// A toggle that changes nothing shows the client the slot again.
		if err := p.SetCrafterSlotDisabled(signPos, 3, true); err != nil {
			t.Fatalf("SetCrafterSlotDisabled: %v", err)
		}
		if len(c.states) != 2 || c.states[1] != [2]any{3, true} || len(viewer.states) != 1 {
			t.Fatalf("the client was shown %v and the viewer %v, want slot 3 disabled to the client again", c.states, viewer.states)
		}
	})
}

func TestPlayerSetCrafterSlotDisabledRefuses(t *testing.T) {
	const (
		noMenu  = "no matching Crafter container opened"
		badSlot = "invalid or unreachable Crafter slot request"
		noBlock = "Crafter block no longer exists"
	)
	openCrafter := func(pos cube.Pos) func(tx *world.Tx, p *Player) {
		return func(tx *world.Tx, p *Player) { p.OpenBlockContainer(pos, tx) }
	}
	for _, c := range []struct {
		name  string
		open  func(tx *world.Tx, p *Player)
		pos   cube.Pos
		slot  int
		error string
	}{
		{"no menu", func(*world.Tx, *Player) {}, signPos, 0, noMenu},
		{"crafter elsewhere", openCrafter(farPos), signPos, 0, noMenu},
		{"menu of another kind", func(tx *world.Tx, p *Player) {
			// The chest menu stays open at the position of the crafter.
			tx.SetBlock(signPos, block.NewChest(), nil)
			p.OpenBlockContainer(signPos, tx)
			tx.SetBlock(signPos, block.NewCrafter(), nil)
		}, signPos, 0, noMenu},
		{"menu closed", func(tx *world.Tx, p *Player) {
			p.OpenBlockContainer(signPos, tx)
			p.CloseMenu(true)
		}, signPos, 0, noMenu},
		{"slot 9", openCrafter(signPos), signPos, 9, badSlot},
		{"negative slot", openCrafter(signPos), signPos, -1, badSlot},
		{"out of reach", openCrafter(farPos), farPos, 0, badSlot},
		{"no crafter", func(tx *world.Tx, p *Player) {
			p.OpenBlockContainer(signPos, tx)
			tx.SetBlock(signPos, nil, nil)
		}, signPos, 0, noBlock},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, _ := clientTestWorld(t)
			view := newCrafterView()
			withClientPlayer(t, w, view, func(tx *world.Tx, p *Player) {
				tx.SetBlock(signPos, block.NewCrafter(), nil)
				tx.SetBlock(farPos, block.NewCrafter(), nil)
				c.open(tx, p)
				err := p.SetCrafterSlotDisabled(c.pos, c.slot, true)
				if err == nil || err.Error() != c.error {
					t.Fatalf("SetCrafterSlotDisabled = %v, want %q", err, c.error)
				}
				for _, pos := range []cube.Pos{signPos, farPos} {
					if got, ok := tx.Block(pos).(block.Crafter); ok && got.DisabledSlots != 0 {
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
