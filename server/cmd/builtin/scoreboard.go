package builtin

import (
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
)

// Objective subcommands follow Java syntax: scoreboard objectives add|remove|list.
type scoreboardObjectiveAdd struct {
	adminPermission
	Objectives cmd.SubCommand       `cmd:"objectives"`
	Add        cmd.SubCommand       `cmd:"add"`
	Name       string               `cmd:"objective"`
	Criteria   scoreboardCriteria   `cmd:"criteria"`
	Display    cmd.Optional[string] `cmd:"displayName"`
}
type scoreboardObjectiveRemove struct {
	adminPermission
	Objectives cmd.SubCommand `cmd:"objectives"`
	Remove     cmd.SubCommand `cmd:"remove"`
	Name       string         `cmd:"objective"`
}
type scoreboardObjectiveList struct {
	adminPermission
	Objectives cmd.SubCommand `cmd:"objectives"`
	List       cmd.SubCommand `cmd:"list"`
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
type scoreboardScoreRemove struct {
	adminPermission
	Players   cmd.SubCommand `cmd:"players"`
	Remove    cmd.SubCommand `cmd:"remove"`
	Holder    string         `cmd:"targets"`
	Objective string         `cmd:"objective"`
	Value     int            `cmd:"score"`
}
type scoreboardScoreGet struct {
	adminPermission
	Players   cmd.SubCommand `cmd:"players"`
	Get       cmd.SubCommand `cmd:"get"`
	Holder    string         `cmd:"target"`
	Objective string         `cmd:"objective"`
}
type scoreboardScoreReset struct {
	adminPermission
	Players   cmd.SubCommand       `cmd:"players"`
	Reset     cmd.SubCommand       `cmd:"reset"`
	Holder    string               `cmd:"targets"`
	Objective cmd.Optional[string] `cmd:"objective"`
}
type scoreboardScoreList struct {
	adminPermission
	Players cmd.SubCommand       `cmd:"players"`
	List    cmd.SubCommand       `cmd:"list"`
	Holder  cmd.Optional[string] `cmd:"target"`
}
type scoreboardScoreEnable struct {
	adminPermission
	Players   cmd.SubCommand `cmd:"players"`
	Enable    cmd.SubCommand `cmd:"enable"`
	Holder    string         `cmd:"targets"`
	Objective string         `cmd:"objective"`
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
	// Java uses the objective name as display name when none is given.
	if err := m.AddObjective(c.Name, c.Display.LoadOr(c.Name), scoreboard.Criteria(c.Criteria)); err != nil {
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

// scoreHolders resolves a Java ScoreHolderArgument: `*` is every tracked
// holder, selectors resolve to player names or entity UUIDs, and any other
// text is a literal (fake player) name.
func scoreHolders(spec string, src cmd.Source, tx *world.Tx, m *scoreboard.Model) ([]string, error) {
	switch {
	case spec == "*":
		return m.Holders(), nil
	case strings.HasPrefix(spec, "@"):
		targets, err := cmd.ResolveTargets(spec, src, tx)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(targets))
		for _, t := range targets {
			if name := cmd.ScoreHolderName(t); name != "" {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			return nil, cmd.MessageNoTargets.F()
		}
		return names, nil
	default:
		return []string{spec}, nil
	}
}

func (c scoreboardScoreSet) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	updateScores(src, o, tx, c.Holder, c.Objective, func(m *scoreboard.Model, holder string) (int, error) {
		return c.Value, m.SetScore(c.Objective, holder, c.Value)
	})
}
func (c scoreboardScoreAdd) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	updateScores(src, o, tx, c.Holder, c.Objective, func(m *scoreboard.Model, holder string) (int, error) {
		return m.AddScore(c.Objective, holder, c.Value)
	})
}
func (c scoreboardScoreRemove) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	updateScores(src, o, tx, c.Holder, c.Objective, func(m *scoreboard.Model, holder string) (int, error) {
		return m.AddScore(c.Objective, holder, -c.Value)
	})
}

// updateScores applies change to each holder; the result is the last value for
// one holder or the holder count, as in Java ScoreboardCommand.
func updateScores(src cmd.Source, o *cmd.Output, tx *world.Tx, spec, objective string, change func(*scoreboard.Model, string) (int, error)) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if obj, ok := m.Objective(objective); !ok {
		o.Errorf("Unknown scoreboard objective '%s'", objective)
		return
	} else if obj.Criteria != scoreboard.Dummy && obj.Criteria != scoreboard.Trigger {
		o.Errorf("That objective is read-only and cannot be set")
		return
	}
	holders, err := scoreHolders(spec, src, tx, m)
	if err != nil {
		o.Error(err)
		return
	}
	last := 0
	for _, holder := range holders {
		if last, err = change(m, holder); err != nil {
			o.Errorf("cannot change score: %v", err)
			return
		}
	}
	refreshScoreboard(tx, m)
	if len(holders) == 1 {
		o.SetResult(last)
		o.Printf("Set [%s] for %s to %d", objective, holders[0], last)
		return
	}
	o.SetResult(len(holders))
	o.Printf("Set [%s] for %d entities", objective, len(holders))
}

