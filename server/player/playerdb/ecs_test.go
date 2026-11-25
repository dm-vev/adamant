package playerdb

import (
	"testing"

	"github.com/df-mc/dragonfly/server/ecs"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

type playerDBStats struct {
	Level int32
	Title string
}

func TestPlayerECSComponentsPersistAcrossRestart(t *testing.T) {
	kStats := ecs.NewKey("playerdb_test:stats", ecs.WithCodec(
		func(s playerDBStats) map[string]any {
			return map[string]any{"level": s.Level, "title": s.Title}
		},
		func(m map[string]any) (playerDBStats, bool) {
			lv, ok1 := m["level"].(int32)
			title, ok2 := m["title"].(string)
			return playerDBStats{Level: lv, Title: title}, ok1 && ok2
		},
	))
	kTransient := ecs.NewKey[string]("playerdb_test:transient")

	dir := t.TempDir()
	prov, err := NewProvider(dir)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	t.Cleanup(func() { _ = w.Close() })

	id := uuid.New()
	var saved player.Config
	<-w.Exec(func(tx *world.Tx) {
		h := world.EntitySpawnOpts{ID: id}.New(player.Type, player.Config{UUID: id, Name: "Hero"})
		p := tx.AddEntity(h).(*player.Player)
		ecs.Set(p.Components(), kStats, playerDBStats{Level: 42, Title: "Vanguard"})
		ecs.Set(p.Components(), kTransient, "ephemeral")
		saved = p.Snapshot()
	})
	if err := prov.Save(id, saved, w); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := prov.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	prov2, err := NewProvider(dir)
	if err != nil {
		t.Fatalf("reopen NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = prov2.Close() })

	loadedCfg, _, err := prov2.Load(id, func(world.Dimension) *world.World { return w })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var got playerDBStats
	var gotOK, transientOK bool
	<-w.Exec(func(tx *world.Tx) {
		h := world.EntitySpawnOpts{ID: id}.New(player.Type, loadedCfg)
		p := tx.AddEntity(h).(*player.Player)
		got, gotOK = ecs.Get(p.Components(), kStats)
		_, transientOK = ecs.Get(p.Components(), kTransient)
	})
	if !gotOK || got.Level != 42 || got.Title != "Vanguard" {
		t.Fatalf("restored player ECS stats = (%+v, %v), want Level=42 Title=Vanguard", got, gotOK)
	}
	if transientOK {
		t.Fatal("transient component without codec should not persist")
	}
}
