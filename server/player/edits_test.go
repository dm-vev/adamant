package player

import (
	"strings"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
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

// pageTurnHandler records the pages a player turns lecterns to.
type pageTurnHandler struct {
	NopHandler
	turns []int
}

func (h *pageTurnHandler) HandleLecternPageTurn(_ *Context, _ cube.Pos, _ int, page *int) {
	h.turns = append(h.turns, *page)
}

func TestPlayerTurnLecternPage(t *testing.T) {
	book := item.NewStack(item.WrittenBook{Title: "t", Pages: []string{"a", "b", "c"}}, 1)
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		h := &pageTurnHandler{}
		p.Handle(h)
		tx.SetBlock(signPos, block.Lectern{Book: book}, nil)
		tx.SetBlock(farPos, block.Lectern{Book: book}, nil)

		if err := p.TurnLecternPage(farPos, 1); err == nil {
			t.Fatal("the player turned the page of a lectern out of reach")
		}
		if err := p.TurnLecternPage(signPos.Side(cube.FaceUp), 1); err == nil {
			t.Fatal("the player turned the page of air")
		}
		if len(h.turns) != 0 {
			t.Fatalf("refused turns reached the handler: %v", h.turns)
		}
		if err := p.TurnLecternPage(signPos, 2); err != nil {
			t.Fatalf("TurnLecternPage: %v", err)
		}
		if l := tx.Block(signPos).(block.Lectern); l.Page != 2 || len(h.turns) != 1 {
			t.Fatalf("the lectern is at page %d after %v turns, want page 2 after one", l.Page, h.turns)
		}
		if l := tx.Block(farPos).(block.Lectern); l.Page != 0 {
			t.Fatalf("the lectern out of reach is at page %d", l.Page)
		}
	})
}

// operatorName is the name of the operator of withOperator.
const operatorName = "edits operator"

var (
	installOperators sync.Once
	// operatorIDs are the levels of the players withLevel made operators,
	// by their UUIDs.
	operatorIDs sync.Map
)

// operatorPermissions are the levels of the players of operatorIDs.
type operatorPermissions struct{}

func (operatorPermissions) PlayerLevel(id uuid.UUID) cmd.Level {
	if l, ok := operatorIDs.Load(id); ok {
		return l.(cmd.Level)
	}
	return cmd.LevelAll
}
func (operatorPermissions) CommandLevel(_ string, declared cmd.Level) cmd.Level { return declared }
func (operatorPermissions) CommandBlockLevel() cmd.Level                        { return cmd.LevelGameMaster }

// withOperator runs f with a player in w named operatorName, an operator of
// cmd.LevelOwner in the game mode passed, unless op is false.
func withOperator(t *testing.T, w *world.World, mode world.GameMode, op bool, f func(tx *world.Tx, p *Player)) {
	t.Helper()
	if !op {
		withLevel(t, w, mode, false, cmd.LevelAll, f)
		return
	}
	withLevel(t, w, mode, true, cmd.LevelOwner, f)
}

// withLevel runs f with a player in w in the game mode passed: named
// operatorName, an operator of the level passed, if op, a player of
// cmd.LevelAll otherwise.
func withLevel(t *testing.T, w *world.World, mode world.GameMode, op bool, level cmd.Level, f func(tx *world.Tx, p *Player)) {
	t.Helper()
	installOperators.Do(func() {
		cmd.SetOperatorChecker(func(id uuid.UUID) bool {
			_, ok := operatorIDs.Load(id)
			return ok
		})
		cmd.SetPermissions(operatorPermissions{})
	})
	name := operatorName
	if !op {
		name = "edits player"
	}
	pos := mgl64.Vec3{0.5, 100, 0.5}
	handle := world.EntitySpawnOpts{Position: pos, ID: uuid.New()}.New(Type, Config{Name: name, Position: pos, GameMode: mode})
	if op {
		operatorIDs.Store(handle.UUID(), level)
	}
	<-w.Exec(func(tx *world.Tx) {
		f(tx, tx.AddEntity(handle).(*Player))
	})
}

