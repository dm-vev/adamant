package player

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	model "github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
)

// tickScoreboard updates the player's stat-driven objective scores and
// mirrors the sidebar once per second.
func (p *Player) tickScoreboard(tx *world.Tx, current int64) {
	if current%20 != 0 {
		return
	}
	m := tx.World().Scoreboard()
	if m.TracksCriteria() {
		name := p.Name()
		m.SetCriteria(model.Health, name, int(math.Ceil(p.Health())))
		m.SetCriteria(model.Food, name, p.Food())
		m.SetCriteria(model.Level, name, p.ExperienceLevel())
		m.SetCriteria(model.XP, name, p.Experience())
		m.SetCriteria(model.Air, name, p.airSupplyTicks)
		m.SetCriteria(model.Armor, name, p.armorPoints())
	}
	p.SyncScoreboard(m)
}

// SyncScoreboard sends the model's sidebar if it changed since the last send.
func (p *Player) SyncScoreboard(m *model.Model) {
	title, lines, ok := m.Sidebar()
	key := ""
	if ok {
		key = title + "\n" + strings.Join(lines, "\n")
	}
	if key == p.sidebar {
		return
	}
	p.sidebar = key
	if !ok {
		p.RemoveScoreboard()
		return
	}
	board := scoreboard.New(title)
	for _, line := range lines {
		_, _ = board.WriteString(line + "\n")
	}
	p.SendScoreboard(board)
}

func (p *Player) armorPoints() int {
	total := 0.0
	for _, stack := range p.Armour().Items() {
		if armour, ok := stack.Item().(interface{ DefencePoints() float64 }); ok {
			total += armour.DefencePoints()
		}
	}
	return int(total)
}

// awardDeathScores ports Player.die's deathCount and the killer's
// playerKillCount/totalKillCount awards.
func (p *Player) awardDeathScores(src world.DamageSource) {
	m := p.tx.World().Scoreboard()
	if !m.TracksCriteria() {
		return
	}
	m.AddCriteria(model.DeathCount, p.Name(), 1)
	if killer, ok := entity.DamageSourceKiller(src).(*Player); ok && killer != p {
		m.AddCriteria(model.PlayerKillCount, killer.Name(), 1)
		m.AddCriteria(model.TotalKillCount, killer.Name(), 1)
	}
}
