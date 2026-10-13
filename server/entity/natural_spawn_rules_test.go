package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// phantomsAfterSleeplessNight returns how many phantoms spawn over an
// insomniac player during the next phantom spawner pass at night on hard.
func phantomsAfterSleeplessNight(t *testing.T, rules map[string]bool) int {
	t.Helper()
	w := newDeathTestWorld(t)
	w.SetDifficulty(world.DifficultyHard)
	w.SetTime(18000)
	w.StopTime()
	for rule, value := range rules {
		if err := w.SetGameRule(rule, value); err != nil {
			t.Fatal(err)
		}
	}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
		tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: mgl64.Vec3{.5, 64, .5}, GameMode: world.GameModeSurvival, TimeSinceRest: 10_000_000}))
	})
	phantoms := 0
	for range 1801 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { phantoms = countEntities(tx, "minecraft:phantom") })
		if phantoms != 0 {
			break
		}
	}
	return phantoms
}

func TestPhantomsNeedMonsterSpawning(t *testing.T) {
	if phantomsAfterSleeplessNight(t, nil) == 0 {
		t.Fatal("no phantoms spawned over an insomniac player")
	}
	for _, rule := range []string{"spawn_mobs", "spawn_monsters", "spawn_phantoms"} {
		if n := phantomsAfterSleeplessNight(t, map[string]bool{rule: false}); n != 0 {
			t.Errorf("%d phantoms spawned with %s off", n, rule)
		}
	}
}
