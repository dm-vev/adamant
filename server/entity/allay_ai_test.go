package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func allayOf(tx *world.Tx, h *world.EntityHandle) *entity.Allay {
	e, _ := h.Entity(tx)
	return e.(*entity.Allay)
}

// giveAllay has the player hand the allay the item they hold.
func giveAllay(t *testing.T, tx *world.Tx, a *entity.Allay, p *player.Player) {
	t.Helper()
	ctx := &item.UseContext{}
	if !a.AcceptItem(p, tx, ctx) {
		t.Fatal("allay did not take the item it was given")
	}
}

// itemsNear returns the count of the items of the kind passed lying within
// r blocks of pos.
func itemsNear(tx *world.Tx, pos mgl64.Vec3, r float64, it world.Item) int {
	n := 0
	name, _ := it.EncodeItem()
	for e := range tx.Entities() {
		i, ok := e.(*entity.Ent)
		if !ok || i.H().Type() != entity.ItemType || e.Position().Sub(pos).Len() > r {
			continue
		}
		if s := i.Behaviour().(*entity.ItemBehaviour).Item(); !s.Empty() {
			if got, _ := s.Item().EncodeItem(); got == name {
				n += s.Count()
			}
		}
	}
	return n
}

// TestAllaysBringItemsToTheirPlayer checks GoToWantedItem and
// GoAndGiveItemsToTarget: an allay given a diamond fetches the diamonds it
// sees and throws them to the player who gave it the first.
func TestAllaysBringItemsToTheirPlayer(t *testing.T) {
	w := wolfTestWorld(t)
	var allay *world.EntityHandle
	playerPos := mgl64.Vec3{.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, playerPos)
		p.SetHeldItems(item.NewStack(item.Diamond{}, 1), item.Stack{})
		allay = tx.AddEntity(entity.NewAllay(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 65, .5}})).H()
		giveAllay(t, tx, allayOf(tx, allay), p)
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{10.5, 64, .5}}, item.NewStack(item.Diamond{}, 3)))
	})
	for range 1200 {
		w.AdvanceTick()
		n := 0
		<-w.Exec(func(tx *world.Tx) { n = itemsNear(tx, playerPos, 4, item.Diamond{}) })
		if n == 3 {
			return
		}
	}
	t.Fatal("allay did not bring three diamonds eight blocks away to its player in a minute")
}

// TestAllaysDuplicateWhileDancing checks Allay.mobInteract and
// setJukeboxPlaying: an allay dancing to a jukebox given an amethyst shard
// makes another allay, after which it waits five minutes to do so again.
func TestAllaysDuplicateWhileDancing(t *testing.T) {
	w := wolfTestWorld(t)
	var allay *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{3, 64, 0}, block.Jukebox{}, nil)
		allay = tx.AddEntity(entity.NewAllay(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}})).H()
		a := allayOf(tx, allay)
		a.SetJukeboxPlaying(cube.Pos{3, 64, 0}, true)
		if !a.Dancing() {
			t.Fatal("allay three blocks from a jukebox that started playing does not dance")
		}
		p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 2.5})
		p.SetHeldItems(item.NewStack(item.AmethystShard{}, 2), item.Stack{})
		giveAllay(t, tx, a, p)
		if a.CanDuplicate() {
			t.Fatal("allay that duplicated may duplicate again at once")
		}
		allays := 0
		for e := range tx.Entities() {
			if _, ok := e.(*entity.Allay); ok {
				allays++
			}
		}
		if allays != 2 {
			t.Fatalf("%d allays after duplicating, want 2", allays)
		}
	})
}
