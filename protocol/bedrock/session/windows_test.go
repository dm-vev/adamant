package session_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl32"
)

// windowChest and windowOther are the chests the tests of the windows open:
// two blocks from the player, which the world of the scenario has no
// blocks around.
var (
	windowChest = cube.Pos{1, -60, -2}
	windowOther = cube.Pos{-1, -60, -2}
)

// afterGrace is the ticks that pass for a window the session was told of
// to be old enough that what the client does says it is closed: more than
// session.WindowGraceTicks.
const afterGrace = session.WindowGraceTicks + 5

// windowsScenario is a scenario with the chests of the tests placed, and the
// client's view of its windows kept from what it was sent.
type windowsScenario struct {
	*scenario
	// inventory is whether the client shows its inventory, menu the window
	// it shows a menu in, 0 if none.
	inventory bool
	menu      byte
}

func newWindowsScenario(t *testing.T) *windowsScenario {
	t.Helper()
	return newWindowsScenarioIn(t, nil)
}

// newWindowsScenarioIn is newWindowsScenario in the world passed, which the
// caller closes, or in a world of its own if nil.
func newWindowsScenarioIn(t *testing.T, w *world.World) *windowsScenario {
	t.Helper()
	s := &windowsScenario{scenario: newScenario(t, "windows", scenarioConfig{world: w})}
	s.run(func(tx *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			t.Fatal(err)
		}
		tx.SetBlock(windowChest, block.NewChest(), nil)
		tx.SetBlock(windowOther, block.NewChest(), nil)
	})
	return s
}

// track updates what the client shows from the packets it was sent.
func (s *windowsScenario) track(pks []proto.Packet) {
	for _, pk := range pks {
		switch pk := pk.(type) {
		case *mc.ContainerOpenPacket:
			if pk.ContainerType == 0xff {
				s.inventory = true
			} else {
				// The screen of a menu replaces that of the inventory.
				s.menu, s.inventory = pk.WindowID, false
			}
		case *mc.ContainerClosePacket:
			if pk.ServerSide {
				if pk.WindowID == s.menu {
					s.menu = 0
				}
				if pk.WindowID == 0 {
					s.inventory = false
				}
			}
		}
	}
}

// do runs act, ticks the world and returns what the client was sent, which
// the client's view is updated with.
func (s *windowsScenario) do(act func()) []proto.Packet {
	s.t.Helper()
	pks := s.gather(act)
	s.track(pks)
	return pks
}

// ticks advances the world n ticks.
func (s *windowsScenario) ticks(n int) {
	s.t.Helper()
	for range n {
		s.advance()
	}
	// What the session sent meanwhile is read so that it does not mix with
	// the next step.
	s.track(s.do(func() {}))
}

// opens returns the ContainerOpen packets of pks.
func opens(pks []proto.Packet) (out []*mc.ContainerOpenPacket) {
	for _, pk := range pks {
		if o, ok := pk.(*mc.ContainerOpenPacket); ok {
			out = append(out, o)
		}
	}
	return out
}

// closes returns the ContainerClose packets of pks.
func closes(pks []proto.Packet) (out []*mc.ContainerClosePacket) {
	for _, pk := range pks {
		if c, ok := pk.(*mc.ContainerClosePacket); ok {
			out = append(out, c)
		}
	}
	return out
}

// clientOpensInventory sends what the client sends when its player opens its
// inventory, and returns what it was sent.
func (s *windowsScenario) clientOpensInventory() []proto.Packet {
	s.t.Helper()
	return s.do(func() {
		s.send(&mc.InteractPacket{ActionType: mc.InteractActionOpenInventory, TargetEntityRuntimeID: 1})
	})
}

// clientClosesInventory sends the close of the inventory.
func (s *windowsScenario) clientClosesInventory() []proto.Packet {
	s.t.Helper()
	return s.do(func() { s.send(&mc.ContainerClosePacket{WindowID: 0, ContainerType: 0xff}) })
}

