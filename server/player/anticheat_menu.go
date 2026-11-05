package player

import (
	"errors"
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/anticheat"
)

// Java's interaction ranges, which Menu adds its buffer to: blocks, and
// entities, in survival and in game modes with the creative inventory.
const (
	javaBlockRange, javaCreativeBlockRange   = 4.5, 5.0
	javaEntityRange, javaCreativeEntityRange = 3.0, 5.0
)

// errMenuFlood is what the operations of a batch beyond the operations a
// player may make in a tick fail with.
var errMenuFlood = errors.New("anti-cheat: more menu operations in a tick than Menu allows")

// menuState is what Menu keeps of the batches of a client.
type menuState struct {
	tick    int64
	batches int
}

// checkMenuReach closes the menu of the player if it is out of reach, as
// Java's stillValid does every tick: the box of the block within Java's
// block range (4.5, 5 in creative) and the Buffer of Menu (4), the entity
// within its entity range (3, 5 in creative) and the buffer.
//
// The client of Bedrock opens a menu from farther than Java's range: it
// clicks a block 12 blocks away in creative, and BlockReach lets it. So a
// menu is never closed for a distance the client was let open it from: the
// reach of a block is the more of Java's with the buffer and that of
// BlockReach, and of an entity that of EntityReach, while the check is on.
// Without it, a creative player that opened a chest from 10 blocks had it
// closed in the same tick, which it saw as the chest not opening.
func (p *Player) checkMenuReach() {
	m := p.Menu()
	ac := p.anticheat()
	if m == nil || !p.checks(ac, anticheat.Menu) {
		return
	}
	eyes := entity.EyePosition(p)
	creative := p.GameMode().CreativeInventory()
	var dist, reach float64
	if h := m.Entity(); h != nil {
		e, ok := h.Entity(p.tx)
		if !ok {
			return
		}
		dist = distanceToBox(eyes, e.H().Type().BBox(e).Translate(e.Position()))
		reach = javaEntityRange
		if creative {
			reach = javaCreativeEntityRange
		}
		reach = p.menuReach(reach, anticheat.EntityReach, ac.conf.EntityReach)
	} else {
		dist = distanceToBox(eyes, cube.Box(0, 0, 0, 1, 1, 1).Translate(m.Pos().Vec3()))
		reach = javaBlockRange
		if creative {
			reach = javaCreativeBlockRange
		}
		reach = p.menuReach(reach, anticheat.BlockReach, ac.conf.BlockReach)
	}
	if dist <= reach {
		return
	}
	if p.violate(ac, anticheat.Menu, "distance", dist-reach, fmt.Sprintf("%.2f > %.2f from the menu", dist, reach)) == anticheat.Cancel {
		p.closeMenu(p.tx, false)
	}
}

// menuReach returns how far from a menu the player may be for it to stay
// open: java, the reach of Java for what the menu is open for, and the
// Buffer of Menu; or, with the check passed on for the player, the reach
// of its settings, if more.
func (p *Player) menuReach(java float64, check anticheat.Check, s anticheat.ReachSettings) float64 {
	ac := p.anticheat()
	reach := java + ac.conf.Menu.Buffer
	if p.checks(ac, check) {
		reach = max(reach, p.reachOf(s))
	}
	return reach
}

// menuRefusal returns what the batch of menu operations of the client of
// the player that starts now fails with: nil but beyond RequestsPerTick
// batches in a tick, a violation of Menu.
func (p *Player) menuRefusal() error {
	ac, end := p.beginAction()
	defer end()
	if !p.checks(ac, anticheat.Menu) {
		return nil
	}
	s := &ac.menu
	if s.tick != p.tickCount {
		s.tick, s.batches = p.tickCount, 0
	}
	s.batches++
	limit := ac.conf.Menu.RequestsPerTick
	if s.batches <= limit {
		return nil
	}
	if p.violate(ac, anticheat.Menu, "requests", float64(s.batches-limit), fmt.Sprintf("%d menu requests in a tick > %d", s.batches, limit)) == anticheat.Log {
		return nil
	}
	return errMenuFlood
}
