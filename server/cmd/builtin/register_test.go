package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/google/uuid"
)

// registerTestAdapter satisfies serverAdapter; constructors must not call
// it. Under its permissions, which Register installs, players have
// cmd.LevelAll.
type registerTestAdapter struct{ serverAdapter }

func (registerTestAdapter) PlayerLevel(uuid.UUID) cmd.Level { return cmd.LevelAll }
func (registerTestAdapter) CommandLevel(_ string, declared cmd.Level) cmd.Level {
	return declared
}
func (registerTestAdapter) CommandBlockLevel() cmd.Level { return cmd.LevelGameMaster }

// Every runnable is validated by cmd.New at registration; an invalid parameter
// layout panics there and would abort server startup.
func TestRegisterBuildsEveryBuiltinCommand(t *testing.T) {
	Register(registerTestAdapter{})
	for _, name := range []string{"tp", "damage", "effect", "execute", "data", "scoreboard", "worldborder", "locate", "tick", "forceload", "give", "kill", "ban", "op"} {
		if _, ok := cmd.ByAlias(name); !ok {
			t.Errorf("command %q was not registered", name)
		}
	}
}
