package mcdb

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb/leveldat"
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

func TestWorldSeedSurvivesRestartAndConfigChange(t *testing.T) {
	for _, seed := range []int64{0, -1 << 63, 1<<63 - 1} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			dir := t.TempDir()
			db, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			w := world.Config{Synchronous: true, Provider: db, Seed: seed}.New()
			w.SetDefaultGameMode(world.GameModeAdventure)
			w.SetDifficulty(world.DifficultyHard)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}

			ldat, err := leveldat.ReadFile(filepath.Join(dir, "level.dat"))
			if err != nil {
				t.Fatal(err)
			}
			var saved leveldat.Data
			if err := ldat.Unmarshal(&saved); err != nil {
				t.Fatal(err)
			}
			if saved.RandomSeed != seed {
				t.Fatalf("saved RandomSeed = %d, want %d", saved.RandomSeed, seed)
			}
			db, err = Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			w = world.Config{Synchronous: true, Provider: db, Seed: seed ^ 12345}.New()
			t.Cleanup(func() { _ = w.Close() })
			if w.Seed() != seed {
				t.Errorf("configuration replaced saved seed: got %d, want %d", w.Seed(), seed)
			}
			if w.DefaultGameMode() != world.GameModeAdventure || w.Difficulty() != world.DifficultyHard {
				t.Errorf("saved mode/difficulty changed: %v/%v", w.DefaultGameMode(), w.Difficulty())
			}
		})
	}
}
