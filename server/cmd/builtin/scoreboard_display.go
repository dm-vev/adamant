package builtin

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

type scoreboardSetDisplay struct {
	adminPermission
	Objectives cmd.SubCommand        `cmd:"objectives"`
	SetDisplay cmd.SubCommand        `cmd:"setdisplay"`
	Slot       scoreboardDisplaySlot `cmd:"slot"`
	Objective  cmd.Optional[string]  `cmd:"objective"`
}
type scoreboardDisplaySlot string

func (scoreboardDisplaySlot) Type() string { return "scoreboard_display_slot" }
func (scoreboardDisplaySlot) Options(cmd.Source) []string {
	return []string{"list", "sidebar", "belowName"}
}
func (c scoreboardSetDisplay) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	m := scoreboardModel(tx)
	if m == nil {
		o.Error("world unavailable")
		return
	}
	objective, _ := c.Objective.Load()
	if err := m.SetDisplay(string(c.Slot), objective); err != nil {
		o.Errorf("cannot set display: %v", err)
		return
	}
	refreshScoreboard(tx, m)
	o.Printf("Set %s display to %s.", c.Slot, objective)
}
