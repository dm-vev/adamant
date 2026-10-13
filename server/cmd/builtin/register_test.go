package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
)

// registerTestAdapter satisfies serverAdapter; constructors must not call it.
type registerTestAdapter struct{ serverAdapter }

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
