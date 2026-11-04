package player

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// leaveLog is the log of what a player leaving does: the calls to its
// client, its Left hook and the quit message written to the chat, each with
// what it found of the player.
type leaveLog struct {
	events []string
}

func (l *leaveLog) add(format string, a ...any) {
	l.events = append(l.events, fmt.Sprintf(format, a...))
}

// leaveClient is a client that logs the calls of the teardown of its
// player.
type leaveClient struct {
	*clienttest.Recorder
	log *leaveLog
	p   *Player
	tx  *world.Tx
}

func (c *leaveClient) Disconnect(msg text.Text) {
	c.Recorder.Disconnect(msg)
	c.log.add("Disconnect")
}

func (c *leaveClient) CloseConnection() {
	c.Recorder.CloseConnection()
	c.log.add("CloseConnection")
}

func (c *leaveClient) Close(tx *world.Tx) {
	c.Recorder.Close(tx)
	_, inWorld := c.p.H().Entity(c.tx)
	c.log.add("Close tx=%t inWorld=%t subscribed=%t handleClosed=%t", tx != nil, inWorld, chat.Global.Subscribed(c.p), c.p.H().Closed())
}

// countItems counts the items the player holds in its inventories and its
// cursor.
func countItems(p *Player) (n int) {
	for _, inv := range []interface{ Items() []item.Stack }{p.inv, p.offHand, p.armour, p.ui} {
		for _, it := range inv.Items() {
			n += it.Count()
		}
	}
	return n
}

// leaveChat is a subscriber of the chat that logs the messages written to
// it, with the items the player held then.
type leaveChat struct {
	id  uuid.UUID
	log *leaveLog
	p   *Player
}

func (s *leaveChat) UUID() uuid.UUID { return s.id }
func (s *leaveChat) Message(a ...any) {
	s.log.add("chat %q items=%d", strings.TrimSpace(fmt.Sprint(a...)), countItems(s.p))
}

// withLeavingPlayer runs f with a player named Alex in w, who has a client
// that logs into log, a Left hook that runs left and logs, and the quit
// message of the server. The player is subscribed to the chat, as the
// server welcomes it, and so is a subscriber that logs what Alex sees.
func withLeavingPlayer(t *testing.T, w *world.World, left func(tx *world.Tx, p *Player), f func(tx *world.Tx, p *Player, c *leaveClient, log *leaveLog)) {
	t.Helper()
	log := &leaveLog{}
	c := &leaveClient{Recorder: clienttest.New(), log: log}
	pos := mgl64.Vec3{0.5, 100, 0.5}
	conf := Config{
		Name: "Alex", Position: pos, GameMode: world.GameModeSurvival, Client: c, QuitMessage: chat.MessageQuit,
		Left: func(tx *world.Tx, p *Player) {
			_, inWorld := p.H().Entity(tx)
			log.add("Left tx=%t inWorld=%t menu=%t items=%d cursor=%d", tx != nil, inWorld, p.Menu() != nil, countItems(p), len(p.ui.Items()))
			if left != nil {
				left(tx, p)
			}
		},
	}
	handle := world.EntitySpawnOpts{Position: pos, ID: uuid.New()}.New(Type, conf)
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(handle).(*Player)
		c.p, c.tx = p, tx
		sub := &leaveChat{id: uuid.New(), log: log, p: p}
		chat.Global.Subscribe(p)
		chat.Global.Subscribe(sub)
		defer chat.Global.Unsubscribe(sub)
		f(tx, p, c, log)
	})
}

