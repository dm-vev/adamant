package player

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/go-gl/mathgl/mgl64"
)

// The reach a player had before the anti-cheat, from the eyes to the
// centre of a block or the position of an entity: in survival, and in game
// modes with the creative inventory.
const (
	defaultReach         = 8.0
	defaultCreativeReach = 14.0
)

// reachOf returns the reach of the settings passed for the player: the
// creative reach in game modes with the creative inventory, the survival
// reach in others, with the buffer.
func (p *Player) reachOf(s anticheat.ReachSettings) float64 {
	if p.GameMode().CreativeInventory() {
		return s.Creative + s.Buffer
	}
	return s.Survival + s.Buffer
}

// distanceToBox returns the distance from the point passed to the nearest
// point of box.
func distanceToBox(from mgl64.Vec3, box cube.BBox) float64 {
	var d mgl64.Vec3
	for i := range 3 {
		d[i] = max(box.Min()[i]-from[i], 0, from[i]-box.Max()[i])
	}
	return d.Len()
}

// canReachBlock reports whether the player reaches the block at pos, by
// BlockReach of its anti-cheat: from the eyes to the centre of the block,
// or to its box, within the reach of its game mode. A player that is not
// checked reaches as before the anti-cheat, 8 blocks to the centre and 14
// in creative; with BlockReach off, or an exempt player, the reach is not
// checked. A block out of the reach of what its client asks is a violation
// of BlockReach, which by default cancels the action; a plugin's action is
// refused, as it always was.
func (p *Player) canReachBlock(pos cube.Pos) bool {
	if p.Dead() || !p.GameMode().AllowsInteraction() {
		return false
	}
	eyes := entity.EyePosition(p)
	ac := p.anticheat()
	if ac == nil {
		return p.canReach(pos.Vec3Centre())
	}
	if !ac.conf.Active(anticheat.BlockReach) || p.exemptFrom(ac, anticheat.BlockReach) {
		return true
	}
	s := ac.conf.BlockReach
	dist := eyes.Sub(pos.Vec3Centre()).Len()
	if s.Measure == "box" {
		dist = distanceToBox(eyes, cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3()))
	}
	reach := p.reachOf(s)
	if dist <= reach {
		return true
	}
	if !p.clientInput() {
		return false
	}
	switch p.violate(ac, anticheat.BlockReach, "distance", dist-reach, fmt.Sprintf("%.2f > %.2f to %v", dist, reach, pos)) {
	case anticheat.Log:
		return true
	}
	return false
}

// canReachPoint reports whether the player reaches the point passed within
// the reach of its game mode as before the anti-cheat.
func (p *Player) canReachPoint(pos mgl64.Vec3) bool {
	dist := entity.EyePosition(p).Sub(pos).Len()
	return !p.Dead() && p.GameMode().AllowsInteraction() &&
		(dist <= defaultReach || (dist <= defaultCreativeReach && p.GameMode().CreativeInventory()))
}

// blockOf returns the block a point is in.
func blockOf(v mgl64.Vec3) cube.Pos {
	return cube.Pos{int(math.Floor(v[0])), int(math.Floor(v[1])), int(math.Floor(v[2]))}
}
