package player

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// commandMinecart stands for a command block minecart at pos, recording the
// edits it is given.
type commandMinecart struct {
	world.Entity
	pos   mgl64.Vec3
	edits [][]any
}

func (m *commandMinecart) Position() mgl64.Vec3 { return m.pos }
func (m *commandMinecart) UpdateCommandBlock(command, name, output string, track bool, delay uint32, first bool) {
	m.edits = append(m.edits, []any{command, name, output, track, delay, first})
}

// otherEntity is an entity at pos that is not a command block minecart.
type otherEntity struct {
	world.Entity
	pos mgl64.Vec3
}

func (e otherEntity) Position() mgl64.Vec3 { return e.pos }

func TestPlayerEditCommandBlock(t *testing.T) {
	edit := block.CommandBlockEdit{Mode: block.CommandBlockRepeat, Command: "say hi", Name: "name", TickDelay: 4}
	w, _ := clientTestWorld(t)
	withOperator(t, w, world.GameModeCreative, true, func(tx *world.Tx, p *Player) {
		tx.SetBlock(signPos, block.CommandBlock{Facing: cube.FaceUp, NeedsRedstone: true, LastOutput: "old"}, nil)
		if err := p.EditCommandBlock(signPos, edit); err != nil {
			t.Fatalf("EditCommandBlock: %v", err)
		}
		want := block.CommandBlock{Facing: cube.FaceUp, Mode: block.CommandBlockRepeat, Command: "say hi", CustomName: "name", TickDelay: 4}
		if got := tx.Block(signPos); got != world.Block(want) {
			t.Fatalf("the command block is %#v, want %#v", got, want)
		}
	})
}

func TestPlayerEditCommandBlockRefuses(t *testing.T) {
	long := strings.Repeat("a", block.MaxCommandBlockText+1)
	cases := []struct {
		name string
		pos  cube.Pos
		mode world.GameMode
		op   bool
		edit block.CommandBlockEdit
	}{
		{"mode", signPos, world.GameModeCreative, true, block.CommandBlockEdit{Mode: block.CommandBlockChain + 1}},
		{"not an operator", signPos, world.GameModeCreative, false, block.CommandBlockEdit{}},
		{"survival", signPos, world.GameModeSurvival, true, block.CommandBlockEdit{}},
		{"long command", signPos, world.GameModeCreative, true, block.CommandBlockEdit{Command: long}},
		{"long name", signPos, world.GameModeCreative, true, block.CommandBlockEdit{Name: long}},
		// Within the reach of a creative player, but its feet are over 8
		// blocks from the centre of the block.
		{"over 8 blocks", cube.Pos{0, 100, 9}, world.GameModeCreative, true, block.CommandBlockEdit{}},
	}
	placed := block.CommandBlock{Facing: cube.FaceUp, Command: "keep"}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, _ := clientTestWorld(t)
			withOperator(t, w, c.mode, c.op, func(tx *world.Tx, p *Player) {
				tx.SetBlock(c.pos, placed, nil)
				if err := p.EditCommandBlock(c.pos, c.edit); err == nil {
					t.Fatal("EditCommandBlock succeeded")
				}
				if got := tx.Block(c.pos); got != world.Block(placed) {
					t.Fatalf("a refused edit made the block %#v", got)
				}
			})
		})
	}
	t.Run("no command block", func(t *testing.T) {
		w, _ := clientTestWorld(t)
		withOperator(t, w, world.GameModeCreative, true, func(tx *world.Tx, p *Player) {
			if err := p.EditCommandBlock(signPos, block.CommandBlockEdit{}); err == nil {
				t.Fatal("EditCommandBlock of air succeeded")
			}
		})
	})
}

func TestPlayerEditCommandMinecart(t *testing.T) {
	edit := block.CommandBlockEdit{Mode: block.CommandBlockRepeat, Command: "say hi", Name: "name", LastOutput: "out", TrackOutput: true, TickDelay: 4, ExecuteOnFirstTick: true}
	w, _ := clientTestWorld(t)
	withOperator(t, w, world.GameModeCreative, true, func(tx *world.Tx, p *Player) {
		near := &commandMinecart{pos: mgl64.Vec3{0.5, 100, 8}}
		if err := p.EditCommandMinecart(near, edit); err != nil {
			t.Fatalf("EditCommandMinecart: %v", err)
		}
		if len(near.edits) != 1 || near.edits[0][0] != "say hi" || near.edits[0][2] != "out" || near.edits[0][4] != uint32(4) || near.edits[0][5] != true {
			t.Fatalf("the minecart got %v", near.edits)
		}

		far := &commandMinecart{pos: mgl64.Vec3{0.5, 100, 9}}
		if err := p.EditCommandMinecart(far, edit); err == nil || len(far.edits) != 0 {
			t.Fatal("the player edited a minecart over 8 blocks away")
		}
		if err := p.EditCommandMinecart(otherEntity{pos: near.pos}, edit); err == nil {
			t.Fatal("the player edited an entity that is not a command minecart")
		}
		if err := p.EditCommandMinecart(near, block.CommandBlockEdit{LastOutput: strings.Repeat("a", block.MaxCommandBlockText+1)}); err == nil || len(near.edits) != 1 {
			t.Fatal("the player set an output over the bound")
		}
	})
	w, _ = clientTestWorld(t)
	withOperator(t, w, world.GameModeCreative, false, func(tx *world.Tx, p *Player) {
		m := &commandMinecart{pos: mgl64.Vec3{0.5, 100, 2}}
		if err := p.EditCommandMinecart(m, edit); err == nil || len(m.edits) != 0 {
			t.Fatal("a player that is not an operator edited a command minecart")
		}
	})
}