// clickBlockData is the click of the client on the block at pos with the
// empty hand.
func clickBlockData(pos cube.Pos) *mc.UseItemTransactionData {
	return &mc.UseItemTransactionData{
		ActionType:       mc.UseItemActionClickBlock,
		TriggerType:      mc.TriggerTypePlayerInput,
		BlockPosition:    [3]int32{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		BlockFace:        int32(cube.FaceUp),
		HotBarSlot:       emptyHand,
		Position:         mgl32.Vec3{0.5, -58.38, 0.5},
		ClickedPosition:  mgl32.Vec3{0.5, 1, 0.5},
		ClientPrediction: mc.ClientPredictionSuccess,
	}
}

// clientClicks sends the click of the client on the block at pos with the
// empty hand.
func (s *windowsScenario) clientClicks(pos cube.Pos) []proto.Packet {
	s.t.Helper()
	return s.do(func() { s.send(&mc.InventoryTransactionPacket{TransactionData: clickBlockData(pos)}) })
}

// clientCloses sends the close of the window passed.
func (s *windowsScenario) clientCloses(window byte) []proto.Packet {
	s.t.Helper()
	return s.do(func() { s.send(&mc.ContainerClosePacket{WindowID: window, ContainerType: 0}) })
}

// clientClosesOpen closes what the client shows with the window it has: the
// inventory or the menu.
func (s *windowsScenario) clientClosesOpen() {
	s.t.Helper()
	if s.menu != 0 {
		w := s.menu
		s.track(s.clientCloses(w))
		s.menu = 0
	}
	if s.inventory {
		s.track(s.clientClosesInventory())
		s.inventory = false
	}
}

// wantInventory fails the test unless a client that shows nothing can open
// its inventory now, and then close it again.
func (s *windowsScenario) wantInventory(what string) {
	s.t.Helper()
	pks := s.clientOpensInventory()
	got := opens(pks)
	if len(got) != 1 || got[0].ContainerType != 0xff {
		s.t.Fatalf("%s: the client that asked for its inventory was sent %v, want a ContainerOpen of the inventory", what, summarise(pks))
	}
	s.clientClosesInventory()
	s.inventory = false
}

// wantChest fails the test unless a client that shows nothing can open the
// chest at pos now, and then close it again.
func (s *windowsScenario) wantChest(what string, pos cube.Pos) {
	s.t.Helper()
	pks := s.clientClicks(pos)
	got := opens(pks)
	if len(got) != 1 || got[0].ContainerType != mc.ContainerTypeContainer {
		s.t.Fatalf("%s: the client that clicked the chest at %v was sent %v, want a ContainerOpen of a container", what, pos, summarise(pks))
	}
	s.clientCloses(got[0].WindowID)
	s.menu = 0
}

// summarise names the window packets among pks.
func summarise(pks []proto.Packet) string {
	var out []string
	for _, pk := range pks {
		switch pk := pk.(type) {
		case *mc.ContainerOpenPacket:
			out = append(out, fmt.Sprintf("ContainerOpen{window %d, type %d}", pk.WindowID, pk.ContainerType))
		case *mc.ContainerClosePacket:
			out = append(out, fmt.Sprintf("ContainerClose{window %d, type %d, server side %v}", pk.WindowID, pk.ContainerType, pk.ServerSide))
		}
	}
	return fmt.Sprint(out)
}

// TestWindowsStuckStates replays how the state of the session about the
// windows of the client got stuck on a server of the owner: afterwards a
// client that shows nothing asks for its inventory or clicks a chest, and
// the session must answer with a ContainerOpen, at once for what the server
// knows about and once the window is older than session.WindowGraceTicks for
// what only the client knows.
func TestWindowsStuckStates(t *testing.T) {
	cases := []struct {
		name string
		// run leaves the client showing nothing.
		run func(s *windowsScenario)
		// wait is whether the client must be given the grace first.
		wait bool
	}{
		{
			name: "the server opens a chest while the inventory is open",
			run: func(s *windowsScenario) {
				s.clientOpensInventory()
				s.do(func() { s.run(func(tx *world.Tx, p *player.Player) { p.OpenBlockContainer(windowChest, tx) }) })
				// The client shows the chest and closes it by its window.
				s.clientClosesOpen()
			},
		},
		{
			name: "the inventory is open when the player dies and respawns",
			run: func(s *windowsScenario) {
				s.clientOpensInventory()
				s.do(func() { s.run(func(_ *world.Tx, p *player.Player) { p.Hurt(1000, entity.FreezeDamageSource{}) }) })
				s.ticks(2)
				s.do(func() { s.run(func(_ *world.Tx, p *player.Player) { p.Respawn() }) })
				s.ticks(2)
				// The client closed its screen with no ContainerClose.
				s.inventory = false
			},
		},
		{
			name: "the inventory is open when the server teleports the player",
			run: func(s *windowsScenario) {
				s.clientOpensInventory()
				s.do(func() {
					s.run(func(_ *world.Tx, p *player.Player) { p.Teleport(p.Position().Add([3]float64{3, 0, 3})) })
				})
				// The client closed its screen with no ContainerClose.
				s.inventory = false
			},
			wait: true,
		},
		{
			name: "the client closes the inventory by another window",
			run: func(s *windowsScenario) {
				s.clientOpensInventory()
				s.clientCloses(7)
				s.inventory = false
			},
			wait: true,
		},
		{
			name: "the client closes a chest by another window",
			run: func(s *windowsScenario) {
				s.clientClicks(windowChest)
				s.clientCloses(s.menu + 5)
				s.menu = 0
			},
			wait: true,
		},
		{
			name: "the close of a chest is lost",
			run: func(s *windowsScenario) {
				s.clientClicks(windowChest)
				s.menu = 0
			},
			wait: true,
		},
		{
			name: "a chest opens while a form is open",
			run: func(s *windowsScenario) {
				s.do(func() {
					s.run(func(_ *world.Tx, p *player.Player) {
						p.SendForm(form.NewModal(goldenModal{Yes: form.NewButton("yes", ""), No: form.NewButton("no", "")}, "Form"))
					})
				})
				// The client shows no chest while it shows a form.
				s.clientClicks(windowChest)
				s.menu = 0
			},
			wait: true,
		},
		{
			name: "the player dies with a chest open",
			run: func(s *windowsScenario) {
				s.clientClicks(windowChest)
				s.do(func() { s.run(func(_ *world.Tx, p *player.Player) { p.Hurt(1000, entity.FreezeDamageSource{}) }) })
				s.ticks(2)
				s.do(func() { s.run(func(_ *world.Tx, p *player.Player) { p.Respawn() }) })
				s.ticks(2)
				s.menu = 0
			},
		},
		{
			name: "the client closes with the 0xff of the chat",
			run: func(s *windowsScenario) {
				s.clientClicks(windowChest)
				s.clientCloses(0xff)
				s.menu = 0
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newWindowsScenario(t)
			c.run(s)
			if c.wait {
				s.ticks(afterGrace)
			}
			s.wantInventory("after " + c.name)
			s.wantChest("after "+c.name, windowChest)
			// And the other chest, and the inventory once more.
			s.wantChest("the other chest after "+c.name, windowOther)
			s.wantInventory("once more after " + c.name)
		})
	}
}

// recordedLog is a slog handler that keeps the messages it was given.
type recordedLog struct {
	mu   sync.Mutex
	msgs []string
}

func (l *recordedLog) Enabled(context.Context, slog.Level) bool { return true }
func (l *recordedLog) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, r.Message)
	return nil
}
func (l *recordedLog) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *recordedLog) WithGroup(string) slog.Handler      { return l }

