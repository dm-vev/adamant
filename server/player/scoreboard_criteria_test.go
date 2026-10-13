package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	model "github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestScoreboardCriteriaTrackPlayerStatsAndKills(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	job := w.Do(func(tx *world.Tx) {
		m := tx.World().Scoreboard()
		for name, criteria := range map[string]model.Criteria{"hp": model.Health, "lvl": model.Level, "deaths": model.DeathCount, "pk": model.PlayerKillCount, "total": model.TotalKillCount, "notes": model.Dummy} {
			if err := m.AddObjective(name, name, criteria); err != nil {
				t.Fatal(err)
			}
		}
		victim := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}.New(Type, Config{Name: "Victim", GameMode: world.GameModeSurvival})).(*Player)
		killer := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 0}}.New(Type, Config{Name: "Killer", GameMode: world.GameModeSurvival})).(*Player)
		victim.Hurt(6, entity.FallDamageSource{})
		killer.SetExperienceLevel(4)
		victim.tickScoreboard(tx, 20)
		killer.tickScoreboard(tx, 20)
		if v, _ := m.Score("hp", "Victim"); v != 14 {
			t.Fatalf("health criteria = %d, want 14", v)
		}
		if v, _ := m.Score("lvl", "Killer"); v != 4 {
			t.Fatalf("level criteria = %d, want 4", v)
		}
		if _, ok := m.Score("notes", "Victim"); ok {
			t.Fatal("dummy objective received a stat update")
		}
		victim.Hurt(100, entity.AttackDamageSource{Attacker: killer})
		if v, _ := m.Score("deaths", "Victim"); v != 1 {
			t.Fatalf("deathCount = %d, want 1", v)
		}
		if pk, _ := m.Score("pk", "Killer"); pk != 1 {
			t.Fatalf("playerKillCount = %d, want 1", pk)
		}
		if total, _ := m.Score("total", "Killer"); total != 1 {
			t.Fatalf("totalKillCount = %d, want 1", total)
		}
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
}