func (c scoreboardScoreGet) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	holders, err := scoreHolders(c.Holder, src, tx, m)
	if err != nil {
		o.Error(err)
		return
	}
	if len(holders) != 1 {
		o.Errorf("Only one entity is allowed, but the provided selector allows more than one")
		return
	}
	v, ok := m.Score(c.Objective, holders[0])
	if !ok {
		o.Errorf("Can't get value of %s for %s; none is set", c.Objective, holders[0])
		return
	}
	o.SetResult(v)
	o.Printf("%s has %d [%s]", holders[0], v, c.Objective)
}

func (c scoreboardScoreReset) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	holders, err := scoreHolders(c.Holder, src, tx, m)
	if err != nil {
		o.Error(err)
		return
	}
	objective, single := c.Objective.Load()
	for _, holder := range holders {
		if single {
			m.ResetScore(objective, holder)
		} else {
			m.ResetHolder(holder)
		}
	}
	refreshScoreboard(tx, m)
	o.SetResult(len(holders))
	o.Printf("Reset scores for %d entities", len(holders))
}

func (c scoreboardScoreList) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	spec, ok := c.Holder.Load()
	if !ok {
		holders := m.Holders()
		o.SetResult(len(holders))
		o.Printf("There are %d tracked entities: %s", len(holders), strings.Join(holders, ", "))
		return
	}
	holders, err := scoreHolders(spec, src, tx, m)
	if err != nil {
		o.Error(err)
		return
	}
	for _, holder := range holders {
		scores := m.HolderScores(holder)
		names := make([]string, 0, len(scores))
		for name := range scores {
			names = append(names, name)
		}
		slices.Sort(names)
		o.SetResult(len(scores))
		o.Printf("%s has %d scores:", holder, len(scores))
		for _, name := range names {
			o.Printf("[%s]: %d", name, scores[name])
		}
	}
}

func (c scoreboardScoreEnable) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if obj, ok := m.Objective(c.Objective); !ok || obj.Criteria != scoreboard.Trigger {
		o.Errorf("You can only enable trigger-objectives")
		return
	}
	holders, err := scoreHolders(c.Holder, src, tx, m)
	if err != nil {
		o.Error(err)
		return
	}
	for _, holder := range holders {
		m.SetTriggerEnabled(holder, true)
	}
	o.Printf("Enabled trigger %s for %d entities", c.Objective, len(holders))
}

func (c scoreboardScoreOperation) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	targets, err := scoreHolders(c.Target, src, tx, m)
	if err != nil {
		o.Error(err)
		return
	}
	sources, err := scoreHolders(c.Source, src, tx, m)
	if err != nil {
		o.Error(err)
		return
	}
	for _, target := range targets {
		for _, source := range sources {
			if err := m.Operate(c.TargetObjective, target, string(c.Operator), c.SourceObjective, source); err != nil {
				o.Errorf("cannot operate on scores: %v", err)
				return
			}
		}
	}
	refreshScoreboard(tx, m)
	if len(targets) == 1 {
		v, _ := m.Score(c.TargetObjective, targets[0])
		o.SetResult(v)
	}
	o.Printf("Updated [%s] for %d entities", c.TargetObjective, len(targets))
}
func scoreboardModel(tx *world.Tx) *scoreboard.Model {
	if tx == nil {
		return nil
	}
	return tx.World().Scoreboard()
}

func newScoreboardCommand(srv serverAdapter) cmd.Command {
	permission := adminPermission{srv}
	return newCommand("scoreboard", nil,
		scoreboardObjectiveAdd{adminPermission: permission},
		scoreboardObjectiveRemove{adminPermission: permission},
		scoreboardObjectiveList{adminPermission: permission},
		scoreboardSetDisplay{adminPermission: permission},
		scoreboardScoreSet{adminPermission: permission},
		scoreboardScoreAdd{adminPermission: permission},
		scoreboardScoreRemove{adminPermission: permission},
		scoreboardScoreGet{adminPermission: permission},
		scoreboardScoreReset{adminPermission: permission},
		scoreboardScoreList{adminPermission: permission},
		scoreboardScoreEnable{adminPermission: permission},
		scoreboardScoreOperation{adminPermission: permission},
	)
}
