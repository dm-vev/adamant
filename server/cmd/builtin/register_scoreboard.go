package builtin

import "github.com/df-mc/dragonfly/server/cmd"

// registerScoreboardCommands registers scoreboard, team, bossbar and trigger commands.
func registerScoreboardCommands(srv serverAdapter) {
	permission := adminPermission{srv}
	cmd.Register(newScoreboardCommand(srv))
	cmd.Register(cmd.New("team", "Manage scoreboard teams.", nil,
		teamAdd{adminPermission: permission}, teamRemove{adminPermission: permission},
		teamJoin{adminPermission: permission}, teamLeave{adminPermission: permission}, teamModify{adminPermission: permission}))
	cmd.Register(cmd.New("trigger", "Use a trigger objective.", nil, triggerEnable{}, triggerUse{}))
	cmd.Register(cmd.New("random", "Generate a random value.", nil, randomValue{}))
	cmd.Register(cmd.New("teammsg", "Send a message to your team.", []string{"tm"}, teamMessage{}))
	cmd.Register(cmd.New("bossbar", "Manage boss bars.", nil,
		bossbarAdd{adminPermission: permission}, bossbarRemove{adminPermission: permission}, bossbarSet{adminPermission: permission}))
}
