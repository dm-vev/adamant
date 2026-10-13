package builtin

import (
	"iter"
	"reflect"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// onlinePlayers yields the players in the transaction's world.
func onlinePlayers(tx *world.Tx) iter.Seq[*player.Player] {
	return func(yield func(*player.Player) bool) {
		for e := range tx.Players() {
			if p, ok := e.(*player.Player); ok && !yield(p) {
				return
			}
		}
	}
}

// singleTarget enforces Java's single-entity selector arguments on the
// framework's multi-target parameter type.
func singleTarget(targets []cmd.Target, o *cmd.Output) (cmd.Target, bool) {
	if len(targets) != 1 {
		if len(targets) == 0 {
			o.Errort(cmd.MessageNoTargets)
		} else {
			o.Errorf("Only one entity is allowed, but the provided selector allows more than one")
		}
		return nil, false
	}
	return targets[0], true
}

type namedSource interface {
	Name() string
}

// sourceName returns a user facing name for the source invoking a command.
func sourceName(src cmd.Source) string {
	if n, ok := src.(namedSource); ok {
		return n.Name()
	}
	return "Server"
}

// playersFromTargets filters a slice of command targets down to players and removes duplicates.
func playersFromTargets(targets []cmd.Target) []*player.Player {
	if len(targets) == 0 {
		return nil
	}
	seen := make(map[*player.Player]struct{}, len(targets))
	players := make([]*player.Player, 0, len(targets))
	for _, target := range targets {
		if p, ok := target.(*player.Player); ok {
			if _, exists := seen[p]; exists {
				continue
			}
			seen[p] = struct{}{}
			players = append(players, p)
		}
	}
	return players
}

// joinNames joins player names into a readable list.
func joinNames(players []*player.Player) string {
	switch len(players) {
	case 0:
		return ""
	case 1:
		return players[0].Name()
	}
	names := make([]string, 0, len(players))
	for _, p := range players {
		names = append(names, p.Name())
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// gameModeValue exposes the vanilla GameMode enum so the client can show rich command hints.
type gameModeValue string

var gameModeOptions = []string{"survival", "creative", "adventure", "spectator"}

func (gameModeValue) Type() string { return "GameMode" }

func (gameModeValue) Parse(line *cmd.Line, v reflect.Value) error {
	arg, ok := line.Next()
	if !ok {
		return line.UsageError()
	}
	_, alias, ok := parseGameMode(arg)
	if !ok {
		return cmd.MessageParameterInvalid.F(arg)
	}
	v.SetString(alias)
	return nil
}

func (gameModeValue) Options(cmd.Source) []string {
	return gameModeOptions
}

// timeSetPreset is an enum backing the `time set` presets shown in the vanilla client.
type timeSetPreset string

func (timeSetPreset) Type() string { return "TimeSpec" }

var timeSetPresets = []string{"day", "noon", "night", "midnight"}

func (timeSetPreset) Options(cmd.Source) []string {
	return timeSetPresets
}
