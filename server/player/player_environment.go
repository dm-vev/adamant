package player

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// airState is the air the player breathes and the fire that burns it.
type airState struct {
	fireTicks         int64
	breathing         bool
	airSupplyTicks    int
	maxAirSupplyTicks int
}

// airDrain takes a tick of air from a player that cannot breathe and reports
// whether it has run out of it for good: at -20 the supply returns to 0 and
// the player takes drowning damage.
func (s *airState) airDrain() bool {
	if s.airSupplyTicks -= 1; s.airSupplyTicks <= -20 {
		s.airSupplyTicks = 0
		return true
	}
	return false
}

// airRefillable reports whether the supply of a player that can breathe is
// below its maximum and not yet back to it.
func (s *airState) airRefillable() bool {
	return !s.breathing && s.airSupplyTicks < s.maxAirSupplyTicks
}

// airRefill gives back 5 ticks of air, up to the maximum, and sets breathing
// once the maximum is reached.
func (s *airState) airRefill() {
	s.airSupplyTicks = min(s.airSupplyTicks+5, s.maxAirSupplyTicks)
	s.breathing = s.airSupplyTicks == s.maxAirSupplyTicks
}

// airLeft returns the remaining air supply.
func (s *airState) airLeft() time.Duration {
	return time.Duration(s.airSupplyTicks) * time.Second / 20
}

// airSetLeft sets the remaining air supply, in whole ticks.
func (s *airState) airSetLeft(duration time.Duration) {
	s.airSupplyTicks = int(duration.Milliseconds() / 50)
}

// airMax returns the maximum air supply.
func (s *airState) airMax() time.Duration {
	return time.Duration(s.maxAirSupplyTicks) * time.Second / 20
}

// airSetMax sets the maximum air supply, in whole ticks.
func (s *airState) airSetMax(duration time.Duration) {
	s.maxAirSupplyTicks = int(duration.Milliseconds() / 50)
}

// airNeedless reports whether a player needs no air from what it is and has
// on it, before the water and the blocks around its head are read.
func airNeedless(canTakeDamage, waterBreathing, conduitPower, nautilus bool) bool {
	return !canTakeDamage || waterBreathing || conduitPower || nautilus
}

// airHeldByNautilus reports whether Breath of the Nautilus holds the supply
// where it is: it does unless Water Breathing or Conduit Power refill it.
// airFrozen asks it only underwater.
func airHeldByNautilus(nautilus, waterBreathing, conduitPower bool) bool {
	return nautilus && !waterBreathing && !conduitPower
}

// Splash is called when a water bottle splashes onto the player.
func (p *Player) Splash(*world.Tx, mgl64.Vec3) {
	if d := p.OnFireDuration(); d.Seconds() <= 0 {
		return
	}
	p.Extinguish()
}

// FireProof checks if the Player is currently fireproof. True is returned if the player has a fireResistance effect or
// if it is in creative mode.
func (p *Player) FireProof() bool {
	if _, ok := p.Effect(effect.FireResistance); ok {
		return true
	}
	return !p.GameMode().AllowsTakingDamage()
}

// OnFireDuration ...
func (p *Player) OnFireDuration() time.Duration {
	return time.Duration(p.fireTicks) * time.Second / 20
}

// SetOnFire ...
func (p *Player) SetOnFire(duration time.Duration) {
	ticks := int64(duration.Seconds() * 20)
	if level := p.Armour().HighestEnchantmentLevel(enchantment.FireProtection); level > 0 {
		ticks -= int64(math.Floor(float64(ticks) * float64(level) * 0.15))
	}

	duration = time.Duration(ticks) * time.Second / 20
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleSetOnFire(ctx, &duration); ctx.Cancelled() {
		return
	}

	ticks = int64(duration.Seconds() * 20)
	p.fireTicks = ticks
	p.updateState()
}

// Extinguish ...
func (p *Player) Extinguish() {
	p.SetOnFire(0)
}

// EnterPowderSnow marks the player as inside powder snow this tick, freezing
// it further unless it wears leather armour or is a spectator.
func (p *Player) EnterPowderSnow() { entity.EnterPowderSnow(p, p.data) }

// FrozenTicks returns how far the player froze in powder snow, up to 140.
func (p *Player) FrozenTicks() int { return p.data.FrozenTicks }

// SetFrozenTicks sets how far the player froze, as Entity.setTicksFrozen.
func (p *Player) SetFrozenTicks(ticks int) {
	if p.data.FrozenTicks != ticks {
		p.data.FrozenTicks = max(0, ticks)
		p.updateState()
	}
}

// FreezingStrength returns the freezing overlay strength from 0 to 1.
func (p *Player) FreezingStrength() float32 { return entity.FreezingStrength(p.data) }

func (p *Player) WearingCreakingDisguise() bool {
	helmet := p.Armour().Helmet()
	if helmet.Empty() {
		return false
	}
	name, _ := helmet.Item().EncodeItem()
	return name == "minecraft:carved_pumpkin"
}

