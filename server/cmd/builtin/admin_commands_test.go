package builtin

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

type operatorTestAdapter struct {
	serverAdapter
	operator bool
}

func (s operatorTestAdapter) IsOperator(string) bool { return s.operator }

func TestAdminPermissionTracksOperatorStatusAndExecuteContext(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	var p *player.Player
	h := world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "Alice"})
	if err := w.Do(func(tx *world.Tx) { p = tx.AddEntity(h).(*player.Player) }).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	if (adminPermission{srv: operatorTestAdapter{}}).Allow(p) {
		t.Fatal("non-operator allowed to run an admin command")
	}
	operator := adminPermission{srv: operatorTestAdapter{operator: true}}
	if !operator.Allow(p) {
		t.Fatal("operator denied an admin command")
	}
	wrapped := cmd.WithContext(p, cmd.ExecutionContext{Executor: p})
	if (adminPermission{srv: operatorTestAdapter{}}).Allow(wrapped) {
		t.Fatal("execute context bypassed admin command permission")
	}
}
