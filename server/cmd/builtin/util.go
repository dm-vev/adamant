package builtin

import (
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
)

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

func (gameModeValue) Type() string { return "GameMode" }

func (gameModeValue) Options(cmd.Source) []string {
	return []string{"survival", "creative", "adventure", "spectator", "0", "1", "2", "3", "s", "c", "a", "sp", "spectate"}
}

// timeSetPreset is an enum backing the `time set` presets shown in the vanilla client.
type timeSetPreset string

func (timeSetPreset) Type() string { return "TimeSpec" }

func (timeSetPreset) Options(cmd.Source) []string {
	return []string{"day", "noon", "night", "midnight"}
}
