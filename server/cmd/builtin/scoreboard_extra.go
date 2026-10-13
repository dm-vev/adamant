package builtin

import (
	"math/rand/v2"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
)

type teamAdd struct {
	adminPermission
	Add     cmd.SubCommand `cmd:"add"`
	Name    string         `cmd:"team"`
	Display string         `cmd:"displayName"`
}
type teamRemove struct {
	adminPermission
	Remove cmd.SubCommand `cmd:"remove"`
	Name   string         `cmd:"team"`
}
type teamJoin struct {
	adminPermission
	Join    cmd.SubCommand `cmd:"join"`
	Name    string         `cmd:"team"`
	Members string         `cmd:"members"`
}
type teamLeave struct {
	adminPermission
	Leave   cmd.SubCommand `cmd:"leave"`
	Members string         `cmd:"members"`
}
type teamModify struct {
	adminPermission
	Modify   cmd.SubCommand `cmd:"modify"`
	Name     string         `cmd:"team"`
	Property teamProperty   `cmd:"property"`
	Value    cmd.Varargs    `cmd:"value"`
}
type teamProperty string

func (teamProperty) Type() string { return "team_property" }
func (teamProperty) Options(cmd.Source) []string {
	return []string{"displayName", "prefix", "suffix", "color", "friendlyFire", "nametagVisibility", "collisionRule"}
}

func (c teamModify) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if err := m.ModifyTeam(c.Name, string(c.Property), strings.TrimSpace(string(c.Value))); err != nil {
		o.Errorf("cannot modify team: %v", err)
		return
	}
	refreshTeamNameTags(tx, m)
	o.Printf("Modified team %s.", c.Name)
}

func (c teamAdd) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if err := m.AddTeam(c.Name, c.Display); err != nil {
		o.Errorf("cannot add team: %v", err)
		return
	}
	o.Printf("Added team %s.", c.Name)
}
func (c teamRemove) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	oldTeam, _ := m.Team(c.Name)
	if !m.RemoveTeam(c.Name) {
		o.Error("team not found")
		return
	}
	for p := range onlinePlayers(tx) {
		if oldTeam.Members[p.Name()] {
			p.SetNameTag(p.Name())
		}
	}
	refreshTeamNameTags(tx, m)
	o.Printf("Removed team %s.", c.Name)
}
func (c teamJoin) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if err := m.JoinTeam(c.Name, c.Members); err != nil {
		o.Errorf("cannot join team: %v", err)
		return
	}
	refreshTeamNameTags(tx, m)
	o.Printf("Added %s to team %s.", c.Members, c.Name)
}
func (c teamLeave) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if m.LeaveTeam(c.Members) == 0 {
		o.Error("not a team member")
		return
	}
	for p := range onlinePlayers(tx) {
		if p.Name() == c.Members {
			p.SetNameTag(p.Name())
		}
	}
	refreshTeamNameTags(tx, m)
	o.Printf("Removed %s from their team.", c.Members)
}

type teamMessage struct {
	Message cmd.Varargs `cmd:"message"`
}

func (c teamMessage) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	sender, ok := src.(*player.Player)
	if m == nil || !ok {
		o.Error("player and world required")
		return
	}
	team, ok := m.TeamFor(sender.Name())
	if !ok {
		o.Error("you are not on a team")
		return
	}
	message := strings.TrimSpace(string(c.Message))
	for p := range onlinePlayers(tx) {
		if team.Members[p.Name()] {
			p.Messagef("[%s] %s: %s", team.DisplayName, sender.Name(), message)
		}
	}
}

type triggerEnable struct {
	Enable    cmd.SubCommand `cmd:"enable"`
	Objective string         `cmd:"objective"`
}
type triggerUse struct {
	Use       cmd.SubCommand `cmd:"use"`
	Objective string         `cmd:"objective"`
}

