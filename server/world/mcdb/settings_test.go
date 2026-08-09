package mcdb

import (
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestFallDamageSettingPersists(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !db.Settings().Snapshot().FallDamage {
		t.Fatal("new database has fall damage disabled")
	}
	settings := db.Settings().Snapshot()
	settings.FallDamage = false
	db.SaveSettings(settings)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if db.Settings().Snapshot().FallDamage {
		t.Fatal("fall damage setting was not persisted")
	}
}

func TestConcurrentSaveSettingsAndSetFallDamage(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Synchronous: true, Provider: db}.New()

	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		for i := range 1000 {
			w.SetFallDamage(i%2 == 0)
		}
	}()
	go func() {
		defer wait.Done()
		for range 1000 {
			db.SaveSettings(db.Settings())
		}
	}()
	wait.Wait()
	w.SetFallDamage(false)
	w.Save()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if db.Settings().Snapshot().FallDamage {
		t.Fatal("concurrently saved fall damage setting was not persisted")
	}
}
