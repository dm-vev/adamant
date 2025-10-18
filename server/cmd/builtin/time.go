package builtin

import (
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

type timeSetValueCommand struct {
	Set   cmd.SubCommand `cmd:"set"`
	Value int            `cmd:"value"`
}

type timeSetPresetCommand struct {
	Set   cmd.SubCommand `cmd:"set"`
	Value timeSetPreset  `cmd:"value"`
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
		timeSetValueCommand{},
		timeSetPresetCommand{},
		timeAddCommand{},
		timeQueryCommand{},
	)
}

func (t timeSetValueCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	if w == nil {
		o.Error("world unavailable")
		return
	}
	val := t.Value % 24000
	if val < 0 {
		val += 24000
	}
	w.SetTime(val)
	o.Printf("Set time to %d.", val)
}

func (t timeSetPresetCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	if w == nil {
		o.Error("world unavailable")
		return
	}
	ticks, ok := presetTimeTicks[t.Value]
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, string(t.Value))
		return
	}
	w.SetTime(ticks)
	o.Printf("Set time to %d.", ticks)
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

var presetTimeTicks = map[timeSetPreset]int{
	"day":      1000,
	"noon":     6000,
	"night":    13000,
	"midnight": 18000,
}