func commandPlayerName(src cmd.Source) string {
	if p, ok := src.(*player.Player); ok {
		return p.Name()
	}
	return ""
}
func (c triggerEnable) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	name := commandPlayerName(src)
	if m == nil || name == "" {
		o.Error("player and world required")
		return
	}
	obj, ok := m.Objective(c.Objective)
	if !ok || obj.Criteria != scoreboard.Trigger {
		o.Error("trigger objective not found")
		return
	}
	m.SetTriggerEnabled(name, true)
	o.Printf("Enabled trigger %s for %s.", c.Objective, name)
}
func (c triggerUse) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	name := commandPlayerName(src)
	if m == nil || name == "" {
		o.Error("player and world required")
		return
	}
	if err := m.TriggerUse(c.Objective, name); err != nil {
		o.Errorf("cannot use trigger: %v", err)
		return
	}
	o.Printf("Used trigger %s.", c.Objective)
}

type randomValue struct {
	Value cmd.SubCommand `cmd:"value"`
	Min   int            `cmd:"min"`
	Max   int            `cmd:"max"`
}

func (c randomValue) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if c.Min > c.Max {
		o.Error("minimum exceeds maximum")
		return
	}
	o.Printf("%d", c.Min+rand.IntN(c.Max-c.Min+1))
}

type bossbarSet struct {
	adminPermission
	Set      cmd.SubCommand  `cmd:"set"`
	ID       string          `cmd:"id"`
	Property bossbarProperty `cmd:"property"`
	Value    cmd.Varargs     `cmd:"value"`
}
type bossbarProperty string

func (bossbarProperty) Type() string { return "bossbar_property" }
func (bossbarProperty) Options(cmd.Source) []string {
	return []string{"name", "value", "max", "color", "visible", "players"}
}

func (c bossbarSet) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if err := m.ModifyBossBar(c.ID, string(c.Property), strings.TrimSpace(string(c.Value))); err != nil {
		o.Errorf("cannot modify bossbar: %v", err)
		return
	}
	bar, _ := m.BossBar(c.ID)
	sendBossBar(tx, bar)
	o.Printf("Updated bossbar %s.", c.ID)
}

type bossbarAdd struct {
	adminPermission
	Add  cmd.SubCommand `cmd:"add"`
	ID   string         `cmd:"id"`
	Name string         `cmd:"name"`
}
type bossbarRemove struct {
	adminPermission
	Remove cmd.SubCommand `cmd:"remove"`
	ID     string         `cmd:"id"`
}

func (c bossbarAdd) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if err := m.AddBossBar(scoreboard.BossBar{ID: c.ID, Name: c.Name, Value: 1, Max: 1, Color: "purple", Visible: true}); err != nil {
		o.Errorf("cannot add bossbar: %v", err)
		return
	}
	sendBossBar(tx, scoreboard.BossBar{ID: c.ID, Name: c.Name, Value: 1, Max: 1, Color: "purple", Visible: true})
	o.Printf("Added bossbar %s.", c.ID)
}
func (c bossbarRemove) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	if !m.RemoveBossBar(c.ID) {
		o.Error("bossbar not found")
		return
	}
	for p := range onlinePlayers(tx) {
		p.RemoveBossBarID(c.ID)
	}
	o.Printf("Removed bossbar %s.", c.ID)
}
func sendBossBar(tx *world.Tx, data scoreboard.BossBar) {
	if tx == nil {
		return
	}
	if !data.Visible {
		for p := range onlinePlayers(tx) {
			p.RemoveBossBarID(data.ID)
		}
		return
	}
	color := bossbar.Purple()
	switch data.Color {
	case "pink":
		color = bossbar.Pink()
	case "blue":
		color = bossbar.Blue()
	case "red":
		color = bossbar.Red()
	case "green":
		color = bossbar.Green()
	case "yellow":
		color = bossbar.Yellow()
	case "white":
		color = bossbar.White()
	}
	bar := bossbar.New(data.Name).WithID(data.ID).WithColour(color).WithHealthPercentage(data.Value / data.Max)
	for p := range onlinePlayers(tx) {
		show := len(data.Players) == 0
		for _, name := range data.Players {
			if name == p.Name() {
				show = true
				break
			}
		}
		if show {
			p.SendBossBar(bar)
		} else {
			p.RemoveBossBarID(data.ID)
		}
	}
}
