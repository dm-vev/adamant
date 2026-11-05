package session_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// wantAnswer fails the test unless pks holds exactly one ContainerClose, not
// server side, that names the window and type passed.
func wantAnswer(t *testing.T, what string, pks []proto.Packet, window, containerType byte) {
	t.Helper()
	got := closes(pks)
	if len(got) != 1 || got[0].ServerSide || got[0].WindowID != window || got[0].ContainerType != containerType {
		t.Fatalf("%s: the client was sent %v, want one ContainerClose{window %d, type %d, server side false}",
			what, summarise(pks), window, containerType)
	}
}

// TestWindowsCloseAnyWindow tests the close of window 0xff, which the client
// sends when it opens the inventory at the same time as chat: BDS closes what
// is open and answers with a ContainerClose naming the last window, type 247,
// or, with nothing open, answers the same and changes nothing.
func TestWindowsCloseAnyWindow(t *testing.T) {
	t.Run("the inventory is open", func(t *testing.T) {
		s := newWindowsScenario(t)
		s.clientOpensInventory()
		s.ticks(afterGrace)
		wantAnswer(t, "inventory open", s.clientCloses(0xff), 0, 247)
		s.inventory = false
		// The state after: the inventory is closed, and opens again at once.
		s.wantInventory("after the 0xff")
		s.wantChest("after the 0xff", windowChest)
	})
	t.Run("the inventory was opened a moment ago", func(t *testing.T) {
		s := newWindowsScenario(t)
		s.clientOpensInventory()
		// The 0xff crossed the ContainerOpen: it closes the inventory, and the
		// answer closes what the client shows of it.
		wantAnswer(t, "inventory fresh", s.clientCloses(0xff), 0, 247)
		s.inventory = false
		s.wantInventory("after the 0xff")
	})
	t.Run("a chest is open", func(t *testing.T) {
		s := newWindowsScenario(t)
		open := opens(s.clientClicks(windowChest))
		if len(open) != 1 {
			t.Fatalf("the click on the chest opened %d windows", len(open))
		}
		s.ticks(afterGrace)
		wantAnswer(t, "chest open", s.clientCloses(0xff), open[0].WindowID, open[0].ContainerType)
		s.menu = 0
		// The menu is closed as the client asked: nothing was sent of the server
		// closing it, and the same chest opens again at once.
		s.wantChest("after the 0xff", windowChest)
		s.wantChest("the other chest after the 0xff", windowOther)
		s.wantInventory("after the 0xff")
	})
	t.Run("a chest is open for a moment", func(t *testing.T) {
		s := newWindowsScenario(t)
		open := opens(s.clientClicks(windowChest))
		if len(open) != 1 {
			t.Fatalf("the click on the chest opened %d windows", len(open))
		}
		// A 0xff that crossed the ContainerOpen of the chest is answered by the
		// close of that window, sent after its open: the client shows no chest
		// and the session has none open, whichever the order they met in.
		pks := s.clientCloses(0xff)
		wantAnswer(t, "chest fresh", pks, open[0].WindowID, open[0].ContainerType)
		s.menu = 0
		s.wantChest("after the 0xff", windowChest)
		s.wantInventory("after the 0xff")
	})
	t.Run("nothing is open", func(t *testing.T) {
		s := newWindowsScenario(t)
		s.wantChest("before", windowChest)
		s.ticks(afterGrace)
		// The last window is the chest's, which the client closed.
		last := byte(0)
		if o := opens(s.clientClicks(windowChest)); len(o) == 1 {
			last = o[0].WindowID
			s.clientCloses(last)
			s.menu = 0
		}
		s.ticks(afterGrace)
		wantAnswer(t, "nothing open", s.clientCloses(0xff), last, 247)
		wantAnswer(t, "nothing open again", s.clientCloses(0xff), last, 247)
		s.wantInventory("after the 0xff")
		s.wantChest("after the 0xff", windowChest)
	})
}

// TestWindowsServerCloseEchoes tests the close the server does, which the
// client may answer with a ContainerClose of its own: the session answers
// that once, as it answers any close, and a client that answers every answer
// is answered every time but never sent a close the client did not ask for.
func TestWindowsServerCloseEchoes(t *testing.T) {
	serverCloses := func(s *windowsScenario) *mc.ContainerClosePacket {
		t.Helper()
		open := opens(s.clientClicks(windowChest))
		if len(open) != 1 {
			t.Fatalf("the click on the chest opened %d windows", len(open))
		}
		s.ticks(afterGrace)
		pks := s.do(func() { s.run(func(_ *world.Tx, p *player.Player) { p.CloseMenu(false) }) })
		got := closes(pks)
		if len(got) != 1 || !got[0].ServerSide || got[0].WindowID != open[0].WindowID {
			t.Fatalf("the server closing the chest sent %v", summarise(pks))
		}
		s.menu = 0
		return got[0]
	}

	t.Run("an echo is answered once", func(t *testing.T) {
		s := newWindowsScenario(t)
		closed := serverCloses(s)
		wantAnswer(t, "echo", s.clientCloses(closed.WindowID), closed.WindowID, 0)
		s.ticks(afterGrace)
		s.wantChest("after the echo", windowChest)
	})
	t.Run("an echo that says it is server side is answered too", func(t *testing.T) {
		s := newWindowsScenario(t)
		closed := serverCloses(s)
		pks := s.do(func() {
			s.send(&mc.ContainerClosePacket{WindowID: closed.WindowID, ContainerType: closed.ContainerType, ServerSide: true})
		})
		if got := closes(pks); len(got) != 1 || got[0].ServerSide {
			t.Fatalf("an echo flagged as server side was answered with %v, want one close, not server side", summarise(pks))
		}
		s.ticks(afterGrace)
		s.wantChest("after the echo", windowChest)
	})
	t.Run("a client that answers every answer", func(t *testing.T) {
		s := newWindowsScenario(t)
		closed := serverCloses(s)
		last := &mc.ContainerClosePacket{WindowID: closed.WindowID, ContainerType: closed.ContainerType}
		for i := range 50 {
			pks := s.do(func() { s.send(last) })
			got := closes(pks)
			if len(got) != 1 || got[0].ServerSide {
				t.Fatalf("round %d: one close of the client was answered with %v", i, summarise(pks))
			}
			last = got[0]
		}
		// And nothing is sent that the client did not ask for.
		if pks := s.do(func() {}); len(closes(pks)) != 0 {
			t.Fatalf("a close was sent with no request: %v", summarise(pks))
		}
		s.ticks(afterGrace)
		s.wantChest("after the echoes", windowChest)
	})
}
