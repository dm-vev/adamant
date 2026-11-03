package player

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// signPos is a sign within reach of the player of withClientPlayer, and
// farPos one out of its reach.
var (
	signPos = cube.Pos{0, 100, 2}
	farPos  = cube.Pos{0, 100, 30}
)

func standingSign() block.Sign {
	return block.Sign{Wood: block.OakWood(), Attach: block.StandingAttachment(cube.OrientationFromYaw(0))}
}

func hangingSign() block.HangingSign {
	return block.HangingSign{Wood: block.OakWood(), Attach: block.WallHangingAttachment(cube.North)}
}

func TestPlayerCanReach(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		near, mid := cube.Pos{0, 100, 7}.Vec3Middle(), cube.Pos{0, 100, 12}.Vec3Middle()
		if !p.CanReach(near) || p.CanReach(mid) {
			t.Fatalf("in survival: CanReach(%v) = %v, CanReach(%v) = %v; want true, false", near, p.CanReach(near), mid, p.CanReach(mid))
		}
		p.SetGameMode(world.GameModeCreative)
		if !p.CanReach(mid) || p.CanReach(farPos.Vec3Middle()) {
			t.Fatal("in creative the player reaches 14 blocks, no further")
		}
		p.SetGameMode(world.GameModeSpectator)
		if p.CanReach(near) {
			t.Fatal("a spectator reached a block")
		}
	})
}

func TestPlayerEditSign(t *testing.T) {
	for _, hanging := range []bool{false, true} {
		w, _ := clientTestWorld(t)
		withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
			if hanging {
				tx.SetBlock(signPos, hangingSign(), nil)
			} else {
				tx.SetBlock(signPos, standingSign(), nil)
			}
			if err := p.EditSign(signPos, hanging, "front\n\n", ""); err != nil {
				t.Fatalf("EditSign: %v", err)
			}
			var front block.SignText
			switch b := tx.Block(signPos).(type) {
			case block.Sign:
				front = b.Front
			case block.HangingSign:
				front = b.Front
			}
			if front.Text != "front" || front.Owner != p.XUID() {
				t.Fatalf("hanging %v: front reads %q by %q, want \"front\" by the player", hanging, front.Text, front.Owner)
			}
		})
	}
}

func TestPlayerEditSignRefuses(t *testing.T) {
	long := strings.Repeat("a", MaxSignTextLength+1)
	cases := []struct {
		name        string
		pos         cube.Pos
		front, back string
	}{
		{"out of reach", farPos, "a", ""},
		{"long front", signPos, long, ""},
		{"long back", signPos, "", long},
		{"invalid front", signPos, "a\xff", ""},
		{"invalid back", signPos, "", "a\xff"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, _ := clientTestWorld(t)
			withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
				tx.SetBlock(c.pos, standingSign(), nil)
				if err := p.EditSign(c.pos, false, c.front, c.back); err == nil {
					t.Fatal("EditSign succeeded")
				}
				if s := tx.Block(c.pos).(block.Sign); s.Front.Text != "" || s.Back.Text != "" {
					t.Fatalf("a refused edit wrote %q and %q", s.Front.Text, s.Back.Text)
				}
			})
		})
	}
	t.Run("long until trimmed", func(t *testing.T) {
		w, _ := clientTestWorld(t)
		withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
			tx.SetBlock(signPos, standingSign(), nil)
			text := strings.Repeat("a", MaxSignTextLength)
			if err := p.EditSign(signPos, false, text+"\n", ""); err != nil {
				t.Fatalf("EditSign: %v", err)
			}
			if s := tx.Block(signPos).(block.Sign); s.Front.Text != text {
				t.Fatalf("front reads %d bytes, want %d", len(s.Front.Text), len(text))
			}
		})
	})
}

func TestPlayerEditSignIgnoresOtherBlocks(t *testing.T) {
	for _, c := range []struct {
		name    string
		b       world.Block
		hanging bool
	}{
		{"hanging edit of a sign", standingSign(), true},
		{"edit of a hanging sign", hangingSign(), false},
		{"air", block.Air{}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, _ := clientTestWorld(t)
			withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
				tx.SetBlock(signPos, c.b, nil)
				if err := p.EditSign(signPos, c.hanging, "text", ""); err != nil {
					t.Fatalf("EditSign: %v, want nil", err)
				}
				if got := tx.Block(signPos); got != c.b {
					t.Fatalf("the block became %#v", got)
				}
			})
		})
	}
}
