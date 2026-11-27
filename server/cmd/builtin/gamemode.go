package builtin

import (
	"strings"

	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
)

type gamemodeCommand struct {
	gameMaster
	Mode    gameModeValue             `cmd:"mode"`
	Targets cmd.Optional[cmd.Players] `cmd:"target"`
}

func newGamemodeCommand() cmd.Command {
	return newCommand("gamemode", []string{"gm"}, gamemodeCommand{})
}

func (g gamemodeCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	mode, alias, ok := parseGameMode(string(g.Mode))
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, g.Mode)
		return
	}
	cmd.ForEach(src, o, tx, optionalPlayers(g.Targets, src),
		func(_ *world.Tx, t cmd.Target) int {
			p, ok := t.(*player.Player)
			if !ok {
				return 0
			}
			p.SetGameMode(mode)
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Set %s to %s mode.", joinNonZeroNames(rs), alias)
		},
	)
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
