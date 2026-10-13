package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// CrammingDamageSource is dealt to an entity squeezed together with more
// than max_entity_cramming-1 other pushable entities. It bypasses armour.
type CrammingDamageSource struct{}

func (CrammingDamageSource) ReducedByArmour() bool     { return false }
func (CrammingDamageSource) ReducedByResistance() bool { return true }
func (CrammingDamageSource) Fire() bool                { return false }
func (CrammingDamageSource) IgnoreTotem() bool         { return false }

// TickEntityCollisions ports LivingEntity.pushEntities for every living
// entity in the world: each pushes the pushable entities overlapping its
// bounding box apart, and takes cramming damage when too many are packed
// together. Players push mobs but are not moved server-side, as the Bedrock
// client resolves its own collisions.
func TickEntityCollisions(tx *world.Tx) {
	maxCramming := tx.World().GameRuleInt("max_entity_cramming")
	for e := range tx.Entities() {
		l, ok := e.(Living)
		if !ok || !pushable(e) {
			continue
		}
		box := e.H().Type().BBox(e).Translate(e.Position())
		var others []world.Entity
		for other := range tx.EntitiesWithin(box.Grow(2)) {
			if other.H() == e.H() || !pushable(other) {
				continue
			}
			if other.H().Type().BBox(other).Translate(other.Position()).IntersectsWith(box) {
				others = append(others, other)
			}
		}
		if len(others) == 0 {
			continue
		}
		if maxCramming > 0 && len(others) > maxCramming-1 && rand.IntN(4) == 0 {
			count := 0
			for _, other := range others {
				if !isPassenger(other) {
					count++
				}
			}
			if count > maxCramming-1 {
				l.Hurt(6, CrammingDamageSource{})
			}
		}
		for _, other := range others {
			pushApart(tx, e, other)
		}
	}
}

// pushable ports Entity.isPushable: living entities that are alive, not
// spectating and not climbing, plus boats and minecarts.
func pushable(e world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:armor_stand":
		return false
	case "minecraft:boat", "minecraft:chest_boat", "minecraft:minecart", "minecraft:chest_minecart",
		"minecraft:hopper_minecart", "minecraft:tnt_minecart", "minecraft:command_block_minecart":
		return true
	}
	l, ok := e.(Living)
	if !ok || l.Dead() {
		return false
	}
	if m, ok := e.(interface{ GameMode() world.GameMode }); ok && m.GameMode() == world.GameModeSpectator {
		return false
	}
	return true
}

func isPassenger(e world.Entity) bool {
	r, ok := e.(Rider)
	return ok && r.Riding() != nil
}

// pushApart ports Entity.push(Entity): both entities are pushed away from
// each other horizontally by up to 0.05 blocks per tick.
func pushApart(tx *world.Tx, e, other world.Entity) {
	if sameVehicle(tx, e, other) {
		return
	}
	xa, za := other.Position()[0]-e.Position()[0], other.Position()[2]-e.Position()[2]
	dd := math.Max(math.Abs(xa), math.Abs(za))
	if dd < 0.01 {
		return
	}
	dd = math.Sqrt(dd)
	xa, za = xa/dd, za/dd
	pow := min(1/dd, 1)
	xa, za = xa*pow*0.05, za*pow*0.05
	if !isVehicle(e) {
		addPushVelocity(e, mgl64.Vec3{-xa, 0, -za})
	}
	if !isVehicle(other) {
		addPushVelocity(other, mgl64.Vec3{xa, 0, za})
	}
}

// addPushVelocity adds velocity to non-player entities; players move
// client-side.
func addPushVelocity(e world.Entity, v mgl64.Vec3) {
	// Passengers move with their vehicle; players move client-side.
	if e.H().Type().EncodeEntity() == "minecraft:player" || isPassenger(e) {
		return
	}
	if m, ok := e.(interface {
		Velocity() mgl64.Vec3
		SetVelocity(mgl64.Vec3)
	}); ok {
		m.SetVelocity(m.Velocity().Add(v))
	}
}

// sameVehicle ports Entity.isPassengerOfSameVehicle: entities sharing a root
// vehicle (including a vehicle and its own riders) never push each other.
func sameVehicle(tx *world.Tx, a, b world.Entity) bool {
	return rootVehicle(tx, a) == rootVehicle(tx, b)
}

func rootVehicle(tx *world.Tx, e world.Entity) *world.EntityHandle {
	h := e.H()
	for range 16 {
		r, ok := e.(Rider)
		if !ok || r.Riding() == nil {
			return h
		}
		next, ok := r.Riding().Entity(tx)
		if !ok {
			return r.Riding()
		}
		h, e = next.H(), next
	}
	return h
}

// isVehicle reports whether another entity is riding e.
func isVehicle(e world.Entity) bool {
	l, ok := e.(Linker)
	if !ok {
		return false
	}
	for _, link := range l.Links() {
		if link.Ridden == e.H() {
			return true
		}
	}
	return false
}
