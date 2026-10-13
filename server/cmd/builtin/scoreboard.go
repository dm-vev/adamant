package builtin

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
)

type scoreboardObjectiveAdd struct {
	adminPermission
	Add      cmd.SubCommand     `cmd:"add"`
	Name     string             `cmd:"objective"`
	Criteria scoreboardCriteria `cmd:"criteria"`
	Display  string             `cmd:"displayName"`
}
type scoreboardObjectiveRemove struct {
	adminPermission
	Remove cmd.SubCommand `cmd:"remove"`
	Name   string         `cmd:"objective"`
}
type scoreboardObjectiveList struct {
	adminPermission
	List cmd.SubCommand `cmd:"list"`
}
type scoreboardScoreSet struct {
	adminPermission
	Players   cmd.SubCommand `cmd:"players"`
	Set       cmd.SubCommand `cmd:"set"`
	Holder    string         `cmd:"targets"`
	Objective string         `cmd:"objective"`
	Value     int            `cmd:"score"`
}
type scoreboardScoreAdd struct {
	adminPermission
	Players   cmd.SubCommand `cmd:"players"`
	Add       cmd.SubCommand `cmd:"add"`
	Holder    string         `cmd:"targets"`
	Objective string         `cmd:"objective"`
	Value     int            `cmd:"score"`
}
type scoreboardScoreOperation struct {
	adminPermission
	Players         cmd.SubCommand      `cmd:"players"`
	Operation       cmd.SubCommand      `cmd:"operation"`
	Target          string              `cmd:"targets"`
	TargetObjective string              `cmd:"targetObjective"`
	Operator        scoreboardOperation `cmd:"operator"`
	Source          string              `cmd:"source"`
	SourceObjective string              `cmd:"sourceObjective"`
}
type scoreboardCriteria string

func (scoreboardCriteria) Type() string { return "scoreboard_criteria" }
func (scoreboardCriteria) Options(cmd.Source) []string {
	out := make([]string, len(scoreboard.CriteriaNames))
	for i, c := range scoreboard.CriteriaNames {
		out[i] = string(c)
	}
	return out
}

type scoreboardOperation string

func (scoreboardOperation) Type() string { return "scoreboard_operation" }
func (scoreboardOperation) Options(cmd.Source) []string {
	return []string{"=", "+=", "-=", "*=", "/=", "%=", "<", ">", "><"}
}

func (c scoreboardObjectiveAdd) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if err := m.AddObjective(c.Name, c.Display, scoreboard.Criteria(c.Criteria)); err != nil {
		o.Errorf("cannot add objective: %v", err)
		return
	}
	refreshScoreboard(tx, m)
	o.Printf("Added objective %s.", c.Name)
}
func (c scoreboardObjectiveRemove) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if !m.RemoveObjective(c.Name) {
		o.Error("objective not found")
		return
	}
	refreshScoreboard(tx, m)
	o.Printf("Removed objective %s.", c.Name)
}
func (scoreboardObjectiveList) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	o.Printf("Objectives: %v", m.ObjectiveNames())
}
func (c scoreboardScoreSet) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if err := m.SetScore(c.Objective, c.Holder, c.Value); err != nil {
		o.Errorf("cannot set score: %v", err)
		return
	}
	refreshScoreboard(tx, m)
	o.Printf("Set %s's score in %s to %d.", c.Holder, c.Objective, c.Value)
}
func (c scoreboardScoreAdd) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	v, err := m.AddScore(c.Objective, c.Holder, c.Value)
	if err != nil {
		o.Errorf("cannot add score: %v", err)
		return
	}
	refreshScoreboard(tx, m)
	o.Printf("Set %s's score in %s to %d.", c.Holder, c.Objective, v)
}
func (c scoreboardScoreOperation) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if err := m.Operate(c.TargetObjective, c.Target, string(c.Operator), c.SourceObjective, c.Source); err != nil {
		o.Errorf("cannot operate on scores: %v", err)
		return
	}
	refreshScoreboard(tx, m)
	o.Printf("Updated %s's score in %s.", c.Target, c.TargetObjective)
}
func scoreboardModel(tx *world.Tx) *scoreboard.Model {
	if tx == nil {
		return nil
	}
	return tx.World().Scoreboard()
}

func newScoreboardCommand(srv serverAdapter) cmd.Command {
	permission := adminPermission{srv}
	return cmd.New("scoreboard", "Manage objectives, scores and displays.", nil,
		scoreboardObjectiveAdd{adminPermission: permission},
		scoreboardObjectiveRemove{adminPermission: permission},
		scoreboardObjectiveList{adminPermission: permission},
		scoreboardSetDisplay{adminPermission: permission},
		scoreboardScoreSet{adminPermission: permission},
		scoreboardScoreAdd{adminPermission: permission},
		scoreboardScoreOperation{adminPermission: permission},
	)
}
