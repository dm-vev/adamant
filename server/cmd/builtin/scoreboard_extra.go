package builtin

import (
	"math/rand/v2"
	"strings"

	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/player/bossbar"
	"github.com/dm-vev/adamant/server/scoreboard"
	"github.com/dm-vev/adamant/server/world"
)

type teamAdd struct {
	gameMaster
	Add     cmd.SubCommand `cmd:"add"`
	Name    string         `cmd:"team"`
	Display string         `cmd:"displayName"`
}
type teamRemove struct {
	gameMaster
	Remove cmd.SubCommand `cmd:"remove"`
	Name   string         `cmd:"team"`
}
type teamJoin struct {
	gameMaster
	Join    cmd.SubCommand `cmd:"join"`
	Name    string         `cmd:"team"`
	Members string         `cmd:"members"`
}
type teamLeave struct {
	gameMaster
	Leave   cmd.SubCommand `cmd:"leave"`
	Members string         `cmd:"members"`
}
type teamModify struct {
	gameMaster
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
	eachOnlinePlayer(tx, func(p *player.Player) {
		if oldTeam.Members[p.Name()] {
			p.SetNameTag(p.Name())
		}
	})
	refreshTeamNameTags(tx, m)
	o.Printf("Removed team %s.", c.Name)
}
func (c teamJoin) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	teamName := c.Name
	resolveScoreHolders(c.Members, src, o, tx, m, func(_ cmd.Source, o *cmd.Output, tx *world.Tx, holders []string, err error) {
		if err != nil {
			o.Error(err)
			return
		}
		for _, holder := range holders {
			if err := m.JoinTeam(teamName, holder); err != nil {
				o.Errorf("cannot join team: %v", err)
				return
			}
		}
		refreshTeamNameTags(tx, m)
		if len(holders) == 1 {
			o.Printf("Added %s to team %s.", holders[0], teamName)
			return
		}
		o.Printf("Added %d entities to team %s.", len(holders), teamName)
	})
}
func (c teamLeave) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	resolveScoreHolders(c.Members, src, o, tx, m, func(_ cmd.Source, o *cmd.Output, tx *world.Tx, holders []string, err error) {
		if err != nil {
			o.Error(err)
			return
		}
		removed := make(map[string]struct{}, len(holders))
		for _, holder := range holders {
			if m.LeaveTeam(holder) > 0 {
				removed[holder] = struct{}{}
			}
		}
		if len(removed) == 0 {
			o.Error("not a team member")
			return
		}
		eachOnlinePlayer(tx, func(p *player.Player) {
			if _, ok := removed[p.Name()]; ok {
				p.SetNameTag(p.Name())
			}
		})
		refreshTeamNameTags(tx, m)
		if len(holders) == 1 {
			o.Printf("Removed %s from their team.", holders[0])
			return
		}
		o.Printf("Removed %d entities from their teams.", len(removed))
	})
}

type teamMessage struct {
	Message cmd.Varargs `cmd:"message"`
}

func (c teamMessage) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	senderName := commandPlayerName(src)
	if m == nil || senderName == "" {
		o.Error("player and world required")
		return
	}
	team, ok := m.TeamFor(senderName)
	if !ok {
		o.Error("you are not on a team")
		return
	}
	message := strings.TrimSpace(string(c.Message))
	eachOnlinePlayer(tx, func(p *player.Player) {
		if team.Members[p.Name()] {
			p.Messagef("[%s] %s: %s", team.DisplayName, senderName, message)
		}
	})
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
	if ctx, ok := cmd.ExecutionContextOf(src); ok {
		if p, ok := ctx.Executor.(*player.Player); ok {
			return p.Name()
		}
	}
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
	gameMaster
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

func (c bossbarSet) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	id, prop, val := c.ID, string(c.Property), strings.TrimSpace(string(c.Value))
	apply := func(o *cmd.Output, tx *world.Tx, value string) {
		if err := m.ModifyBossBar(id, prop, value); err != nil {
			o.Errorf("cannot modify bossbar: %v", err)
			return
		}
		bar, _ := m.BossBar(id)
		sendBossBar(tx, bar)
		o.Printf("Updated bossbar %s.", id)
	}
	if prop == "players" && strings.HasPrefix(val, "@") {
		sel, err := cmd.ParsePlayers(val, src)
		if err != nil {
			o.Error(err)
			return
		}
		cmd.ForEach(src, o, tx, sel,
			func(_ *world.Tx, t cmd.Target) string {
				if p, ok := t.(*player.Player); ok {
					return p.Name()
				}
				return ""
			},
			func(_ cmd.Source, o *cmd.Output, tx *world.Tx, rs []cmd.Result[string]) {
				names := make([]string, 0, len(rs))
				for _, r := range rs {
					if r.Value != "" {
						names = append(names, r.Value)
					}
				}
				if len(names) == 0 {
					o.Errort(cmd.MessageNoTargets)
					return
				}
				apply(o, tx, strings.Join(names, ","))
			},
		)
		return
	}
	apply(o, tx, val)
}

type bossbarAdd struct {
	gameMaster
	Add  cmd.SubCommand `cmd:"add"`
	ID   string         `cmd:"id"`
	Name string         `cmd:"name"`
}
type bossbarRemove struct {
	gameMaster
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
	id := c.ID
	eachOnlinePlayer(tx, func(p *player.Player) {
		p.RemoveBossBarID(id)
	})
	o.Printf("Removed bossbar %s.", c.ID)
}
func sendBossBar(tx *world.Tx, data scoreboard.BossBar) {
	if tx == nil {
		return
	}
	if !data.Visible {
		eachOnlinePlayer(tx, func(p *player.Player) {
			p.RemoveBossBarID(data.ID)
		})
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
	eachOnlinePlayer(tx, func(p *player.Player) {
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
	})
}
