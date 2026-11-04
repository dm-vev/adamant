package mcdb

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/google/uuid"
)

// TestPlayerSpawnPositionRoundTrip checks that the spawn point of a player
// is kept by the UUID of the player under player_<uuid> and
// player_server_<uuid>, as BDS keeps it, across a reopen of the database,
// and that another UUID has none.
func TestPlayerSpawnPositionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	steve, alex := uuid.MustParse("00000000-0000-4000-8000-00000000057e"), uuid.MustParse("00000000-0000-4000-8000-0000000000a1")
	if _, ok, err := db.LoadPlayerSpawnPosition(steve); ok || err != nil {
		t.Fatalf("spawn point of a new player: %t, %v", ok, err)
	}
	if err := db.SavePlayerSpawnPosition(steve, cube.Pos{10, 64, -20}); err != nil {
		t.Fatal(err)
	}
	if err := db.SavePlayerSpawnPosition(steve, cube.Pos{11, 65, -21}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"player_" + steve.String(), "player_server_" + steve.String()} {
		if ok, err := db.ldb.Has([]byte(key), nil); !ok || err != nil {
			t.Errorf("%s: %t, %v", key, ok, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if db, err = Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if pos, ok, err := db.LoadPlayerSpawnPosition(steve); !ok || err != nil || pos != (cube.Pos{11, 65, -21}) {
		t.Errorf("spawn point of Steve after a reopen: %v, %t, %v", pos, ok, err)
	}
	if _, ok, err := db.LoadPlayerSpawnPosition(alex); ok || err != nil {
		t.Errorf("spawn point of another player: %t, %v", ok, err)
	}
}
