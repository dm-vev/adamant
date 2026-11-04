package mcdb

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb/leveldat"
)

// createWorld creates a world in dir with a seed and settings that differ
// from those of a new world, closing it twice so that it has a level.dat and
// a level.dat_old.
func createWorld(t *testing.T, dir string, seed int64) {
	t.Helper()
	for range 2 {
		db, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		w := world.Config{Synchronous: true, Provider: db, Seed: seed}.New()
		w.SetDifficulty(world.DifficultyHard)
		w.SetFallDamage(false)
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func openLogged(t *testing.T, dir string) (*DB, *bytes.Buffer, error) {
	t.Helper()
	var logs bytes.Buffer
	db, err := Config{Log: slog.New(slog.NewTextHandler(&logs, nil))}.Open(dir)
	if err == nil {
		t.Cleanup(func() { _ = db.Close() })
	}
	return db, &logs, err
}

// TestLevelDatRoundTripBytes checks that the level.dat a world saves is the
// bytes Write writes, as WriteFile wrote them in place before, and that
// reading it and writing it again with WriteFile changes no byte.
func TestLevelDatRoundTripBytes(t *testing.T) {
	dir := t.TempDir()
	createWorld(t, dir, 0x5eed)
	name := filepath.Join(dir, "level.dat")
	saved, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}

	ldat, err := leveldat.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var data leveldat.Data
	if err := ldat.Unmarshal(&data); err != nil {
		t.Fatal(err)
	}
	if data.RandomSeed != 0x5eed {
		t.Fatalf("saved seed = %v, want %v", data.RandomSeed, 0x5eed)
	}
	var encoded bytes.Buffer
	if err := ldat.Write(&encoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded.Bytes(), saved) {
		t.Fatalf("saved level.dat (%d bytes) differs from Write of it (%d bytes)", len(saved), encoded.Len())
	}

	copied := filepath.Join(t.TempDir(), "level.dat")
	if err := ldat.WriteFile(copied); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(copied); err != nil || !bytes.Equal(got, saved) {
		t.Fatalf("level.dat read and written again differs (err %v)", err)
	}
}

// TestOpenRecoversLevelDatFromOld breaks the level.dat of a world in the ways
// an interrupted in-place write or a crash between the renames of a save
// would, and checks that Open reads the level.dat_old instead and says so.
func TestOpenRecoversLevelDatFromOld(t *testing.T) {
	const seed = -0x1234567890
	for name, breakLevelDat := range map[string]func(path string) error{
		"empty":     func(path string) error { return os.Truncate(path, 0) },
		"truncated": func(path string) error { return os.Truncate(path, 10) },
		"half": func(path string) error {
			st, err := os.Stat(path)
			if err != nil {
				return err
			}
			return os.Truncate(path, st.Size()/2)
		},
		"garbage header": func(path string) error {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			copy(b, []byte{0xff, 0xff, 0xff, 0x7f, 0xff, 0xff, 0xff, 0xff})
			return os.WriteFile(path, b, 0644)
		},
		"garbage data": func(path string) error {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i := 8; i < len(b); i++ {
				b[i] = 0xee
			}
			return os.WriteFile(path, b, 0644)
		},
		"missing": os.Remove,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			createWorld(t, dir, seed)
			if err := breakLevelDat(filepath.Join(dir, "level.dat")); err != nil {
				t.Fatal(err)
			}
			db, logs, err := openLogged(t, dir)
			if err != nil {
				t.Fatalf("open with a broken level.dat and a good level.dat_old: %v", err)
			}
			s := db.Settings().Snapshot()
			if !s.SeedSet || s.Seed != seed || s.Difficulty != world.DifficultyHard || s.FallDamage {
				t.Fatalf("settings not read from level.dat_old: seed %v (set %v), difficulty %v, fall damage %v", s.Seed, s.SeedSet, s.Difficulty, s.FallDamage)
			}
			if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "read level.dat_old instead") {
				t.Fatalf("no warning logged reading level.dat_old: %q", logs.String())
			}

			// Saving again writes a good level.dat with the same settings.
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db, logs, err = openLogged(t, dir)
			if err != nil {
				t.Fatal(err)
			}
			if s := db.Settings().Snapshot(); s.Seed != seed {
				t.Fatalf("seed after saving again = %v, want %v", s.Seed, seed)
			}
			if logs.Len() != 0 {
				t.Fatalf("logged opening the world saved again: %q", logs.String())
			}
		})
	}
}

// TestOpenBothLevelDatsBroken checks what Open does when neither file can
// be read, which is what it did before there was a level.dat_old: a world
// whose level.dat exists but cannot be read is not opened, and a world
// without level.dat gets new settings.
func TestOpenBothLevelDatsBroken(t *testing.T) {
	t.Run("level.dat broken", func(t *testing.T) {
		dir := t.TempDir()
		createWorld(t, dir, 1)
		for _, name := range []string{"level.dat", "level.dat_old"} {
			if err := os.Truncate(filepath.Join(dir, name), 10); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err := openLogged(t, dir)
		if err == nil || !strings.Contains(err.Error(), "open db: read level.dat: ") || !strings.Contains(err.Error(), "level.dat_old") {
			t.Fatalf("Open error = %v, want the error reading level.dat and level.dat_old", err)
		}
	})
	t.Run("level.dat broken, no level.dat_old", func(t *testing.T) {
		dir := t.TempDir()
		createWorld(t, dir, 1)
		if err := os.Remove(filepath.Join(dir, "level.dat_old")); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(filepath.Join(dir, "level.dat"), 10); err != nil {
			t.Fatal(err)
		}
		_, _, err := openLogged(t, dir)
		if err == nil || !strings.HasPrefix(err.Error(), "open db: read level.dat: ") || strings.Contains(err.Error(), "level.dat_old") {
			t.Fatalf("Open error = %v, want the error reading level.dat", err)
		}
	})
	t.Run("level.dat missing", func(t *testing.T) {
		dir := t.TempDir()
		createWorld(t, dir, 1)
		if err := os.Remove(filepath.Join(dir, "level.dat")); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(filepath.Join(dir, "level.dat_old"), 10); err != nil {
			t.Fatal(err)
		}
		db, logs, err := openLogged(t, dir)
		if err != nil {
			t.Fatal(err)
		}
		if db.Settings().Snapshot().SeedSet {
			t.Fatal("world without level.dat and a broken level.dat_old kept a seed, want new settings")
		}
		if !strings.Contains(logs.String(), "level=WARN") {
			t.Fatalf("no warning logged: %q", logs.String())
		}
	})
}
