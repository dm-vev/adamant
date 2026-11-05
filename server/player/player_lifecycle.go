package player

import (
	"net"
	"time"

	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
)

// Transfer transfers the player to a server at the address passed. If the address could not be resolved, an
// error is returned. If it is returned, the player is closed and transferred to the server.
func (p *Player) Transfer(address string) error {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return err
	}

	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleTransfer(ctx, addr); ctx.Cancelled() {
		return nil
	}
	p.client().Transfer(addr.IP, addr.Port)
	return nil
}

// transferRetryDelay is how long a respawn or a teleport to another world
// waits before trying again while a checkpoint holds transfers: a tick.
const transferRetryDelay = time.Second / 20

// Disconnect closes the player and removes it from the world.
// Disconnect, unlike Close, allows a custom message to be passed to show to the player when it is
// disconnected. The message is formatted following the rules of fmt.Sprintln without a newline at the end and
// read as a legacy string, with § codes; a lone text.Text is shown as it is.
// The first call removes the player from its current world before returning.
// Calls made after teardown starts return without waiting, including calls from
// quit callbacks. The player must not be used afterwards. If the player is dead,
// it first respawns: teardown completes on the owner of its respawn world.
func (p *Player) Disconnect(msg ...any) {
	if p.closing.CompareAndSwap(false, true) {
		p.close(formatText(msg))
	}
}

// Close closes the player and removes it from the world.
// Close disconnects the player with a 'Connection closed.' message. Disconnect should be used to disconnect a
// player with a custom message.
// The first call removes the player from its current world before returning.
// Calls made after teardown starts return without waiting, including calls from
// quit callbacks. The player must not be used afterwards. If the player is dead,
// it first respawns: teardown completes on the owner of its respawn world.
func (p *Player) Close() error {
	if p.closing.CompareAndSwap(false, true) {
		p.close(text.Literal("Connection closed."))
	}
	return nil
}

// close closes the player without disconnecting it. It executes code shared by both the closing and the
// disconnecting of players.
func (p *Player) close(msg text.Text) {
	// If the player is being disconnected while they are dead, we respawn the player
	// so that the player logic works correctly the next time they join. This
	// asks whether a client exists that can respawn the player: respawn does
	// nothing without a client, so a dead player without one quits directly
	// instead of staying in the world.
	if p.Dead() && p.hasClient() {
		p.respawn(func(np *Player) {
			np.quit(msg)
		})
		return
	}
	p.quit(msg)
}

func (p *Player) quit(msg text.Text) {
	p.StopFishing(p.tx, false)
	p.h.HandleQuit(p)
	p.h = NopHandler{}

	if c := p.c; c != nil {
		c.Disconnect(msg)
		c.CloseConnection()
		// Leave on this owner directly: teardown must not depend on the
		// goroutine of the client, which cannot schedule work anymore once
		// the world starts closing.
		p.leave(p.tx)
		return
	}
	// A player without a client leaves no session: it has no quit message,
	// no Left hook and keeps its inventories.
	p.tx.RemoveEntity(p)
	_ = p.handle.Close()
}

// Leave ends the session of the player with its client in tx, as quitting
// does after its handler ran, unless it has ended. tx is the transaction
// of the world the player is in, nil if it is in none. An adapter calls it
// when its client went away and the player did not quit at once, such as
// when the quit handler panicked.
func (p *Player) Leave(tx *world.Tx) { p.leave(tx) }

// leave ends the session of the player with its client in tx, once: in
// this order, it moves the items of its crafting grid and cursor into its
// inventory and closes its menu (if tx is not nil), calls its Left hook,
// clears its inventories, writes its quit message, unsubscribes it from
// the chat, removes it from the world of tx (if tx is not nil), closes its
// client and, last, closes its entity handle. Each step runs if an earlier
// one panics, after which the first panic propagates.
func (p *Player) leave(tx *world.Tx) {
	if !p.left.CompareAndSwap(false, true) {
		return
	}
	var panicValue any
	run := func(f func()) {
		defer func() {
			if r := recover(); r != nil && panicValue == nil {
				panicValue = r
			}
		}()
		f()
	}
	if tx != nil {
		run(p.MoveItemsToInventory)
		run(func() { p.closeMenu(tx, false) })
	}
	if p.leftHook != nil {
		run(func() { p.leftHook(tx, p) })
	}
	// Clear the inventories so that they no longer hold references to the
	// client.
	run(func() { _ = p.inv.Close() })
	run(func() { _ = p.offHand.Close() })
	run(func() { _ = p.armour.Close() })
	if !p.quitMessage.Zero() {
		run(func() { chat.Global.Writet(p.quitMessage, p.Name()) })
	}
	run(func() { chat.Global.Unsubscribe(p) })
	// Remove the player before its client closes its chunk loader: closing
	// the last loader may unload the chunk and close its remaining entities.
	if tx != nil {
		run(func() { tx.RemoveEntity(p) })
	}
	run(func() { p.client().Close(tx) })
	run(func() { _ = p.handle.Close() })
	if panicValue != nil {
		panic(panicValue)
	}
}
