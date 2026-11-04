package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestPlayerEditStructureBlock(t *testing.T) {
	edit := block.StructureBlockEdit{Mode: block.StructureBlockSave, Name: "edits:box", Size: [3]int{2, 500, 2}, Offset: [3]int{1, 0, 0}, Integrity: 100}
	for _, trigger := range []bool{false, true} {
		w, _ := clientTestWorld(t)
		withOperator(t, w, world.GameModeCreative, true, func(tx *world.Tx, p *Player) {
			tx.SetBlock(signPos, block.StructureBlock{}, nil)
			if err := p.EditStructureBlock(signPos, edit, trigger); err != nil {
				t.Fatalf("EditStructureBlock: %v", err)
			}
			b := tx.Block(signPos).(block.StructureBlock)
			if b.Name != "edits:box" || b.Size != [3]int{2, 384, 2} || b.Offset != (cube.Pos{1, 0, 0}) {
				t.Fatalf("the structure block is %#v", b)
			}
			_, saved, err := tx.World().StructureTemplate("edits:box")
			if err != nil || saved != trigger {
				t.Fatalf("trigger %v: the structure was saved: %v (%v)", trigger, saved, err)
			}
		})
	}
}

func TestPlayerEditStructureBlockRefuses(t *testing.T) {
	cases := []struct {
		name string
		pos  cube.Pos
		mode world.GameMode
		op   bool
		edit block.StructureBlockEdit
	}{
		{"not an operator", signPos, world.GameModeCreative, false, block.StructureBlockEdit{Name: "x"}},
		{"survival", signPos, world.GameModeSurvival, true, block.StructureBlockEdit{Name: "x"}},
		{"out of reach", farPos, world.GameModeCreative, true, block.StructureBlockEdit{Name: "x"}},
		{"mode", signPos, world.GameModeCreative, true, block.StructureBlockEdit{Mode: block.StructureBlockExport + 1, Name: "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, _ := clientTestWorld(t)
			withOperator(t, w, c.mode, c.op, func(tx *world.Tx, p *Player) {
				tx.SetBlock(c.pos, block.StructureBlock{}, nil)
				if err := p.EditStructureBlock(c.pos, c.edit, true); err == nil {
					t.Fatal("EditStructureBlock succeeded")
				}
				if got := tx.Block(c.pos).(block.StructureBlock); got.Name != "" {
					t.Fatalf("a refused edit made the block %#v", got)
				}
			})
		})
	}
	t.Run("no structure block", func(t *testing.T) {
		w, _ := clientTestWorld(t)
		withOperator(t, w, world.GameModeCreative, true, func(tx *world.Tx, p *Player) {
			if err := p.EditStructureBlock(signPos, block.StructureBlockEdit{Name: "x"}, true); err != nil {
				t.Fatalf("EditStructureBlock: %v, want nil", err)
			}
			if _, ok := tx.Block(signPos).(block.Air); !ok {
				t.Fatalf("an edit of air placed %#v", tx.Block(signPos))
			}
		})
	})
}