// TestGameMasterBlocksNeedLevel2 checks that an operator in creative may
// use the blocks of game masters from cmd.LevelGameMaster on, as Java's
// canUseGameMasterBlocks asks for COMMANDS_GAMEMASTER: not at level 0 or
// 1.
func TestGameMasterBlocksNeedLevel2(t *testing.T) {
	for _, c := range []struct {
		level cmd.Level
		want  bool
	}{
		{cmd.LevelAll, false}, {cmd.LevelModerator, false}, {cmd.LevelGameMaster, true}, {cmd.LevelAdmin, true}, {cmd.LevelOwner, true},
	} {
		w, _ := clientTestWorld(t)
		withLevel(t, w, world.GameModeCreative, true, c.level, func(tx *world.Tx, p *Player) {
			if got := p.CanUseGameMasterBlocks(); got != c.want {
				t.Errorf("an operator of level %v: CanUseGameMasterBlocks() = %v, want %v", c.level, got, c.want)
			}
		})
	}
}

func TestPlayerCanUseGameMasterBlocks(t *testing.T) {
	for _, c := range []struct {
		mode world.GameMode
		op   bool
		want bool
	}{
		{world.GameModeCreative, true, true},
		{world.GameModeCreative, false, false},
		{world.GameModeSurvival, true, false},
		{world.GameModeSpectator, true, false},
	} {
		w, _ := clientTestWorld(t)
		withOperator(t, w, c.mode, c.op, func(tx *world.Tx, p *Player) {
			if got := p.CanUseGameMasterBlocks(); got != c.want {
				t.Errorf("%v, operator %v: CanUseGameMasterBlocks() = %v, want %v", c.mode, c.op, got, c.want)
			}
		})
	}
}

func TestPlayerEditJigsaw(t *testing.T) {
	placed := block.NewJigsaw(cube.FaceNorth, 0)
	edited := block.NewJigsaw(cube.FaceUp, 2)
	edited.Name, edited.Pool, edited.PlacementPriority = "minecraft:a", "minecraft:pool", 3

	t.Run("edit", func(t *testing.T) {
		w, _ := clientTestWorld(t)
		withOperator(t, w, world.GameModeCreative, true, func(tx *world.Tx, p *Player) {
			tx.SetBlock(signPos, placed, nil)
			if err := p.EditJigsaw(signPos, edited); err != nil {
				t.Fatalf("EditJigsaw: %v", err)
			}
			got := tx.Block(signPos).(block.Jigsaw)
			want := edited
			want.Facing, want.Rotation = placed.Facing, placed.Rotation
			if got != want {
				t.Fatalf("the jigsaw is %#v, want %#v", got, want)
			}
		})
	})
	for _, c := range []struct {
		name string
		pos  cube.Pos
		mode world.GameMode
		op   bool
	}{
		{"out of reach", farPos, world.GameModeCreative, true},
		{"not an operator", signPos, world.GameModeCreative, false},
		{"survival", signPos, world.GameModeSurvival, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, _ := clientTestWorld(t)
			withOperator(t, w, c.mode, c.op, func(tx *world.Tx, p *Player) {
				tx.SetBlock(c.pos, placed, nil)
				if err := p.EditJigsaw(c.pos, edited); err == nil {
					t.Fatal("EditJigsaw succeeded")
				}
				if got := tx.Block(c.pos); got != world.Block(placed) {
					t.Fatalf("a refused edit made the jigsaw %#v", got)
				}
			})
		})
	}
	t.Run("no jigsaw", func(t *testing.T) {
		w, _ := clientTestWorld(t)
		withOperator(t, w, world.GameModeCreative, true, func(tx *world.Tx, p *Player) {
			if err := p.EditJigsaw(signPos, edited); err != nil {
				t.Fatalf("EditJigsaw: %v, want nil", err)
			}
			if _, ok := tx.Block(signPos).(block.Air); !ok {
				t.Fatalf("an edit of air placed %#v", tx.Block(signPos))
			}
		})
	})
}
