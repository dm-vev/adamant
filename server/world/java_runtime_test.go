package world

import "testing"

func TestCreakingTimelineBoundaries(t *testing.T) {
	for _, dimension := range []Dimension{Overworld, Nether, End} {
		w := Config{Synchronous: true, Dim: dimension}.New()
		for _, test := range []struct{ tick int; active bool }{
			{12599, false}, {12600, true}, {23400, true}, {23401, false},
			{36600, true}, {-11400, true}, {-599, false},
		} {
			w.SetTime(test.tick)
			if got, want := w.CreakingActive(), test.active && dimension == Overworld; got != want { t.Errorf("dimension %v at %d: active=%v, want %v", dimension, test.tick, got, want) }
		}
		w.Close()
	}
}

func TestMonsterSpawningRequiresBothRules(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()
	for _, mobs := range []bool{false, true} {
		for _, monsters := range []bool{false, true} {
			w.SetSpawnMobs(mobs)
			w.SetSpawnMonsters(monsters)
			if got := w.MonsterSpawningEnabled(); got != (mobs && monsters) { t.Fatalf("spawn_mobs=%v spawn_monsters=%v: enabled=%v", mobs, monsters, got) }
		}
	}
}
