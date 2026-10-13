package mcdb

import (
	"testing"

	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
)

func TestWorldScoreboardPersistsAcrossMCDBReopen(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Synchronous: true, RandomTickSpeed: -1}.New()
	if err := w.Scoreboard().AddObjective("kills", "Kills", scoreboard.Dummy); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w = world.Config{Provider: db, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	if _, ok := w.Scoreboard().Objective("kills"); !ok {
		t.Fatal("scoreboard objective did not persist")
	}
}
