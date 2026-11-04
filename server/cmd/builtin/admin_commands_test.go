package builtin

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// TestAdminCommandsNeedAdminLevelAlsoThroughExecute checks that /ban runs
// for a player of cmd.LevelAdmin, not for one of cmd.LevelGameMaster, and
// that /execute gives the player no more.
func TestAdminCommandsNeedAdminLevelAlsoThroughExecute(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	var p *player.Player
	h := world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "Alice"})
	if err := w.Do(func(tx *world.Tx) { p = tx.AddEntity(h).(*player.Player) }).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ban := newBanPlayerCommand(registerTestAdapter{})
	wrapped := cmd.WithContext(p, cmd.ExecutionContext{Executor: p})
	stored := &storeSource{Source: wrapped, store: func(bool, int) {}}
	playersAtLevel(t, cmd.LevelGameMaster)
	for _, src := range []cmd.Source{p, wrapped, stored} {
		if len(ban.Runnables(src)) != 0 {
			t.Fatalf("%T of a game master may run /ban", src)
		}
	}
	playersAtLevel(t, cmd.LevelAdmin)
	for _, src := range []cmd.Source{p, wrapped, stored} {
		if len(ban.Runnables(src)) == 0 {
			t.Fatalf("%T of an admin may not run /ban", src)
		}
	}
}
