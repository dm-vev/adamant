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
	} else {
		dist = distanceToBox(eyes, cube.Box(0, 0, 0, 1, 1, 1).Translate(m.Pos().Vec3()))
		reach = javaBlockRange
		if creative {
			reach = javaCreativeBlockRange
		}
	}
	reach += ac.conf.Menu.Buffer
	if dist <= reach {
		return
	}
	switch p.violate(ac, anticheat.Menu, "distance", dist-reach, fmt.Sprintf("%.2f > %.2f from the menu", dist, reach)) {
	case anticheat.Cancel:
		p.closeMenu(p.tx, false)
	}
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
