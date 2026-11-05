package player

import "time"

// menuLogEvery is the least time between two debug lines about requests to
// open a menu that was already open.
const menuLogEvery = 5 * time.Second

// logMenuIgnored writes a debug line that a request to open a menu of the
// player was ignored as that menu is open already, at most once every
// menuLogEvery: a menu that is open on the server but not on the client
// repeats the request, and the line shows it. kind is what the menu is
// open for, at the position passed.
func (p *Player) logMenuIgnored(kind string, at any) {
	now := time.Now()
	if !p.menuIgnoredAt.IsZero() && now.Sub(p.menuIgnoredAt) < menuLogEvery {
		p.menuIgnoredCount++
		return
	}
	skipped := p.menuIgnoredCount
	p.menuIgnoredAt, p.menuIgnoredCount = now, 0
	p.log.Debug("menu: opening ignored, the menu is open already", "kind", kind, "at", at, "player", p.Name(), "suppressed", skipped)
}
