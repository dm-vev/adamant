package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
)

// fallDamageImmune is Java's #fall_damage_immune entity type tag.
var fallDamageImmune = map[string]bool{
	"minecraft:copper_golem": true, "minecraft:iron_golem": true, "minecraft:snow_golem": true,
	"minecraft:shulker": true, "minecraft:allay": true, "minecraft:bat": true, "minecraft:bee": true,
	"minecraft:blaze": true, "minecraft:cat": true, "minecraft:chicken": true, "minecraft:ghast": true,
	"minecraft:happy_ghast": true, "minecraft:phantom": true, "minecraft:magma_cube": true,
	"minecraft:ocelot": true, "minecraft:parrot": true, "minecraft:wither": true, "minecraft:breeze": true,
}

// horseFallTypes are Java's AbstractHorse subclasses, which fall six blocks
// safely and take half damage.
var horseFallTypes = map[string]bool{
	"minecraft:horse": true, "minecraft:donkey": true, "minecraft:mule": true,
	"minecraft:skeleton_horse": true, "minecraft:zombie_horse": true, "minecraft:llama": true,
	"minecraft:trader_llama": true, "minecraft:camel": true, "minecraft:camel_husk": true,
}

// FallDistance returns how far the entity has fallen since it last stood on
// the ground.
func (e *Ent) FallDistance() float64 { return e.data.FallDistance }

// ResetFallDistance resets the fall distance, as water, ladders and cobwebs
// do.
func (e *Ent) ResetFallDistance() { e.data.FallDistance = 0 }

// SetFallDistance sets how far the entity counts as having fallen, as
// geysers capping it do.
func (e *Ent) SetFallDistance(distance float64) { e.data.FallDistance = distance }

// tickFall ports Entity.checkFallDamage for the movement of a tick: falling
// out of water builds up fall distance, which the entity lands with when the
// movement ends on the ground. Boats, like AbstractBoat, never land with it.
func (e *Ent) tickFall(tx *world.Tx, m *Movement) {
	if _, boat := e.data.Data.(*BoatBehaviour); boat || e.data.Riding != nil || e.fallSlowed() {
		e.data.FallDistance = 0
		return
	}
	if dy := m.dpos[1]; dy < 0 {
		if l, ok := tx.Liquid(cube.PosFromVec3(e.data.Pos)); !ok || l.LiquidType() != "water" {
			e.data.FallDistance -= dy
		}
	}
	if !m.onGround {
		return
	}
	if distance := e.data.FallDistance; distance > 0 {
		e.fallOn(tx, distance)
	}
	e.data.FallDistance = 0
}

// fallSlowed reports whether Slow Falling or Levitation keeps resetting the
// fall distance.
func (e *Ent) fallSlowed() bool {
	l, ok := e.living().(interface {
		Effect(effect.Type) (effect.Effect, bool)
	})
	if !ok {
		return false
	}
	_, slow := l.Effect(effect.SlowFalling)
	_, levitating := l.Effect(effect.Levitation)
	return slow || levitating
}

// fallOn ports Block.fallOn for the block the entity landed on: blocks like
// beds and hay bales change the distance, and the entity takes fall damage.
func (e *Ent) fallOn(tx *world.Tx, distance float64) {
	target := e.living()
	if pos, lander, ok := landedOn(tx, target); ok {
		lander.EntityLand(pos, tx, target, &distance)
	}
	if f, ok := target.(interface {
		CauseFallDamage(distance, multiplier float64, src world.DamageSource)
	}); ok {
		f.CauseFallDamage(distance, 1, FallDamageSource{})
	}
}

// landedOn returns the first block.EntityLander under the entity's box.
func landedOn(tx *world.Tx, e world.Entity) (cube.Pos, block.EntityLander, bool) {
	box := e.H().Type().BBox(e).Translate(e.Position())
	y := int(math.Floor(box.Min()[1] - 0.0001))
	horizontal := box.Grow(-0.0001)
	low, high := cube.PosFromVec3(horizontal.Min()), cube.PosFromVec3(horizontal.Max())
	for x := low[0]; x <= high[0]; x++ {
		for z := low[2]; z <= high[2]; z++ {
			pos := cube.Pos{x, y, z}
			if lander, ok := tx.Block(pos).(block.EntityLander); ok {
				return pos, lander, true
			}
		}
	}
	return cube.Pos{}, nil, false
}

// CauseFallDamage ports LivingEntity.causeFallDamage: a living entity takes
// the blocks fallen beyond its safe fall distance of 3, raised by Jump Boost,
// times multiplier as damage from src, or FallDamageSource if nil, and its
// passengers fall with it. Fall damage immune entities take no damage and
// pass nothing on.
func (e *Ent) CauseFallDamage(distance, multiplier float64, src world.DamageSource) {
	id := e.H().Type().EncodeEntity()
	if fallDamageImmune[id] {
		return
	}
	if src == nil {
		src = FallDamageSource{}
	}
	if s, ok := e.data.Data.(*speciesState); ok && s.kind == creeperSpecies {
		s.swell = min(s.swell+int(distance*1.5), s.fuse-5)
	}
	target := e.living()
	if living, ok := target.(interface {
		Hurt(float64, world.DamageSource) (float64, bool)
	}); ok {
		if dmg := fallDamage(target, id, distance, multiplier); dmg > 0 && !(isLlama(id) && distance < 6) {
			living.Hurt(dmg, src)
		}
	}
	for _, passenger := range passengersOf(e.tx, e) {
		if f, ok := passenger.(interface {
			CauseFallDamage(distance, multiplier float64, src world.DamageSource)
		}); ok {
			f.CauseFallDamage(distance, multiplier, src)
		}
	}
}

func isLlama(id string) bool { return id == "minecraft:llama" || id == "minecraft:trader_llama" }

// fallDamage ports calculateFallDamage with the SAFE_FALL_DISTANCE and
// FALL_DAMAGE_MULTIPLIER attributes of horses and foxes and the reductions of
// goats and frogs.
func fallDamage(e world.Entity, id string, distance, multiplier float64) float64 {
	safe, scale := 3.0, 1.0
	switch {
	case horseFallTypes[id]:
		safe, scale = 6, 0.5
	case id == "minecraft:fox":
		safe = 5
	}
	if l, ok := e.(interface {
		Effect(effect.Type) (effect.Effect, bool)
	}); ok {
		if boost, ok := l.Effect(effect.JumpBoost); ok {
			safe += float64(boost.Level())
		}
	}
	dmg := math.Floor((distance + 1e-6 - safe) * multiplier * scale)
	switch id {
	case "minecraft:goat":
		dmg -= 10
	case "minecraft:frog":
		dmg -= 5
	}
	return dmg
}

// passengersOf returns the loaded entities riding the vehicle.
func passengersOf(tx *world.Tx, vehicle world.Entity) (passengers []world.Entity) {
	if tx == nil {
		return nil
	}
	for e := range tx.EntitiesWithin(vehicle.H().Type().BBox(vehicle).Translate(vehicle.Position()).Grow(3)) {
		if r, ok := e.(Rider); ok && r.Riding() == vehicle.H() {
			passengers = append(passengers, e)
		}
	}
	return passengers
}
