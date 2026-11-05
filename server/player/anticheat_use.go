package player

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
)

// refuseFastUse judges a use of an item without a cooldown the client of
// the player asks for, such as a snowball, an egg or a splash potion: a
// client waits MinTicks (4) ticks between two, Java's right-click delay,
// and Burst (2) uses may come closer, as a double tap on a touch screen
// does. A use beyond is a violation of FastUse, logged by default. It
// reports whether the use is refused.
func (p *Player) refuseFastUse(it world.Item) bool {
	ac, end := p.beginAction()
	defer end()
	if !p.checks(ac, anticheat.FastUse) {
		return false
	}
	conf := ac.conf.FastUse
	if conf.MinTicks <= 0 {
		return false
	}
	perSecond := float64(anticheat.TicksPerSecond) / float64(conf.MinTicks)
	if ac.uses.Take(p.inputArrival(ac), perSecond, conf.Burst) {
		return false
	}
	name, _ := it.EncodeItem()
	return p.violate(ac, anticheat.FastUse, "rate", 1, fmt.Sprintf("%s more often than every %d ticks", name, conf.MinTicks)) != anticheat.Log
}
