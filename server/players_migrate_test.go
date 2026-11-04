package server

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/playerdb"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/google/uuid"
)

// diamondsIn counts the diamonds in inv.
func diamondsIn(inv *inventory.Inventory) int {
	n := 0
	if inv == nil {
		return 0
	}
	for _, it := range inv.Slots() {
		if _, ok := it.Item().(item.Diamond); ok {
			n += it.Count()
		}
	}
	return n
}

// TestPlayersMigrateFromTheOldFolder checks that a server saving its world
// and its players, as config.toml does by default, keeps its players in the
// world database, reads those it has no record of there from the old
// players folder, and moves them at the next checkpoint, leaving the folder
// as it was: on a server keyed by name, a record by the UUID of the name
// and one by the UUID a client sent before; on one keyed by XUID, a record
// by the UUID of the login, whose XUID also seeds the known players.
func TestPlayersMigrateFromTheOldFolder(t *testing.T) {
	type record struct {
		login *clienttest.Join
		// key is what the old folder holds the record by, moved what the
		// world database holds it by after the move.
		key, moved uuid.UUID
		diamonds   int
	}
	carol := account("Carol", "2535412345678903")
	carolJoin := joinLogin("Carol", "")
	carolJoin.L.Identity = carol
	bob := joinLogin("Bob", "")
	for _, c := range []struct {
		mode    string
		records []record
	}{
		{"name", []record{
			{joinLogin("Alice", ""), identity.OfflineUUID("Alice"), identity.OfflineUUID("Alice"), 10},
			{bob, bob.L.Identity.UUID, identity.OfflineUUID("Bob"), 5},
		}},
		{"xuid", []record{{carolJoin, carol.UUID, carol.UUID, 3}}},
	} {
		t.Run(c.mode, func(t *testing.T) {
			dir := t.TempDir()
			w := world.Config{Synchronous: true}.New()
			old, err := playerdb.NewProvider(filepath.Join(dir, "players"))
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range c.records {
				id := r.login.L.Identity
				conf := player.Config{UUID: id.UUID, Name: id.Name, XUID: id.XUID, Inventory: inventory.New(36, nil), OffHand: inventory.New(1, nil),
					Armour: inventory.NewArmour(nil), EnderChestInventory: inventory.New(27, nil)}
				_ = conf.Inventory.SetItem(0, item.NewStack(item.Diamond{}, r.diamonds))
				if err := old.Save(r.key, conf, w); err != nil {
					t.Fatal(err)
				}
			}
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}
			_ = w.Close()

			uc := DefaultConfig()
			uc.World.Folder, uc.Players.Folder = filepath.Join(dir, "world"), filepath.Join(dir, "players")
			uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
			uc.Server.IdentityKey = c.mode
			conf, err := uc.Config(slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}
			db := conf.WorldProvider.(*mcdb.DB)
			inDB := playerdb.NewWorldProvider(db, nil)
			if _, ok := conf.PlayerProvider.(*playerdb.WorldProvider); !ok {
				t.Fatalf("the player provider is %T, want *playerdb.WorldProvider", conf.PlayerProvider)
			}
			if c.mode == "xuid" {
				known, err := os.ReadFile(filepath.Join(dir, "known-players.json"))
				if err != nil || !strings.Contains(string(known), carol.XUID) {
					t.Errorf("the known players seeded from the old folder are %s (%v), want Carol's XUID %s", known, err, carol.XUID)
				}
			}
			jt := newJoinTest(t, conf.PlayerProvider, conf)
			jt.start()
			for _, r := range c.records {
				h := jt.join(t, r.login)
				if err := player.Do(h, func(_ *world.Tx, p *player.Player) {
					if n := diamondsIn(p.Inventory()); n != r.diamonds {
						t.Errorf("%s joined with %d diamonds, want the %d of the old folder", p.Name(), n, r.diamonds)
					}
				}).Wait(context.Background()); err != nil {
					t.Fatal(err)
				}
				if _, _, err := inDB.Load(r.moved, func(world.Dimension) *world.World { return jt.srv.World() }); !errors.Is(err, leveldb.ErrNotFound) {
					t.Errorf("the world database holds a record of %v before the checkpoint: %v", r.moved, err)
				}
			}
			if err := jt.srv.SaveAll(); err != nil {
				t.Fatal(err)
			}
			for _, r := range c.records {
				d, _, err := inDB.Load(r.moved, func(world.Dimension) *world.World { return jt.srv.World() })
				if err != nil || diamondsIn(d.Inventory) != r.diamonds {
					t.Errorf("after the checkpoint the world database holds %v with %d diamonds (%v), want %d", r.moved, diamondsIn(d.Inventory), err, r.diamonds)
				}
			}
			if err := jt.srv.Close(); err != nil {
				t.Fatal(err)
			}

			old, err = playerdb.NewProvider(filepath.Join(dir, "players"))
			if err != nil {
				t.Fatal(err)
			}
			defer old.Close()
			listed := 0
			_ = old.Players(func(uuid.UUID, string, string) bool { listed++; return true })
			if listed != len(c.records) {
				t.Errorf("the old folder holds %d records after the server ran, want the %d it had", listed, len(c.records))
			}
		})
	}
}
