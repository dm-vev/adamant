package mcdb

import (
	"encoding/hex"
	"errors"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

const (
	vanillaPositionTrackLast = "0a000008020069640a003078303030303030326101070076657273696f6e0100"
	vanillaPositionTrack42   = "0a000003030064696d0000000008020069640a0030783030303030303261090300706f7303030000000400000041000000faffffff0106007374617475730001070076657273696f6e0100"
)

func TestVanillaPositionTrackingImportExport(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ldb.Put([]byte(keyPositionTrackLast), decodeHex(t, vanillaPositionTrackLast), nil); err != nil {
		t.Fatal(err)
	}
	if err := db.ldb.Put(positionTrackKey(42), decodeHex(t, vanillaPositionTrack42), nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Synchronous: true}.New()
	want := cube.Pos{4, 65, -6}
	if got, dim, ok := w.TrackedPosition(42); !ok || got != want || dim != 0 {
		t.Fatalf("imported target = %v, %d, %v; want %v, 0, true", got, dim, ok, want)
	}
	newPos := cube.Pos{8, 70, 9}
	if handle := w.TrackPosition(newPos, 0); handle != 43 {
		t.Fatalf("new handle = %d, want 43", handle)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lastBytes, err := db.ldb.Get([]byte(keyPositionTrackLast), nil)
	if err != nil {
		t.Fatal(err)
	}
	var last positionTrackLastID
	if err := nbt.UnmarshalEncoding(lastBytes, &last, nbt.LittleEndian); err != nil {
		t.Fatal(err)
	}
	if last.ID != "0x0000002b" || last.Version != 1 {
		t.Fatalf("exported last ID = %#v", last)
	}
	entryBytes, err := db.ldb.Get(positionTrackKey(43), nil)
	if err != nil {
		t.Fatal(err)
	}
	var entry positionTrackEntry
	if err := nbt.UnmarshalEncoding(entryBytes, &entry, nbt.LittleEndian); err != nil {
		t.Fatal(err)
	}
	if entry.ID != "0x0000002b" || entry.Dimension != 0 || entry.Status != 0 || len(entry.Position) != 3 || entry.Position[0] != 8 || entry.Position[1] != 70 || entry.Position[2] != 9 {
		t.Fatalf("exported entry = %#v", entry)
	}
	roundTrip := world.Config{Provider: db, Synchronous: true}.New()
	if got, _, ok := roundTrip.TrackedPosition(42); !ok || got != want {
		t.Fatalf("round-trip imported target = %v, %v; want %v, true", got, ok, want)
	}
	if got, _, ok := roundTrip.TrackedPosition(43); !ok || got != newPos {
		t.Fatalf("round-trip Adamant target = %v, %v; want %v, true", got, ok, newPos)
	}
	if err := roundTrip.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPositionTrackingExportRemovesPrunedEntries(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := nbt.MarshalEncoding(positionTrackEntry{ID: "0x00000001", Position: []int32{1, 2, 3}, Version: 1}, nbt.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ldb.Put(positionTrackKey(1), stale, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.SavePositionTrackingData(world.PositionTrackingData{Next: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ldb.Get(positionTrackKey(1), nil); !errors.Is(err, leveldb.ErrNotFound) {
		t.Fatalf("pruned key lookup error = %v, want leveldb.ErrNotFound", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func decodeHex(t *testing.T, value string) []byte {
	t.Helper()
	b, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
