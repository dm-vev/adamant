package player

import "time"

// LastActivity reports the time of the most recent packet received from the player.
func (p *Player) LastActivity() time.Time {
	if p == nil || p.session() == nil {
		return time.Time{}
	}
	return p.session().LastActivity()
}
