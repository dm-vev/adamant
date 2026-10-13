package mcdb

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestJavaOnlyGameRulePersistsAcrossMCDBReopen(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Synchronous: true, RandomTickSpeed: -1}.New()
	if err := w.SetGameRule("spawn_patrols", false); err != nil {
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
	got, ok := w.GameRule("spawn_patrols")
	if !ok || got != false {
		t.Fatalf("reopened spawn_patrols = (%v, %t), want (false, true)", got, ok)
	}
}
