package playerdb

import (
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// TestProviderListsItsPlayers checks that the records the provider saves
// hold the XUID of the player, and that Players lists each record with its
// name and XUID, across a reopen.
func TestProviderListsItsPlayers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "players")
	p, err := NewProvider(path)
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Dim: world.Overworld}.New()
	t.Cleanup(func() { _ = w.Close() })
	steve, alex := uuid.New(), uuid.New()
	for id, c := range map[uuid.UUID]struct{ name, xuid string }{steve: {"Steve", "2535412345678901"}, alex: {"Alex", ""}} {
		conf := player.Config{UUID: id, Name: c.name, XUID: c.xuid, Inventory: inventory.New(36, nil), OffHand: inventory.New(1, nil),
			Armour: inventory.NewArmour(nil), EnderChestInventory: inventory.New(27, nil)}
		if err := p.Save(id, conf, w); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if p, err = NewProvider(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	got := map[uuid.UUID]string{}
	if err := p.Players(func(id uuid.UUID, name, xuid string) bool {
		got[id] = name + " " + xuid
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[steve] != "Steve 2535412345678901" || got[alex] != "Alex " {
		t.Errorf("listed %v", got)
	}
	conf, _, err := p.Load(steve, func(world.Dimension) *world.World { return w })
	if err != nil || conf.XUID != "2535412345678901" {
		t.Errorf("the record of Steve loads with XUID %q, %v", conf.XUID, err)
	}
}
