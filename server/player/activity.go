package player

import "time"

// LastActivity reports the time of the most recent packet received from the player.
func (p *Player) LastActivity() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.client().LastActivity()
}
