package builtin

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

func TestExperienceCommandAddLevelsSetPointsAndQuery(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "XPTarget"})).(*player.Player)
		xp := newExperienceCommand()
		runTestCommand(xp, "add @a 3 levels", tx)
		if p.ExperienceLevel() != 3 {
			t.Fatalf("add levels: level=%d, want 3", p.ExperienceLevel())
		}
		runTestCommand(xp, "set @a 17 points", tx)
		if p.Experience() != 17 {
			t.Fatalf("set points: experience=%d, want 17", p.Experience())
		}
		query := runTestCommand(xp, "query @a points", tx)
		if query.MessageCount() != 1 || !strings.Contains(query.Messages()[0].String(), "17") {
			t.Fatalf("query messages=%v errors=%v", query.Messages(), query.Errors())
		}
	})
}
