package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPlayerSnapshotIsDetached checks that the data Snapshot returns holds
// what Data does, in inventories of its own: changing the inventories of the
// player afterwards leaves the snapshot as it was.
func TestPlayerSnapshotIsDetached(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.5, 64, 0.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos})).(*Player)
		diamonds, apple, arrows := item.NewStack(item.Diamond{}, 10), item.NewStack(item.Apple{}, 3), item.NewStack(item.Arrow{}, 16)
		helmet := item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1)
		_ = p.Inventory().SetItem(4, diamonds)
		_ = p.offHand.SetItem(0, arrows)
		p.Armour().SetHelmet(helmet)
		_ = p.EnderChestInventory().SetItem(26, diamonds)

		snap := p.Snapshot()
		inventories := []struct {
			name       string
			live, snap *inventory.Inventory
		}{
			{"inventory", p.Inventory(), snap.Inventory},
			{"off hand", p.offHand, snap.OffHand},
			{"armour", p.Armour().Inventory(), snap.Armour.Inventory()},
			{"ender chest", p.EnderChestInventory(), snap.EnderChestInventory},
		}
		for _, inv := range inventories {
			if inv.snap == inv.live {
				t.Errorf("snapshot holds the live %s", inv.name)
			}
			if inv.snap.Size() != inv.live.Size() || !equalStacks(inv.snap.Slots(), inv.live.Slots()) {
				t.Errorf("snapshot %s = %v, want %v", inv.name, inv.snap.Slots(), inv.live.Slots())
			}
		}
		before := make([][]item.Stack, len(inventories))
		for i, inv := range inventories {
			before[i] = inv.snap.Slots()
		}

		// The player goes on playing after the snapshot.
		_ = p.Inventory().SetItem(4, item.Stack{})
		_, _ = p.Inventory().AddItem(apple)
		_ = p.offHand.SetItem(0, item.Stack{})
		p.Armour().SetHelmet(item.Stack{})
		_ = p.EnderChestInventory().SetItem(0, apple)
		for i, inv := range inventories {
			if !equalStacks(inv.snap.Slots(), before[i]) {
				t.Errorf("snapshot %s changed with the player: %v, want %v", inv.name, inv.snap.Slots(), before[i])
			}
		}
		if helmet := snap.Armour.Helmet(); helmet.Count() != 1 {
			t.Errorf("snapshot helmet = %v, want the iron helmet", helmet)
		}
		if held, _ := snap.OffHand.Item(0); held.Count() != 16 {
			t.Errorf("snapshot off hand = %v, want 16 arrows", held)
		}
	})
}

func equalStacks(a, b []item.Stack) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}