// touchingWater reports if water touches the box of the player, as
// Entity.isInWater.
func (p *Player) touchingWater() bool {
	box := p.H().Type().BBox(p).Translate(p.Position()).Grow(-.001)
	for x := int(math.Floor(box.Min()[0])); x <= int(math.Floor(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])); y <= int(math.Floor(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])); z <= int(math.Floor(box.Max()[2])); z++ {
				pos := cube.Pos{x, y, z}
				if !p.movementChunkReady(pos) {
					// Looking for water in a chunk the world lacks would load it,
					// and wait for its generation.
					continue
				}
				if l, ok := p.tx.Liquid(pos); ok && l.LiquidType() == "water" {
					return true
				}
			}
		}
	}
	return false
}

// tickAirSupply tick's the player's air supply, consuming it when underwater, and replenishing it when out of water.
func (p *Player) tickAirSupply() {
	if !p.canBreathe() {
		if r, ok := p.Armour().Helmet().Enchantment(enchantment.Respiration); ok && rand.Float64() < enchantment.Respiration.Chance(r.Level()) {
			// respiration grants a chance to avoid drowning damage every tick.
			return
		}
		if p.airDrain() {
			p.Hurt(2, entity.DrowningDamageSource{})
		}
		p.breathing = false
		p.updateState()
	} else if p.airRefillable() && !p.airFrozen() {
		p.airRefill()
		p.updateState()
	}
}

// AirSupply returns the player's remaining air supply.
func (p *Player) AirSupply() time.Duration {
	return p.airLeft()
}

// SetAirSupply sets the player's remaining air supply.
func (p *Player) SetAirSupply(duration time.Duration) {
	p.airSetLeft(duration)
	p.updateState()
}

// MaxAirSupply returns the player's maximum air supply.
func (p *Player) MaxAirSupply() time.Duration {
	return p.airMax()
}

// SetMaxAirSupply sets the player's maximum air supply.
func (p *Player) SetMaxAirSupply(duration time.Duration) {
	p.airSetMax(duration)
	p.updateState()
}

// canBreathe returns true if the player can currently breathe.
func (p *Player) canBreathe() bool {
	canTakeDamage := p.GameMode().AllowsTakingDamage()
	_, waterBreathing := p.effects.Effect(effect.WaterBreathing)
	_, conduitPower := p.effects.Effect(effect.ConduitPower)
	_, nautilus := p.effects.Effect(effect.BreathOfTheNautilus)
	return airNeedless(canTakeDamage, waterBreathing, conduitPower, nautilus) || (!p.insideOfWater() && !p.insideOfSolid())
}

// airFrozen ports MobEffectUtil.shouldEffectsRefillAirsupply: underwater,
// Breath of the Nautilus holds the air supply where it is unless Water
// Breathing or Conduit Power refill it.
func (p *Player) airFrozen() bool {
	_, nautilus := p.effects.Effect(effect.BreathOfTheNautilus)
	_, waterBreathing := p.effects.Effect(effect.WaterBreathing)
	_, conduitPower := p.effects.Effect(effect.ConduitPower)
	return airHeldByNautilus(nautilus, waterBreathing, conduitPower) && p.insideOfWater()
}

// breathingDistanceBelowEyes is the lowest distance the player can be in water and still be able to breathe based on
// the player's eye height.
const breathingDistanceBelowEyes = 0.11111111

// insideOfWater returns true if the player is currently underwater.
func (p *Player) insideOfWater() bool {
	pos := cube.PosFromVec3(entity.EyePosition(p))
	if !p.movementChunkReady(pos) {
		return false
	}
	if l, ok := p.tx.Liquid(pos); ok {
		if _, ok := l.(block.Water); ok {
			d := float64(l.SpreadDecay()) + 1
			if l.LiquidFalling() {
				d = 1
			}
			return p.Position().Y() < (pos.Side(cube.FaceUp).Vec3().Y())-(d/9-breathingDistanceBelowEyes)
		}
	}
	return false
}

// insideOfSolid returns true if the player is inside a solid block.
func (p *Player) insideOfSolid() bool {
	if p.GameMode() == world.GameModeSpectator {
		return false
	}
	pos := cube.PosFromVec3(entity.EyePosition(p))
	b, loaded := p.tx.BlockLoaded(pos)
	if !loaded {
		return false
	}
	box := p.handle.Type().BBox(p).Translate(p.Position())

	_, solid := b.Model().(model.Solid)
	if !solid {
		// Not solid.
		return false
	}
	d, diffuses := b.(block.LightDiffuser)
	if diffuses && d.LightDiffusionLevel() == 0 {
		// Transparent.
		return false
	}
	if immune, ok := b.(block.NonSuffocating); ok && immune.PreventsSuffocation() {
		return false
	}
	for _, blockBox := range b.Model().BBox(pos, p.tx) {
		if blockBox.Translate(pos.Vec3()).IntersectsWith(box) {
			return true
		}
	}
	return false
}

// Breathing checks if the player is currently able to breathe. If it's underwater and the player does not
// have the water breathing or conduit power effect, this returns false.
// If the player is in creative or spectator mode, Breathing always returns true.
func (p *Player) Breathing() bool {
	if !p.GameMode().AllowsTakingDamage() {
		return true
	}
	if _, ok := p.Effect(effect.WaterBreathing); ok {
		return true
	}
	if _, ok := p.Effect(effect.ConduitPower); ok {
		return true
	}
	if _, ok := p.Effect(effect.BreathOfTheNautilus); ok {
		return true
	}
	return p.breathing
}
