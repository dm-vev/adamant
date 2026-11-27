package builtin

import (
	"github.com/dm-vev/adamant/server/player"
	model "github.com/dm-vev/adamant/server/scoreboard"
	"github.com/dm-vev/adamant/server/world"
)

// eachOnlinePlayer invokes fn for every player in tx's world synchronously and
// schedules fn on every other server dimension without blocking.
func eachOnlinePlayer(tx *world.Tx, fn func(*player.Player)) {
	if tx == nil {
		return
	}
	for p := range onlinePlayers(tx) {
		fn(p)
	}
	if commandServer == nil {
		return
	}
	origin := tx.World()
	var seen [3]*world.World
	n := 0
	for _, w := range [3]*world.World{commandServer.World(), commandServer.Nether(), commandServer.End()} {
		if w == nil || w == origin {
			continue
		}
		dup := false
		for i := range n {
			if seen[i] == w {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		seen[n] = w
		n++
		w.Do(func(wtx *world.Tx) {
			for p := range onlinePlayers(wtx) {
				fn(p)
			}
		})
	}
}

// refreshScoreboard mirrors the model's sidebar onto every Bedrock viewer
// immediately after a command changes it.
func refreshScoreboard(tx *world.Tx, m *model.Model) {
	eachOnlinePlayer(tx, func(p *player.Player) {
		p.SyncScoreboard(m)
	})
}

// refreshTeamNameTags applies team prefix and suffix to online members.
func refreshTeamNameTags(tx *world.Tx, m *model.Model) {
	eachOnlinePlayer(tx, func(p *player.Player) {
		if team, ok := m.TeamFor(p.Name()); ok {
			p.SetNameTag(teamColorCode(team.Color) + team.Prefix + p.Name() + team.Suffix)
		}
	})
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
