package playerdb_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/playerdb"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/google/uuid"
)

// playerConf returns the data of a player of the name and XUID passed,
// holding n diamonds.
func playerConf(id uuid.UUID, name, xuid string, n int) player.Config {
	conf := player.Config{UUID: id, Name: name, XUID: xuid, Inventory: inventory.New(36, nil), OffHand: inventory.New(1, nil),
		Armour: inventory.NewArmour(nil), EnderChestInventory: inventory.New(27, nil)}
	if n > 0 {
		_ = conf.Inventory.SetItem(0, item.NewStack(item.Diamond{}, n))
	}
	return conf
}

// diamondsOf counts the diamonds in the inventory of conf.
func diamondsOf(conf player.Config) int {
	n := 0
	if conf.Inventory == nil {
		return 0
	}
	for _, it := range conf.Inventory.Slots() {
		if _, ok := it.Item().(item.Diamond); ok {
			n += it.Count()
		}
	}
	return n
}

// TestPlayersMigrateFromTheOldFolder checks that the players of a world
// database (mcdb) load from the old players folder when the database has no
// record of them, under whatever key the folder holds them by, that their
// next save writes them into the database and leaves the folder as it was,
// and that the database then loads them without the folder.
func TestPlayersMigrateFromTheOldFolder(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	dir := t.TempDir()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	dim := func(world.Dimension) *world.World { return w }

	// The folder keyed the players by the UUID of their name on a server
	// without sign-in, by the UUID the client sent before that, and by the
	// UUID of the login on a server with sign-in: they are all UUIDs.
	alice, bob, carol := uuid.New(), uuid.New(), uuid.New()
	old, err := playerdb.NewProvider(filepath.Join(dir, "players"))
	if err != nil {
		t.Fatal(err)
	}
	for id, c := range map[uuid.UUID]player.Config{alice: playerConf(alice, "Alice", "", 10), bob: playerConf(bob, "Bob", "", 5), carol: playerConf(carol, "Carol", "2535412345678903", 3)} {
		if err := old.Save(id, c, w); err != nil {
			t.Fatal(err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := mcdb.Open(filepath.Join(dir, "world"))
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := playerdb.OpenReadOnly(filepath.Join(dir, "players"))
	if err != nil {
		t.Fatal(err)
	}
	players := playerdb.NewWorldProvider(db, fallback)
	for id, want := range map[uuid.UUID]int{alice: 10, bob: 5, carol: 3} {
		conf, _, err := players.Load(id, dim)
		if err != nil || diamondsOf(conf) != want {
			t.Errorf("player %v loads from the old folder with %d diamonds (%v), want %d", id, diamondsOf(conf), err, want)
		}
	}
	if _, _, err := players.Load(uuid.New(), dim); !errors.Is(err, leveldb.ErrNotFound) {
		t.Errorf("a player held nowhere loads with %v, want leveldb.ErrNotFound", err)
	}
	// Alice plays and is saved: her record moves into the database.
	if err := players.Save(alice, playerConf(alice, "Alice", "", 7), w); err != nil {
		t.Fatal(err)
	}
	if err := fallback.Save(alice, playerConf(alice, "Alice", "", 1), w); err == nil {
		t.Error("the old folder, opened read-only, took a save")
	}
	if err := players.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = mcdb.Open(filepath.Join(dir, "world"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if conf, _, err := playerdb.NewWorldProvider(db, nil).Load(alice, dim); err != nil || diamondsOf(conf) != 7 {
		t.Errorf("Alice loads from the world database with %d diamonds (%v), want 7", diamondsOf(conf), err)
	}
	if _, _, err := playerdb.NewWorldProvider(db, nil).Load(bob, dim); !errors.Is(err, leveldb.ErrNotFound) {
		t.Errorf("Bob, never saved since, loads from the world database without the folder with %v, want leveldb.ErrNotFound", err)
	}
	old, err = playerdb.NewProvider(filepath.Join(dir, "players"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Close() })
	if conf, _, err := old.Load(alice, dim); err != nil || diamondsOf(conf) != 10 {
		t.Errorf("the old folder holds Alice with %d diamonds (%v), want the 10 she had there", diamondsOf(conf), err)
	}
}

// TestWorldProviderListsItsPlayers checks that the players of a world
// database (mcdb) list each record with its name and XUID, those of the database and those
// of the old folder without one there, each once, across a reopen, as
// TestProviderListsItsPlayers checks Provider lists its own.
func TestWorldProviderListsItsPlayers(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	dir := t.TempDir()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	steve, alex, moved := uuid.New(), uuid.New(), uuid.New()
	old, err := playerdb.NewProvider(filepath.Join(dir, "players"))
	if err != nil {
		t.Fatal(err)
	}
	for id, c := range map[uuid.UUID]player.Config{alex: playerConf(alex, "Alex", "", 0), moved: playerConf(moved, "Old", "2535412345678909", 0)} {
		if err := old.Save(id, c, w); err != nil {
			t.Fatal(err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := mcdb.Open(filepath.Join(dir, "world"))
	if err != nil {
		t.Fatal(err)
	}
	for id, c := range map[uuid.UUID]player.Config{steve: playerConf(steve, "Steve", "2535412345678901", 0), moved: playerConf(moved, "Moved", "2535412345678909", 0)} {
		if err := playerdb.NewWorldProvider(db, nil).Save(id, c, w); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if db, err = mcdb.Open(filepath.Join(dir, "world")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fallback, err := playerdb.OpenReadOnly(filepath.Join(dir, "players"))
	if err != nil {
		t.Fatal(err)
	}
	players := playerdb.NewWorldProvider(db, fallback)
	t.Cleanup(func() { _ = players.Close() })
	got := map[uuid.UUID]string{}
	n := 0
	if err := players.Players(func(id uuid.UUID, name, xuid string) bool {
		got[id] = name + " " + xuid
		n++
		return true
	}); err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID]string{steve: "Steve 2535412345678901", alex: "Alex ", moved: "Moved 2535412345678909"}
	if n != len(want) || len(got) != len(want) {
		t.Errorf("listed %d records %v, want %v", n, got, want)
	}
	for id, s := range want {
		if got[id] != s {
			t.Errorf("listed %v as %q, want %q", id, got[id], s)
		}
	}
	n = 0
	_ = players.Players(func(uuid.UUID, string, string) bool { n++; return false })
	if n != 1 {
		t.Errorf("listing went on %d times after yield returned false, want 1", n)
	}
}