// count returns how many times the message passed was logged.
func (l *recordedLog) count(msg string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, m := range l.msgs {
		if m == msg {
			n++
		}
	}
	return n
}

// TestWindowsLogWhatTheyIgnore checks that a request the session ignores or
// answers by ending a window it believed open is logged at the debug level,
// once for each kind however often the client repeats it.
func TestWindowsLogWhatTheyIgnore(t *testing.T) {
	logs := &recordedLog{}
	s := &windowsScenario{scenario: newScenario(t, "windows", scenarioConfig{session: func(conf *session.Config) {
		conf.Log = slog.New(logs)
	}})}
	s.run(func(tx *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			t.Fatal(err)
		}
		tx.SetBlock(windowChest, block.NewChest(), nil)
	})
	s.clientOpensInventory()
	for range 4 {
		s.clientOpensInventory()
	}
	if n := logs.count("window: OpenInventory ignored"); n != 1 {
		t.Errorf("a repeated OpenInventory was logged %d times, want once", n)
	}
	for range 3 {
		s.clientCloses(77)
	}
	if n := logs.count("window: ContainerClose ignored"); n != 1 {
		t.Errorf("a close of another window was logged %d times, want once", n)
	}
	s.ticks(afterGrace)
	s.clientOpensInventory()
	if n := logs.count("window: OpenInventory reopens"); n != 1 {
		t.Errorf("an OpenInventory over a stale inventory was logged %d times, want once", n)
	}
	// A chest never closed on the client, clicked again once old.
	s.clientCloses(0)
	s.clientClicks(windowChest)
	s.ticks(afterGrace)
	s.clientClicks(windowChest)
	if n := logs.count("window: menu closed by the server"); n != 1 {
		t.Errorf("the menu of a client that clicks a chest again was ended %d times, want once", n)
	}
}

// TestWindowsInventoryOpensAfterChangingDimension checks that a client whose
// screen went away with a change of dimension, which sends no
// ContainerClose, can open its inventory in the other dimension, at once.
func TestWindowsInventoryOpensAfterChangingDimension(t *testing.T) {
	// The worlds close once the scenario ended: cleanups run last first.
	overworld := goldenWorld(t)
	t.Cleanup(func() { _ = overworld.Close() })
	nether := world.Config{
		Log:             slog.New(slog.DiscardHandler),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Dim:             world.Nether,
		Generator:       goldenGenerator(),
		Provider:        &goldenProvider{NopProvider: world.NopProvider{Set: goldenSettings()}},
		Seed:            goldenSeed,
	}.New()
	t.Cleanup(func() { _ = nether.Close() })
	s := newWindowsScenarioIn(t, overworld)

	s.clientOpensInventory()
	s.do(func() {
		s.run(func(tx *world.Tx, p *player.Player) { p.TeleportToWorld(tx, nether, p.Position()) })
		s.w = nether
	})
	s.ticks(3)
	s.inventory = false
	s.wantInventory("in the nether")
	s.do(func() {
		s.run(func(tx *world.Tx, p *player.Player) { p.TeleportToWorld(tx, overworld, p.Position()) })
		s.w = overworld
	})
	s.ticks(3)
	s.wantInventory("back in the overworld")
}
