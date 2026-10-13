package builtin

import (
	model "github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
)

// refreshScoreboard mirrors the model's sidebar onto every Bedrock viewer
// immediately after a command changes it.
func refreshScoreboard(tx *world.Tx, m *model.Model) {
	if tx == nil {
		return
	}
	for p := range onlinePlayers(tx) {
		p.SyncScoreboard(m)
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
