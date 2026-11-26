package mcdb_test

import (
	"testing"

	"github.com/dm-vev/adamant/server/ecs"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/mcdb"
)

type claimData struct {
	Owner string
	Tier  int32
}

type worldMeta struct {
	Season int32
	Tag    string
}

func TestChunkAndWorldECSComponentsPersistAcrossRestart(t *testing.T) {
	kClaim := ecs.NewKey("mcdb_test:chunk_claim", ecs.WithCodec(
		func(c claimData) map[string]any {
			return map[string]any{"owner": c.Owner, "tier": c.Tier}
		},
		func(m map[string]any) (claimData, bool) {
			owner, ok1 := m["owner"].(string)
			tier, ok2 := m["tier"].(int32)
			return claimData{Owner: owner, Tier: tier}, ok1 && ok2
		},
	))
	kMeta := ecs.NewKey("mcdb_test:world_meta", ecs.WithCodec(
		func(wm worldMeta) map[string]any {
			return map[string]any{"season": wm.Season, "tag": wm.Tag}
		},
		func(m map[string]any) (worldMeta, bool) {
			season, ok1 := m["season"].(int32)
			tag, ok2 := m["tag"].(string)
			return worldMeta{Season: season, Tag: tag}, ok1 && ok2
		},
	))

	dir := t.TempDir()
	db, err := mcdb.Open(dir)
	if err != nil {
		t.Fatalf("mcdb.Open: %v", err)
	}
	w := world.Config{
		Synchronous:     true,
		RandomTickSpeed: -1,
		Generator:       world.NopGenerator{},
		Provider:        db,
	}.New()

	pos := world.ChunkPos{3, -2}
	ecs.Set(w.Components(), kMeta, worldMeta{Season: 5, Tag: "beta"})
	<-w.Exec(func(tx *world.Tx) {
		bag := tx.ChunkComponents(pos)
		ecs.Set(bag, kClaim, claimData{Owner: "guild-alpha", Tier: 3})
	})
	w.Save()
	if err := w.Close(); err != nil {
		t.Fatalf("world Close: %v", err)
	}

	db2, err := mcdb.Open(dir)
	if err != nil {
		t.Fatalf("reopen mcdb.Open: %v", err)
	}
	w2 := world.Config{
		Synchronous:     true,
		RandomTickSpeed: -1,
		Generator:       world.NopGenerator{},
		Provider:        db2,
	}.New()
	t.Cleanup(func() { _ = w2.Close() })

	gotMeta, ok := ecs.Get(w2.Components(), kMeta)
	if !ok || gotMeta.Season != 5 || gotMeta.Tag != "beta" {
		t.Fatalf("restored world ECS meta = (%+v, %v), want Season=5 Tag=beta", gotMeta, ok)
	}

	var gotClaim claimData
	var claimOK bool
	<-w2.Exec(func(tx *world.Tx) {
		gotClaim, claimOK = ecs.Get(tx.ChunkComponents(pos), kClaim)
	})
	if !claimOK || gotClaim.Owner != "guild-alpha" || gotClaim.Tier != 3 {
		t.Fatalf("restored chunk ECS claim = (%+v, %v), want Owner=guild-alpha Tier=3", gotClaim, claimOK)
	}

	// Verify deleting chunk & world components persists deletion across restart.
	ecs.Delete(w2.Components(), kMeta)
	<-w2.Exec(func(tx *world.Tx) {
		ecs.Delete(tx.ChunkComponents(pos), kClaim)
	})
	w2.Save()
	if err := w2.Close(); err != nil {
		t.Fatalf("world2 Close: %v", err)
	}

	db3, err := mcdb.Open(dir)
	if err != nil {
		t.Fatalf("reopen3 mcdb.Open: %v", err)
	}
	w3 := world.Config{
		Synchronous:     true,
		RandomTickSpeed: -1,
		Generator:       world.NopGenerator{},
		Provider:        db3,
	}.New()
	t.Cleanup(func() { _ = w3.Close() })

	if _, ok := ecs.Get(w3.Components(), kMeta); ok {
		t.Fatal("deleted world ECS component should not reappear after restart")
	}
	<-w3.Exec(func(tx *world.Tx) {
		if _, ok := ecs.Get(tx.ChunkComponents(pos), kClaim); ok {
			t.Fatal("deleted chunk ECS component should not reappear after restart")
		}
	})
}
