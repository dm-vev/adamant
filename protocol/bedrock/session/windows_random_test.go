package session_test

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// The blocks the random sequences open, besides windowChest.
var (
	windowTable   = cube.Pos{2, -60, 2}
	windowFurnace = cube.Pos{-2, -60, 2}
)

// randomWindowClient is a client that opens and closes windows at random, as
// a client that shows one screen at a time does. It keeps what it shows
// from what the session sends it, and acts only as a client can: it
// opens the inventory or clicks a block only when it shows nothing, and
// closes a window by the ID it has. What it does that the session cannot tell
// is, when lying is set, to not send the close of a screen that went away
// with a death or a change of dimension, to not show the window it is sent
// while it shows a form, and to repeat a request to open the inventory.
type randomWindowClient struct {
	*windowsScenario
	rng   *rand.Rand
	lying bool
	// base is where the player starts, around which it is teleported.
	base mgl64.Vec3
	// form is the ID of the form the client shows, 0 if none; formOpen is
	// whether it shows one.
	form     uint32
	formOpen bool
	// log is what the client did, to be shown if a sequence fails.
	log []string
}

// note records what the client does, to be shown if the sequence fails.
func (c *randomWindowClient) note(format string, args ...any) {
	c.log = append(c.log, fmt.Sprintf(format, args...))
}

// shows reports whether the client shows a window.
func (c *randomWindowClient) shows() bool { return c.inventory || c.menu != 0 }

// trackForms keeps the form the client was sent, and the windows it shows:
// not the ones it was sent while a form is open.
func (c *randomWindowClient) trackWithForms(pks []proto.Packet) {
	for _, pk := range pks {
		switch pk := pk.(type) {
		case *mc.ModalFormRequestPacket:
			c.form, c.formOpen = pk.FormID, true
			// A form hides the screen under it.
			c.inventory, c.menu = false, 0
		case *mc.ContainerOpenPacket:
			if c.formOpen {
				// The client shows no window while it shows a form.
				continue
			}
			c.track([]proto.Packet{pk})
		default:
			c.track([]proto.Packet{pk})
		}
	}
}

// act runs act, ticks the world and updates the client from what it was
// sent, which it takes from the connection once the session wrote it. It is
// gather without a marker and without decoding the packets that do not
// matter to windows: the sequences are long.
func (c *randomWindowClient) act(f func()) []proto.Packet {
	c.t.Helper()
	f()
	c.advance()
	deadline := time.Now().Add(5 * time.Second)
	for !c.sess.Idle() {
		if time.Now().After(deadline) {
			c.t.Fatal("the session did not write what it queued")
		}
		runtime.Gosched()
	}
	var out []proto.Packet
	for {
		select {
		case pk := <-c.conn.written:
			switch pk.(type) {
			case *mc.ContainerOpenPacket, *mc.ContainerClosePacket, *mc.ModalFormRequestPacket:
				out = append(out, pk)
			}
		default:
			c.trackWithForms(out)
			return out
		}
	}
}

func (c *randomWindowClient) sendClose(window byte, typ byte) {
	c.act(func() { c.send(&mc.ContainerClosePacket{WindowID: window, ContainerType: typ}) })
}

// closeShown closes what the client shows, as the client does: the close of
// the inventory is window 0, or 0xff when it was opened with the chat.
func (c *randomWindowClient) closeShown() {
	switch {
	case c.menu != 0:
		c.note("close the menu %d", c.menu)
		c.sendClose(c.menu, 0)
		c.menu = 0
	case c.inventory:
		if c.rng.IntN(4) == 0 {
			c.note("close the inventory with the 0xff of the chat")
			c.sendClose(0xff, 0xff)
		} else {
			c.note("close the inventory")
			c.sendClose(0, 0xff)
		}
		c.inventory = false
	}
}

// screenGoesAway is what a client does when the screen it shows goes away
// without a window being closed, as with a death or a change of dimension:
// it sends the close, unless it is lying.
func (c *randomWindowClient) screenGoesAway() {
	if c.lying && c.rng.IntN(2) == 0 {
		c.note("the screen went away with no close")
		c.inventory, c.menu = false, 0
		return
	}
	c.closeShown()
}

