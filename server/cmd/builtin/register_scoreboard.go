package builtin

import "github.com/df-mc/dragonfly/server/cmd"

// registerScoreboardCommands registers scoreboard, team, bossbar and trigger commands.
func registerScoreboardCommands(srv serverAdapter) {
	cmd.Register(newScoreboardCommand(srv))
	cmd.Register(newCommand("team", nil,
		teamAdd{}, teamRemove{},
		teamJoin{}, teamLeave{}, teamModify{}))
	cmd.Register(newCommand("trigger", nil, triggerEnable{}, triggerUse{}))
	cmd.Register(newCommand("random", nil, randomValue{}))
	cmd.Register(newCommand("teammsg", []string{"tm"}, teamMessage{}))
	cmd.Register(newCommand("bossbar", nil,
		bossbarAdd{}, bossbarRemove{}, bossbarSet{}))
}
