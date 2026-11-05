package session

import (
	"sync"
	"time"

	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// The session knows of the windows of its client only what it sent and what
// the client sent back, and the two disagree after a screen goes away with
// no ContainerClose (a death, a change of dimension, a form shown over it), a
// close names a window the session has no record of, or a ContainerOpen is
// shown to no one. BDS keeps this state alike, and so did the session, which
// then answered no request to open the inventory or a chest again: an
// OpenInventory was ignored as a repeat of the one that opened a screen the
// client had long closed, and a chest as a menu that was open already.
//
// So the session ends what it believed open when it knows the client cannot
// have it open:
//
//   - a window it shows replaces the inventory it showed;
//   - a death, a respawn and a change of dimension end the inventory;
//   - a request of the client that it cannot make with a window open (to open
//     the inventory, or to click a block or an entity) ends the windows
//     shown more than the grace ago (windowGraceTicks, windowGraceMin).
//
// Within the grace of a window shown, the client may not have seen it yet,
// and what it sends was sent as if it had not: a second OpenInventory is a
// repeat of the first and crashes the client if answered, and a close for the
// window it had before is for that one. Such a request is ignored. Each
// time the session ignores or ends a window it logs it at the debug level,
// once every windowLogEvery for each kind (see logWindow).

// windowGraceTicks is how many ticks of the session a window shown to the
// client stays too recent for what the client sends to say it is closed.
const windowGraceTicks = 20

// windowGraceMin is the least wall time the same takes; it is longer if the
// connection of the client takes longer to answer, see windowFresh.
const windowGraceMin = time.Second

// windowLogEvery is the least time between two debug lines of a kind of
// ignored request.
const windowLogEvery = 5 * time.Second

// windowStamp is when a window was shown to the client.
type windowStamp struct {
	tick int64
	at   time.Time
}

// windows is the state of the windows of a session that the menu of the
// player does not hold: whether the client shows its inventory, and when it
// was shown that and its menu.
type windows struct {
	// ticks counts the ticks of the session.
	ticks int64
	// inventory is whether the inventory of the client is open as the
	// session knows; inventoryAt is when it was opened.
	inventory   bool
	inventoryAt windowStamp
	// menuAt is when the window of the menu now open was shown.
	menuAt windowStamp

	logMu  sync.Mutex
	logged map[string]windowLogged
}

// windowLogged is when a kind of ignored request was last logged, and how
// many were left out since.
type windowLogged struct {
	at         time.Time
	suppressed int
}

// tick counts a tick of the session.
func (w *windows) tick() { w.ticks++ }

// stamp returns the time now.
func (w *windows) stamp() windowStamp {
	return windowStamp{tick: w.ticks, at: time.Now()}
}

// windowFresh reports whether a window shown at the stamp passed is too
// recent for the client to have closed it: it was shown less than
// windowGraceTicks ticks and less than windowGraceMin ago, or twice the
// latency of the client, whichever is more.
func (s *Session) windowFresh(at windowStamp) bool {
	if s.windows.ticks-at.tick >= windowGraceTicks {
		return false
	}
	grace := windowGraceMin
	if s.conn != nil {
		grace = max(grace, 2*s.conn.Latency())
	}
	return time.Since(at.at) < grace
}

// logWindow writes a debug line of the kind passed with args, at most once
// every windowLogEvery for each kind, with the number of those it left out:
// a client that is stuck repeats its request every few moments.
func (s *Session) logWindow(kind string, args ...any) {
	if s.conf.Log == nil {
		return
	}
	now := time.Now()
	w := &s.windows
	w.logMu.Lock()
	if w.logged == nil {
		w.logged = make(map[string]windowLogged)
	}
	e := w.logged[kind]
	if !e.at.IsZero() && now.Sub(e.at) < windowLogEvery {
		e.suppressed++
		w.logged[kind] = e
		w.logMu.Unlock()
		return
	}
	suppressed := e.suppressed
	w.logged[kind] = windowLogged{at: now}
	w.logMu.Unlock()

	args = append(args, "tick", w.ticks)
	if suppressed > 0 {
		args = append(args, "suppressed", suppressed)
	}
	if s.conn != nil {
		args = append(args, "player", s.conn.IdentityData().DisplayName)
	}
	s.conf.Log.Debug("window: "+kind, args...)
}

// resetInventory ends the inventory window the session believes open, as
// the client can no longer have it open: why names the reason in the log.
func (s *Session) resetInventory(why string) {
	if !s.windows.inventory {
		return
	}
	s.windows.inventory = false
	s.logWindow("inventory closed by the server", "why", why)
}

// openInventory answers the request of the client to open its inventory: a
// ContainerOpen of window 0, unless it is shown one already, or a menu, which
// the client cannot have missed.
func (s *Session) openInventory(c Controllable) {
	w := &s.windows
	if w.inventory {
		if s.windowFresh(w.inventoryAt) {
			// When there is latency, this might end up being sent multiple times. If we send a ContainerOpen
			// multiple times, the client crashes.
			s.logWindow("OpenInventory ignored", "why", "inventory open", "age", w.ticks-w.inventoryAt.tick)
			return
		}
		s.logWindow("OpenInventory reopens", "why", "inventory shown and never closed", "age", w.ticks-w.inventoryAt.tick)
	}
	if s.menu.Load() != nil {
		if s.windowFresh(w.menuAt) {
			s.logWindow("OpenInventory ignored", "why", "menu open", "age", w.ticks-w.menuAt.tick)
			return
		}
		s.logWindow("menu closed by the server", "why", "OpenInventory with a menu never closed", "age", w.ticks-w.menuAt.tick)
		c.CloseMenu(false)
	}
	w.inventory, w.inventoryAt = true, w.stamp()
	pos := c.Position()
	s.writePacket(&mc.ContainerOpenPacket{
		WindowID:                0,
		ContainerType:           0xff,
		ContainerEntityUniqueID: -1,
		ContainerPosition: mc.BlockPos{
			int32(pos[0]),
			int32(pos[1]),
			int32(pos[2]),
		},
	})
}

// closeUnknown handles the close the client sent of a window the session
// has no record of: it is a repeat of a close, or one that crossed the
// opening of the window the session shows now, if that was shown a moment
// ago; otherwise the client shows nothing, and what the session thought it
// did is ended.
func (s *Session) closeUnknown(c Controllable, window byte) {
	w := &s.windows
	menu := s.menu.Load()
	if !w.inventory && menu == nil {
		s.logWindow("ContainerClose ignored", "why", "no window open", "window", window)
		return
	}
	if w.inventory {
		if s.windowFresh(w.inventoryAt) {
			s.logWindow("ContainerClose ignored", "why", "inventory shown a moment ago", "window", window)
		} else {
			s.logWindow("inventory closed by the server", "why", "ContainerClose of another window", "window", window)
			w.inventory = false
		}
	}
	if menu != nil {
		if s.windowFresh(w.menuAt) {
			s.logWindow("ContainerClose ignored", "why", "menu shown a moment ago", "window", window)
		} else {
			s.logWindow("menu closed by the server", "why", "ContainerClose of another window", "window", window)
			c.CloseMenu(false)
		}
	}
}

// clientActs ends the windows the session shows for longer than the grace
// when the client acts in the world, as it cannot with a window open: it
// clicks a block or an entity. What ends is the inventory and the menu, the
// latter closed as the server closes it, so that the client is sure to
// show nothing.
func (s *Session) clientActs(c Controllable) {
	w := &s.windows
	if w.inventory && !s.windowFresh(w.inventoryAt) {
		s.logWindow("inventory closed by the server", "why", "the client acts in the world", "age", w.ticks-w.inventoryAt.tick)
		w.inventory = false
	}
	if s.menu.Load() != nil && !s.windowFresh(w.menuAt) {
		s.logWindow("menu closed by the server", "why", "the client acts in the world", "age", w.ticks-w.menuAt.tick)
		c.CloseMenu(false)
	}
}
