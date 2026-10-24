package builtin

import "github.com/df-mc/dragonfly/server/cmd"

// registerScoreboardCommands registers scoreboard, team, bossbar and trigger commands.
func registerScoreboardCommands(srv serverAdapter) {
	permission := adminPermission{srv}
	cmd.Register(newScoreboardCommand(srv))
	cmd.Register(newCommand("team", nil,
		teamAdd{adminPermission: permission}, teamRemove{adminPermission: permission},
		teamJoin{adminPermission: permission}, teamLeave{adminPermission: permission}, teamModify{adminPermission: permission}))
	cmd.Register(newCommand("trigger", nil, triggerEnable{}, triggerUse{}))
	cmd.Register(newCommand("random", nil, randomValue{}))
	cmd.Register(newCommand("teammsg", []string{"tm"}, teamMessage{}))
	cmd.Register(newCommand("bossbar", nil,
		bossbarAdd{adminPermission: permission}, bossbarRemove{adminPermission: permission}, bossbarSet{adminPermission: permission}))
}
