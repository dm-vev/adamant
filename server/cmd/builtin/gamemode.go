package builtin

import (
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

type gamemodeCommand struct {
	Mode    gameModeValue              `cmd:"mode"`
	Targets cmd.Optional[[]cmd.Target] `cmd:"target"`
}

func newGamemodeCommand() cmd.Command {
	return cmd.New("gamemode", "Changes a player's game mode.", []string{"gm"}, gamemodeCommand{})
}

func (g gamemodeCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	mode, alias, ok := parseGameMode(string(g.Mode))
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, g.Mode)
		return
	}

	targets, ok := g.Targets.Load()
	if !ok {
		if p, ok := src.(*player.Player); ok {
			targets = []cmd.Target{p}
		} else {
			o.Errort(cmd.MessageNoTargets)
			return
		}
	}

	players := playersFromTargets(targets)
	if len(players) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}

	for _, p := range players {
		p.SetGameMode(mode)
	}
	o.Printf("Set %s to %s mode.", joinNames(players), alias)
}

func parseGameMode(value string) (world.GameMode, string, bool) {
	switch strings.ToLower(value) {
	case "0", "s", "survival":
		return world.GameModeSurvival, "survival", true
	case "1", "c", "creative":
		return world.GameModeCreative, "creative", true
	case "2", "a", "adventure":
		return world.GameModeAdventure, "adventure", true
	case "3", "sp", "spectator", "spectate":
		return world.GameModeSpectator, "spectator", true
	}
	return nil, "", false
}
