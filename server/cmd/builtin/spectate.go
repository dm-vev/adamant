package builtin

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// spectateCommand ports SpectateCommand: a player in spectator mode watches
// through the eyes of an entity, or stops doing so without a target.
type spectateCommand struct {
	worldOpCommand
	Target cmd.Optional[[]cmd.Target] `cmd:"target"`
	Player cmd.Optional[[]cmd.Target] `cmd:"player"`
}

func registerSpectateCommand(guard worldOpCommand) {
	cmd.Register(newCommand("spectate", nil, spectateCommand{worldOpCommand: guard}))
}

// Run ...
func (c spectateCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	var spectator *player.Player
	if players, ok := c.Player.Load(); ok {
		t, ok := singleTarget(players, o)
		if !ok {
			return
		}
		if spectator, ok = t.(*player.Player); !ok {
			o.Errorf("Only players may be affected by this command, but the provided selector includes entities")
			return
		}
	} else {
		executor, ok := commandExecutor(src)
		if spectator, _ = executor.(*player.Player); !ok || spectator == nil {
			o.Errorf("A player is required to run this command here")
			return
		}
	}
	var target world.Entity
	if targets, ok := c.Target.Load(); ok {
		t, ok := singleTarget(targets, o)
		if !ok {
			return
		}
		if target, ok = t.(world.Entity); !ok {
			o.Errort(cmd.MessageNoTargets)
			return
		}
		if target.H() == spectator.H() {
			o.Errorf("You can't spectate yourself")
			return
		}
	}
	if spectator.GameMode() != world.GameModeSpectator {
		o.Errorf("%s is not in spectator mode", spectator.Name())
		return
	}
	spectator.Spectate(target)
	if target == nil {
		o.Printf("No longer spectating an entity")
		return
	}
	o.Printf("Now spectating %s", commandTargetName(target))
}