// stepOnce does a step chosen at random.
func (c *randomWindowClient) stepOnce() {
	t := c.t
	t.Helper()
	blocks := []cube.Pos{windowChest, windowOther, windowTable, windowFurnace}
	base := c.base
	switch n := c.rng.IntN(100); {
	case n < 12:
		// The client opens its inventory.
		if !c.shows() || (c.lying && c.inventory && c.rng.IntN(2) == 0) {
			c.note("open the inventory (shown: inventory %v menu %d)", c.inventory, c.menu)
			pks := c.act(func() {
				c.send(&mc.InteractPacket{ActionType: mc.InteractActionOpenInventory, TargetEntityRuntimeID: 1})
			})
			if c.inventory && len(opens(pks)) > 1 {
				t.Fatalf("the client was sent the ContainerOpen of its inventory twice: %v", summarise(pks))
			}
		}
	case n < 32:
		// The client clicks a block: a chest, a crafting table or a furnace.
		if !c.shows() {
			pos := blocks[c.rng.IntN(len(blocks))]
			c.note("click the block at %v", pos)
			c.act(func() { c.clickPacket(pos) })
		}
	case n < 52:
		c.closeShown()
	case n < 62:
		// A late close of a window the client does not show.
		window := byte(c.rng.IntN(100))
		if c.rng.IntN(5) == 0 {
			window = 0xff
		}
		if (window == c.menu && c.menu != 0) || (c.inventory && (window == 0 || window == 0xff)) {
			return
		}
		c.note("a late close of window %d", window)
		c.sendClose(window, 0)
	case n < 70:
		// The server closes the menu.
		c.note("the server closes the menu")
		c.act(func() { c.run(func(_ *world.Tx, p *player.Player) { p.CloseMenu(false) }) })
	case n < 76:
		c.note("the server teleports the player")
		c.act(func() {
			c.run(func(_ *world.Tx, p *player.Player) {
				// Within the chunk, so that none is generated, and within reach of the blocks.
				p.Teleport(base.Add([3]float64{float64(c.rng.IntN(5)), 0, float64(c.rng.IntN(5))}))
			})
		})
	case n < 82:
		// Death, then a respawn. The player that dies is removed 1.1 seconds
		// later by a timer that runs its transaction on a goroutine of its
		// own in the synchronous world of the scenario, where the ticks of
		// the test run too: the race detector sees them touch the entity at
		// once. In a world that ticks on its own, the timer queues.
		if raceEnabled {
			return
		}
		c.note("the player dies and respawns")
		c.act(func() { c.run(func(_ *world.Tx, p *player.Player) { p.Hurt(1000, entity.FreezeDamageSource{}) }) })
		c.screenGoesAway()
		c.advanceTicks(2)
		c.act(func() { c.run(func(_ *world.Tx, p *player.Player) { p.Respawn() }) })
		c.advanceTicks(2)
	case n < 86:
		if c.lying {
			// A form the client shows hides the windows it is sent.
			c.note("the server sends a form")
			c.act(func() {
				c.run(func(_ *world.Tx, p *player.Player) {
					p.SendForm(form.NewModal(goldenModal{Yes: form.NewButton("yes", ""), No: form.NewButton("no", "")}, "Form"))
				})
			})
		}
	case n < 90:
		if c.formOpen {
			c.note("the client closes the form")
			id := c.form
			c.act(func() {
				c.send(&mc.ModalFormResponsePacket{FormID: id, CancelReason: proto.Some[uint8](0)})
			})
			c.formOpen = false
		}
	case n < 95:
		// The server opens a chest for the player, as a command or a plugin does.
		c.note("the server opens a chest")
		c.act(func() {
			c.run(func(tx *world.Tx, p *player.Player) { p.OpenBlockContainer(windowOther, tx) })
		})
	default:
		ticks := c.rng.IntN(4)
		if c.rng.IntN(6) == 0 {
			ticks = afterGrace
		}
		c.note("%d ticks", ticks)
		c.advanceTicks(ticks)
	}
}

// advanceTicks advances the world n ticks, and updates the client from
// what it was sent.
func (c *randomWindowClient) advanceTicks(n int) {
	c.t.Helper()
	for range max(n-1, 0) {
		c.advance()
	}
	c.act(func() {})
}

// clickPacket sends the click of the client on the block at pos.
func (c *randomWindowClient) clickPacket(pos cube.Pos) {
	c.send(&mc.InventoryTransactionPacket{TransactionData: clickBlockData(pos)})
}

// TestWindowsRandomSequences runs seeded random sequences of 2000 steps
// of the windows of a client: it opens its inventory and chests, crafting
// tables and furnaces, closes them by the right and by other window IDs,
// has the server close its menu, teleports, dies and respawns, and shows
// forms. After each sequence the client closes what it shows; once the
// windows have had time to age, it must be sent a ContainerOpen when it
// opens its inventory, and when it clicks a chest.
//
// With lying off, the client is one that keeps to the protocol, and the
// session must answer it at once, without the grace. With it on, the client
// also leaves out the close of a screen that went away, and sends a repeat of
// its request to open the inventory, which a session may ignore, but only
// until the grace has passed.
func TestWindowsRandomSequences(t *testing.T) {
	for _, lying := range []bool{false, true} {
		for _, seed := range []uint64{1, 2} {
			t.Run(fmt.Sprintf("lying=%v/seed=%d", lying, seed), func(t *testing.T) {
				c := &randomWindowClient{windowsScenario: newWindowsScenario(t), rng: rand.New(rand.NewPCG(seed, 0xad)), lying: lying}
				c.run(func(tx *world.Tx, p *player.Player) {
					c.base = p.Position()
					tx.SetBlock(windowTable, block.CraftingTable{}, nil)
					tx.SetBlock(windowFurnace, block.NewFurnace(cube.North), nil)
				})
				defer func() {
					if t.Failed() {
						t.Logf("the last steps of the client:\n%v", c.log[max(0, len(c.log)-25):])
					}
				}()
				const steps = 2000
				for range steps {
					c.stepOnce()
				}
				// The client closes what it shows and the form, and the windows
				// age; the second pass is the one with nothing to wait for.
				if c.formOpen {
					id := c.form
					c.act(func() { c.send(&mc.ModalFormResponsePacket{FormID: id, CancelReason: proto.Some[uint8](0)}) })
					c.formOpen = false
				}
				c.closeShown()
				c.advanceTicks(afterGrace)
				c.wantInventory(fmt.Sprintf("after %d steps", steps))
				c.wantChest(fmt.Sprintf("after %d steps", steps), windowChest)
				c.wantInventory(fmt.Sprintf("once more after %d steps", steps))
				c.wantChest(fmt.Sprintf("once more after %d steps", steps), windowOther)
			})
		}
	}
}
