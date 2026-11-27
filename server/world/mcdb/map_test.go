package mcdb_test

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/df-mc/goleveldb/leveldb"
	_ "github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/mcdb"
)

func TestIndependentMapTransitionsPersist(t *testing.T) {
	dir := t.TempDir()
	db, err := (mcdb.Config{}).Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	over := world.Config{Provider: db, Synchronous: true}.New()
	nether := world.Config{Provider: db, Dim: world.Nether, Synchronous: true}.New()
	t.Cleanup(func() { _ = nether.Close(); _ = over.Close() })
	source, err := over.CreateMap(cube.Pos{-130, 64, 257}, 1)
	if err != nil {
		t.Fatal(err)
	}
	source.Colors[317] = 5
	source.Tracking, source.UnlimitedTracking = false, true
	source.Decorations = []world.MapDecoration{{Type: 1, X: 7, Z: -9, Rotation: 3}}
	if err := over.StoreMapData(source); err != nil {
		t.Fatal(err)
	}
	scaled, err := nether.ScaleMap(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if scaled.Dimension != source.Dimension || scaled.Scale != 2 || scaled.CenterX != -320 || scaled.CenterZ != 192 || scaled.Tracking || !scaled.UnlimitedTracking || len(scaled.Decorations) != 0 || !bytes.Equal(scaled.Colors, make([]byte, 16384)) {
		t.Fatal("scaled map did not retain source dimension/tracking with a blank aligned coarser grid")
	}
	locked, err := nether.LockMap(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := over.CloneMap(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !locked.Locked || clone.Locked || !bytes.Equal(locked.Colors, source.Colors) || !bytes.Equal(clone.Colors, source.Colors) {
		t.Fatal("copy transitions changed explored or transparent pixels")
	}
	maps := []world.MapData{source, scaled, locked, clone}
	nativeIDs, stateIDs := map[int64]bool{}, map[int32]bool{}
	for _, m := range maps {
		if !m.HasCanonicalID || nativeIDs[m.ID] || stateIDs[m.CanonicalID] {
			t.Fatalf("duplicate map identity native=%d java=%d", m.ID, m.CanonicalID)
		}
		nativeIDs[m.ID], stateIDs[m.CanonicalID] = true, true
	}
	if err := nether.Close(); err != nil {
		t.Fatal(err)
	}
	if err := over.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = (mcdb.Config{}).Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	reopened := world.Config{Provider: db, Synchronous: true}.New()
	t.Cleanup(func() { _ = reopened.Close() })
	for _, want := range maps {
		got, err := reopened.MapData(want.ID)
		if err != nil {
			t.Fatal(err)
		}
		native, err := db.NativeMapID(want.CanonicalID)
		if err != nil {
			t.Fatal(err)
		}
		if native != want.ID || got.CanonicalID != want.CanonicalID || got.Locked != want.Locked || got.Dimension != want.Dimension || got.Scale != want.Scale || got.CenterX != want.CenterX || got.CenterZ != want.CenterZ || got.Tracking != want.Tracking || got.UnlimitedTracking != want.UnlimitedTracking || !bytes.Equal(got.Colors, want.Colors) || len(got.Decorations) != len(want.Decorations) {
			t.Fatalf("map %d changed across disk reopen", want.ID)
		}
		if got.NativeColors[3] != 0 {
			t.Fatalf("map %d lost transparent background", want.ID)
		}
		if len(got.Decorations) != 0 {
			got, want := got.Decorations[0], want.Decorations[0]
			if got.Type != want.Type || got.X != want.X || got.Z != want.Z || got.Rotation != want.Rotation {
				t.Fatal("map decoration changed across disk reopen")
			}
		}
	}
}

func TestLegacyMapKeysFallbackAndMigrate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := (mcdb.Config{}).Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ldb, err := leveldb.OpenFile(filepath.Join(dir, "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var nativeBuf [8]byte
	var canonBuf [4]byte
	var nativeIDVal int64 = -42
	binary.LittleEndian.PutUint64(nativeBuf[:], uint64(nativeIDVal))
	binary.LittleEndian.PutUint32(canonBuf[:], 7)
	var nextBuf [8]byte
	binary.LittleEndian.PutUint64(nextBuf[:], 8)
	_ = ldb.Put([]byte("adamant:map_java_7"), nativeBuf[:], nil)
	_ = ldb.Put([]byte("adamant:map_native_java_-42"), canonBuf[:], nil)
	_ = ldb.Put([]byte("adamant:map_next_java_id"), nextBuf[:], nil)
	_ = ldb.Close()

	db, err = (mcdb.Config{}).Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	nativeID, err := db.NativeMapID(7)
	if err != nil || nativeID != -42 {
		t.Fatalf("NativeMapID(7) = %d, %v; want -42", nativeID, err)
	}
	canonID, err := db.CanonicalMapID(-42)
	if err != nil || canonID != 7 {
		t.Fatalf("JavaMapID(-42) = %d, %v; want 7", canonID, err)
	}
	nextID, err := db.CanonicalMapID(-99)
	if err != nil || nextID != 8 {
		t.Fatalf("next JavaMapID(-99) = %d, %v; want 8", nextID, err)
	}
}
