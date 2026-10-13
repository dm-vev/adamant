package builtin

import (
	"fmt"
	"sort"

	playerscoreboard "github.com/df-mc/dragonfly/server/player/scoreboard"
	model "github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
)

// refreshScoreboard mirrors the model's sidebar onto every Bedrock viewer.
func refreshScoreboard(tx *world.Tx, m *model.Model) {
	if tx == nil {
		return
	}
	name, ok := m.DisplayObjective("sidebar")
	if !ok {
		for p := range onlinePlayers(tx) {
			p.RemoveScoreboard()
		}
		return
	}
	objective, ok := m.Objective(name)
	if !ok {
		return
	}
	entries := make([]string, 0, len(objective.Scores))
	for holder, score := range objective.Scores {
		entries = append(entries, fmt.Sprintf("%s: %d", holder, score))
	}
	sort.Strings(entries)
	if len(entries) > 15 {
		entries = entries[len(entries)-15:]
	}
	board := playerscoreboard.New(objective.DisplayName)
	for _, entry := range entries {
		_, _ = board.WriteString(entry + "\n")
	}
	for p := range onlinePlayers(tx) {
		p.SendScoreboard(board)
	}
}

// refreshTeamNameTags applies team prefix and suffix to online members.
func refreshTeamNameTags(tx *world.Tx, m *model.Model) {
	if tx == nil {
		return
	}
	for p := range onlinePlayers(tx) {
		if team, ok := m.TeamFor(p.Name()); ok {
			p.SetNameTag(teamColorCode(team.Color) + team.Prefix + p.Name() + team.Suffix)
		}
	}
}

func teamColorCode(color string) string {
	switch color {
	case "black":
		return "§0"
	case "dark_blue":
		return "§1"
	case "dark_green":
		return "§2"
	case "dark_aqua":
		return "§3"
	case "dark_red":
		return "§4"
	case "dark_purple":
		return "§5"
	case "gold":
		return "§6"
	case "gray":
		return "§7"
	case "dark_gray":
		return "§8"
	case "blue":
		return "§9"
	case "green":
		return "§a"
	case "aqua":
		return "§b"
	case "red":
		return "§c"
	case "light_purple":
		return "§d"
	case "yellow":
		return "§e"
	case "white":
		return "§f"
	default:
		return ""
	}
}
