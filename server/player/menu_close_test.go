package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
)

// TestMenuClosesWhenThePlayerDies checks that a menu does not outlive its
// player's death, as Java's ServerPlayer.die closes the container: a player
// that respawned near the chest would otherwise find the request to open it
// ignored as a menu open already.
func TestMenuClosesWhenThePlayerDies(t *testing.T) {
	withBreaker(t, anticheat.Default(), func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, tick func(int)) {
		chest := cube.Pos{1, 100, 0}
		tx.SetBlock(chest, block.NewChest(), nil)
		p.OpenBlockContainer(chest, tx)
		if p.Menu() == nil {
			t.Fatal("the chest did not open")
		}
		p.Hurt(1000, entity.FreezeDamageSource{})
		if p.Menu() != nil {
			t.Fatal("the menu is open after the player died")
		}
		closes := r.CallsTo("CloseMenu")
		if len(closes) != 1 || closes[0].Args[1] != false {
			t.Fatalf("closes %v, want one by the server", closes)
		}
	})
}

// TestMenuOpensAgainInTheWorldThePlayerArrivedIn checks that a menu opened in
// another world does not make the request to open the chest at the same
// position ignored as a menu open already: a player that travelled through a
// portal with a chest open, and clicks the chest at the same coordinates.
func TestMenuOpensAgainInTheWorldThePlayerArrivedIn(t *testing.T) {
	w, _ := clientTestWorld(t)
	other, _ := clientTestWorld(t)
	r := clienttest.New()
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		chest := cube.Pos{1, 100, 0}
		tx.SetBlock(chest, block.NewChest(), nil)
		p.OpenBlockContainer(chest, tx)
		if p.Menu() == nil {
			t.Fatal("the chest did not open")
		}
		// The menu was opened in the other world: the player has arrived in
		// this one since.
		p.menuWorld = other
		p.OpenBlockContainer(chest, tx)
		if got := len(r.CallsTo("OpenMenu")); got != 2 {
			t.Fatalf("the chest opened %d times, want 2: the request in the new world was ignored", got)
		}
		closes := r.CallsTo("CloseMenu")
		if len(closes) != 1 || closes[0].Args[1] != false {
			t.Fatalf("closes %v, want the menu of the other world closed by the server", closes)
		}
	})
}

// TestMenuClosesWhenThePlayerIsTransferred checks that a player taken to
// another world, as a teleport command run in another dimension does, has its
// menu closed in the world it leaves: the block it was open for is not the
// one at its position in the world it arrives in.
func TestMenuClosesWhenThePlayerIsTransferred(t *testing.T) {
	w, _ := clientTestWorld(t)
	other, _ := clientTestWorld(t)
	r := clienttest.New()
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		chest := cube.Pos{1, 100, 0}
		tx.SetBlock(chest, block.NewChest(), nil)
		p.OpenBlockContainer(chest, tx)
		if p.Menu() == nil {
			t.Fatal("the chest did not open")
		}
		p.TeleportToWorld(tx, other, p.Position())
		if p.Menu() != nil {
			t.Fatal("the menu is open after the player was transferred")
		}
		closes := r.CallsTo("CloseMenu")
		if len(closes) != 1 || closes[0].Args[1] != false {
			t.Fatalf("closes %v, want one by the server", closes)
		}
	})
}
