package entity

import (
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
)

// mobEffectDamage ports the effect checks of LivingEntity.hurtServer: Fire
// Resistance makes fire harmless and Resistance reduces the damage. It
// returns the damage left and whether the mob takes it.
func mobEffectDamage(m *EffectManager, damage float64, src world.DamageSource) (float64, bool) {
	if _, ok := m.Effect(effect.FireResistance); ok && src.Fire() {
		return 0, false
	}
	if r, ok := m.Effect(effect.Resistance); ok {
		damage *= effect.Resistance.Multiplier(src, r.Level())
	}
	return damage, true
}

// AddEffect adds an effect to the pig.
func (p *Pig) AddEffect(e effect.Effect) { p.behaviour().effects.Add(e, p) }

// RemoveEffect removes an effect from the pig.
func (p *Pig) RemoveEffect(t effect.Type) { p.behaviour().effects.Remove(t, p) }

// Effects returns the pig's active effects.
func (p *Pig) Effects() []effect.Effect { return p.behaviour().effects.Effects() }

// Speed returns the pig's movement speed.
func (p *Pig) Speed() float64 { return p.behaviour().speed }

// SetSpeed changes the pig's movement speed.
func (p *Pig) SetSpeed(v float64) { p.behaviour().speed = v }

// AddEffect adds an effect to the sheep.
func (s *Sheep) AddEffect(e effect.Effect) { s.behaviour().effects.Add(e, s) }

// RemoveEffect removes an effect from the sheep.
func (s *Sheep) RemoveEffect(t effect.Type) { s.behaviour().effects.Remove(t, s) }

// Effects returns the sheep's active effects.
func (s *Sheep) Effects() []effect.Effect { return s.behaviour().effects.Effects() }

// Speed returns the sheep's movement speed.
func (s *Sheep) Speed() float64 { return s.behaviour().speed }

// SetSpeed changes the sheep's movement speed.
func (s *Sheep) SetSpeed(v float64) { s.behaviour().speed = v }

// AddEffect adds an effect to the goat.
func (g *Goat) AddEffect(e effect.Effect) { g.behaviour().effects.Add(e, g) }

// RemoveEffect removes an effect from the goat.
func (g *Goat) RemoveEffect(t effect.Type) { g.behaviour().effects.Remove(t, g) }

// Effects returns the goat's active effects.
func (g *Goat) Effects() []effect.Effect { return g.behaviour().effects.Effects() }

// Speed returns the goat's movement speed.
func (g *Goat) Speed() float64 { return g.behaviour().speed }

// SetSpeed changes the goat's movement speed.
func (g *Goat) SetSpeed(v float64) { g.behaviour().speed = v }

// AddEffect adds an effect to the rabbit.
func (r *Rabbit) AddEffect(e effect.Effect) { r.behaviour().effects.Add(e, r) }

// RemoveEffect removes an effect from the rabbit.
func (r *Rabbit) RemoveEffect(t effect.Type) { r.behaviour().effects.Remove(t, r) }

// Effects returns the rabbit's active effects.
func (r *Rabbit) Effects() []effect.Effect { return r.behaviour().effects.Effects() }

// Speed returns the rabbit's movement speed.
func (r *Rabbit) Speed() float64 { return r.behaviour().speed }

// SetSpeed changes the rabbit's movement speed.
func (r *Rabbit) SetSpeed(v float64) { r.behaviour().speed = v }

// AddEffect adds an effect to the mooshroom.
func (m *Mooshroom) AddEffect(e effect.Effect) { m.behaviour().effects.Add(e, m) }

// RemoveEffect removes an effect from the mooshroom.
func (m *Mooshroom) RemoveEffect(t effect.Type) { m.behaviour().effects.Remove(t, m) }

// Effects returns the mooshroom's active effects.
func (m *Mooshroom) Effects() []effect.Effect { return m.behaviour().effects.Effects() }

// Speed returns the mooshroom's movement speed.
func (m *Mooshroom) Speed() float64 { return m.behaviour().speed }

// SetSpeed changes the mooshroom's movement speed.
func (m *Mooshroom) SetSpeed(v float64) { m.behaviour().speed = v }

// AddEffect adds an effect to the sniffer.
func (s *Sniffer) AddEffect(e effect.Effect) { s.behaviour().effects.Add(e, s) }

// RemoveEffect removes an effect from the sniffer.
func (s *Sniffer) RemoveEffect(t effect.Type) { s.behaviour().effects.Remove(t, s) }

// Effects returns the sniffer's active effects.
func (s *Sniffer) Effects() []effect.Effect { return s.behaviour().effects.Effects() }

// Speed returns the sniffer's movement speed.
func (s *Sniffer) Speed() float64 { return s.behaviour().speed }

// SetSpeed changes the sniffer's movement speed.
func (s *Sniffer) SetSpeed(v float64) { s.behaviour().speed = v }
