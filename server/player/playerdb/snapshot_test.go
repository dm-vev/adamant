package playerdb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSnapshotSavesAsData checks that a player's Snapshot is saved byte for
// byte as its Data is, but for the order of the keys in the NBT of items,
// which encoding a map does not keep from one call to the next.
func TestSnapshotSavesAsData(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.5, 64, 0.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Position: pos, Name: "Alice"})).(*player.Player)
		_ = p.Inventory().SetItem(4, item.NewStack(item.Diamond{}, 10))
		_ = p.EnderChestInventory().SetItem(26, item.NewStack(item.Apple{}, 3))
		p.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
		p.SetHeldItems(item.NewStack(item.Diamond{}, 1), item.NewStack(item.Arrow{}, 16))
		p.AddEffect(effect.New(effect.Speed, 2, time.Minute))
		p.SetExperienceLevel(7)

		var provider Provider
		data, snapshot := provider.toJson(p.Data(), w), provider.toJson(p.Snapshot(), w)
		gotItems, gotRest := splitItems(t, snapshot)
		wantItems, wantRest := splitItems(t, data)
		if !bytes.Equal(gotRest, wantRest) {
			t.Errorf("snapshot saves as\n%s\nwant, as the data,\n%s", gotRest, wantRest)
		}
		if !reflect.DeepEqual(gotItems, wantItems) {
			t.Errorf("snapshot saves the items\n%v\nwant, as the data,\n%v", gotItems, wantItems)
		}
		if len(wantItems) != 5 {
			t.Errorf("saved %d items, want the 5 given: %v", len(wantItems), wantItems)
		}
	})
}

// splitItems returns the JSON d is saved as, without its items, and the NBT
// of its items decoded, by where they are.
func splitItems(t *testing.T, d jsonData) (map[string]map[string]any, []byte) {
	t.Helper()
	items := make(map[string]map[string]any)
	add := func(where string, b []byte) {
		if b == nil {
			return
		}
		var m map[string]any
		if err := nbt.UnmarshalEncoding(b, &m, nbt.LittleEndian); err != nil {
			t.Fatalf("decode item NBT of %s: %v", where, err)
		}
		items[where] = m
	}
	for _, slot := range d.Inventory.Items {
		add(fmt.Sprintf("inventory %d", slot.Slot), slot.Item)
	}
	for _, slot := range d.EnderChestInventory {
		add(fmt.Sprintf("ender chest %d", slot.Slot), slot.Item)
	}
	inv := d.Inventory
	add("boots", inv.Boots)
	add("leggings", inv.Leggings)
	add("chestplate", inv.Chestplate)
	add("helmet", inv.Helmet)
	add("off hand", inv.OffHand)
	d.Inventory = jsonInventoryData{MainHandSlot: inv.MainHandSlot}
	d.EnderChestInventory = nil
	rest, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return items, rest
}
