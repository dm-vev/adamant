package mcdb_test

import (
	"bytes"
	"testing"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
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
	nativeIDs, javaIDs := map[int64]bool{}, map[int32]bool{}
	for _, m := range maps {
		if !m.HasJavaID || nativeIDs[m.ID] || javaIDs[m.JavaID] {
			t.Fatalf("duplicate map identity native=%d java=%d", m.ID, m.JavaID)
		}
		nativeIDs[m.ID], javaIDs[m.JavaID] = true, true
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
		native, err := db.NativeMapID(want.JavaID)
		if err != nil {
			t.Fatal(err)
		}
		if native != want.ID || got.JavaID != want.JavaID || got.Locked != want.Locked || got.Dimension != want.Dimension || got.Scale != want.Scale || got.CenterX != want.CenterX || got.CenterZ != want.CenterZ || got.Tracking != want.Tracking || got.UnlimitedTracking != want.UnlimitedTracking || !bytes.Equal(got.Colors, want.Colors) || len(got.Decorations) != len(want.Decorations) {
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
