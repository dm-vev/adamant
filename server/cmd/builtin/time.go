package builtin

import (
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

type timeSetCommand struct {
	Set   cmd.SubCommand `cmd:"set"`
	Value string         `cmd:"value"`
}

type timeAddCommand struct {
	Add   cmd.SubCommand `cmd:"add"`
	Value int            `cmd:"value"`
}

type timeQueryCommand struct {
	Query cmd.SubCommand `cmd:"query"`
	Type  timeQueryType  `cmd:"type"`
}

type timeQueryType string

func (timeQueryType) Type() string { return "timequery" }

func (timeQueryType) Options(cmd.Source) []string {
	return []string{"daytime", "gametime", "day"}
}

func newTimeCommand() cmd.Command {
	return cmd.New(
		"time",
		"Adjusts or queries the world time.",
		nil,
		timeSetCommand{},
		timeAddCommand{},
		timeQueryCommand{},
	)
}

func (t timeSetCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	if w == nil {
		o.Error("world unavailable")
		return
	}
	val, ok := parseTimeValue(t.Value)
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, t.Value)
		return
	}
	w.SetTime(val % 24000)
	o.Printf("Set time to %d.", val%24000)
}

func (t timeAddCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	if w == nil {
		o.Error("world unavailable")
		return
	}
	current := w.Time()
	next := (current + t.Value) % 24000
	if next < 0 {
		next += 24000
	}
	w.SetTime(next)
	o.Printf("Set time to %d.", next)
}

func (t timeQueryCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	if w == nil {
		o.Error("world unavailable")
		return
	}
	time := w.Time()
	switch strings.ToLower(string(t.Type)) {
	case "daytime", "gametime":
		o.Printf("%d", time%24000)
	case "day":
		o.Printf("%d", time/24000)
	default:
		o.Errort(cmd.MessageParameterInvalid, string(t.Type))
	}
}

func parseTimeValue(value string) (int, bool) {
	switch strings.ToLower(value) {
	case "day":
		return 1000, true
	case "night":
		return 13000, true
	case "noon":
		return 6000, true
	case "midnight":
		return 18000, true
	}
	if v, err := strconv.Atoi(value); err == nil {
		return v, true
	}
	return 0, false
}