// A player that quits with a client tears itself down in the order the
// session did: it disconnects and closes the connection of its client,
// moves the items of its cursor into its inventory and closes its menu,
// calls Left with itself still in the world, clears its inventories,
// writes the quit message, unsubscribes from the chat, removes itself from
// the world, closes its client and, last, closes its handle. Only the first
// quit or Leave does anything.
func TestPlayerQuitLeavesInOrder(t *testing.T) {
	w, _ := clientTestWorld(t)
	withLeavingPlayer(t, w, nil, func(tx *world.Tx, p *Player, c *leaveClient, log *leaveLog) {
		tx.SetBlock(signPos, block.NewChest(), nil)
		p.OpenBlockContainer(signPos, tx)
		_ = p.ui.SetItem(0, item.NewStack(item.Apple{}, 1))
		_ = p.inv.SetItem(0, item.NewStack(item.Apple{}, 1))
		rebind(tx, p)
		if p.Menu() == nil {
			t.Fatal("the player has no menu open")
		}

		p.Disconnect("bye")
		want := []string{
			"Disconnect",
			"CloseConnection",
			"Left tx=true inWorld=true menu=false items=2 cursor=0",
			`chat "§r§eAlex left the game§r" items=0`,
			"Close tx=true inWorld=false subscribed=false handleClosed=false",
		}
		if !slices.Equal(log.events, want) {
			t.Fatalf("the player left as\n\t%s\nwant\n\t%s", strings.Join(log.events, "\n\t"), strings.Join(want, "\n\t"))
		}
		if !p.H().Closed() {
			t.Error("the handle of the player is open after it left")
		}
		menus := c.CallsTo("CloseMenu")
		if len(menus) != 1 || menus[0].Args[1] != false {
			t.Errorf("the client was shown %d menus closed, want one closed by the server", len(menus))
		}
		if calls := methods(c.Calls()); slices.Index(calls, "CloseMenu") > slices.Index(calls, "Close") {
			t.Errorf("the client was called %v, want CloseMenu before Close", calls)
		}

		// Neither quitting nor leaving again does anything.
		p.Disconnect("again")
		p.Leave(tx)
		if len(log.events) != len(want) || len(c.CallsTo("Close")) != 1 || len(c.CallsTo("Disconnect")) != 1 {
			t.Errorf("leaving again did %v", log.events[len(want):])
		}
	})
}

// A panicking Left still lets every later step of the teardown run, and
// the panic is raised again after the last.
func TestPlayerLeavePanickingLeftCompletesTeardown(t *testing.T) {
	w, _ := clientTestWorld(t)
	withLeavingPlayer(t, w, func(*world.Tx, *Player) { panic("left panic") }, func(tx *world.Tx, p *Player, c *leaveClient, log *leaveLog) {
		_ = p.inv.SetItem(0, item.NewStack(item.Apple{}, 1))
		_ = p.offHand.SetItem(0, item.NewStack(item.Shield{}, 1))
		p.armour.SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
		rebind(tx, p)

		var got any
		func() {
			defer func() { got = recover() }()
			p.Disconnect("bye")
		}()
		if got != "left panic" {
			t.Fatalf("Disconnect panicked with %v, want the panic of Left", got)
		}
		want := []string{
			"Disconnect",
			"CloseConnection",
			"Left tx=true inWorld=true menu=false items=3 cursor=0",
			`chat "§r§eAlex left the game§r" items=0`,
			"Close tx=true inWorld=false subscribed=false handleClosed=false",
		}
		if !slices.Equal(log.events, want) {
			t.Fatalf("the player left as\n\t%s\nwant\n\t%s", strings.Join(log.events, "\n\t"), strings.Join(want, "\n\t"))
		}
		if !p.H().Closed() {
			t.Error("the handle of the player is open after Left panicked")
		}
	})
}

// Leave, as an adapter calls it when its player did not quit at once, tears
// the player down as quitting does after its quit handler, without
// disconnecting the client, and only once.
func TestPlayerLeaveRunsOnce(t *testing.T) {
	w, _ := clientTestWorld(t)
	withLeavingPlayer(t, w, nil, func(tx *world.Tx, p *Player, c *leaveClient, log *leaveLog) {
		p.Leave(tx)
		p.Leave(tx)
		want := []string{
			"Left tx=true inWorld=true menu=false items=0 cursor=0",
			`chat "§r§eAlex left the game§r" items=0`,
			"Close tx=true inWorld=false subscribed=false handleClosed=false",
		}
		if !slices.Equal(log.events, want) {
			t.Fatalf("the player left as\n\t%s\nwant\n\t%s", strings.Join(log.events, "\n\t"), strings.Join(want, "\n\t"))
		}
		if !p.H().Closed() {
			t.Error("the handle of the player is open after it left")
		}
	})
}

// rebind binds p to tx again. On a synchronous world, the slot functions of
// the held slot, the off hand and the armour run the Do of their broadcast at
// once, in a transaction of its own, which leaves p bound to that
// transaction once it ends; the teardown would then run in it.
func rebind(tx *world.Tx, p *Player) {
	_, _ = p.H().Entity(tx)
}
