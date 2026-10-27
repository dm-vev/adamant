package entity

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// sensing caches what a mob saw during a tick, ported from Java's Sensing.
type sensing struct {
	seen, unseen map[*world.EntityHandle]struct{}
}

func (s *sensing) tick() {
	clear(s.seen)
	clear(s.unseen)
}

// HasLineOfSight reports if the mob sees an entity, as
// Sensing.hasLineOfSight: once per entity per tick.
func (m *Mob) HasLineOfSight(e world.Entity) bool {
	// A stunned or roaring ravager sees nothing.
	if b, ok := m.Behaviour().(interface{ blind() bool }); ok && b.blind() {
		return false
	}
	s := &m.AI.sense
	h := e.H()
	if _, ok := s.seen[h]; ok {
		return true
	}
	if _, ok := s.unseen[h]; ok {
		return false
	}
	if s.seen == nil {
		s.seen, s.unseen = make(map[*world.EntityHandle]struct{}, 4), make(map[*world.EntityHandle]struct{}, 4)
	}
	visible := lineOfSight(m.tx, mgl64.Vec3{m.data.Pos[0], m.EyeY(), m.data.Pos[2]}, mgl64.Vec3{e.Position()[0], entityEyeY(e), e.Position()[2]})
	if visible {
		s.seen[h] = struct{}{}
	} else {
		s.unseen[h] = struct{}{}
	}
	return visible
}

// lineOfSight reports if no block collides with the line between two eyes
// within 128 blocks, as LivingEntity.hasLineOfSight.
func lineOfSight(tx *world.Tx, from, to mgl64.Vec3) bool {
	if to.Sub(from).Len() > 128 {
		return false
	}
	return clipMiss(tx, from, to)
}

// clipMiss reports if a line misses the collision shapes of all blocks, as a
// clip with ClipContext.Block.COLLIDER and no fluid misses.
func clipMiss(tx *world.Tx, from, to mgl64.Vec3) bool {
	miss := true
	trace.TraverseBlocks(from, to, func(pos cube.Pos) bool {
		b := pathBlockAt(tx, pos)
		if _, hit := trace.BlockIntercept(pos, tx, b, from, to, trace.CollisionShape); hit {
			miss = false
			return false
		}
		return true
	})
	return miss
}

// clipMissFluids reports if a line from one position to another passes no
// collision box of a block and no liquid, as a Level.clip with
// ClipContext.Fluid.ANY misses.
func clipMissFluids(tx *world.Tx, from, to mgl64.Vec3) bool {
	miss := true
	trace.TraverseBlocks(from, to, func(pos cube.Pos) bool {
		b := pathBlockAt(tx, pos)
		if _, hit := trace.BlockIntercept(pos, tx, b, from, to, trace.CollisionShape); hit {
			miss = false
			return false
		}
		if l, ok := tx.Liquid(pos); ok {
			box := cube.Box(0, 0, 0, 1, liquidHeight(tx, pos, l), 1).Translate(pos.Vec3())
			if _, hit := trace.BBoxIntercept(box, from, to); hit {
				miss = false
				return false
			}
		}
		return true
	})
	return miss
}

// targeting are the conditions an entity must meet for a mob to target it,
// ported from Java's TargetingConditions.
type targeting struct {
	combat      bool
	rangeLimit  float64
	ignoreSight bool
	ignoreInvis bool
	selector    func(m *Mob, target world.Entity) bool
}

// forCombat returns conditions for a mob attacking a target.
func forCombat() targeting { return targeting{combat: true, rangeLimit: -1} }

// forNonCombat returns conditions for a mob merely noticing a target.
func forNonCombat() targeting { return targeting{rangeLimit: -1} }

// test ports TargetingConditions.test.
func (c targeting) test(m *Mob, target world.Entity) bool {
	if target == nil || target.H() == m.H() {
		return false
	}
	if !entityAlive(target) || spectator(target) {
		return false
	}
	if c.selector != nil && !c.selector(m, target) {
		return false
	}
	if c.combat && (!m.CanAttack(target) || alliedTo(m, target)) {
		return false
	}
	if c.rangeLimit > 0 {
		modifier := 1.0
		if !c.ignoreInvis {
			modifier = visibilityPercent(target)
		}
		visibility := math.Max(c.rangeLimit*modifier, 2)
		if m.DistanceToSqr(target) > visibility*visibility {
			return false
		}
	}
	if !c.ignoreSight && !m.HasLineOfSight(target) {
		return false
	}
	return true
}

// alliedTo reports if two entities are on the same side, as
// Entity.isAlliedTo: raiders stick together.
func alliedTo(m *Mob, target world.Entity) bool {
	if a, ok := m.Behaviour().(interface{ alliedTo(*Mob, world.Entity) bool }); ok {
		return a.alliedTo(m, target)
	}
	return false
}

// nearestTarget returns the nearest of the candidates that meet the
// conditions, as Level.getNearestEntity measured from the eyes of the mob.
func nearestTarget(m *Mob, c targeting, candidates func(yield func(world.Entity) bool)) world.Entity {
	eye := mgl64.Vec3{m.data.Pos[0], m.EyeY(), m.data.Pos[2]}
	var best world.Entity
	bestDist := -1.0
	candidates(func(e world.Entity) bool {
		if !c.test(m, e) {
			return true
		}
		d := e.Position().Sub(eye).LenSqr()
		if bestDist == -1 || d < bestDist {
			best, bestDist = e, d
		}
		return true
	})
	return best
}

// nearestPlayer returns the nearest player that meets the conditions, as
// Level.getNearestPlayer.
func nearestPlayer(m *Mob, c targeting) world.Entity {
	players := m.tx.Players()
	if c.rangeLimit > 0 {
		// The range of the conditions is never more than max(rangeLimit, 2).
		players = m.tx.PlayersNear(m.data.Pos, max(c.rangeLimit, 2))
	}
	return nearestTarget(m, c, players)
}

// nearestOfTypes returns the nearest entity within a box of an entity type
// for which isType returns true that meets the conditions.
func nearestOfTypes(m *Mob, c targeting, box cube.BBox, isType func(world.Entity) bool) world.Entity {
	return nearestTarget(m, c, func(yield func(world.Entity) bool) {
		for e := range m.tx.EntitiesWithin(box) {
			if isType(e) && !yield(e) {
				return
			}
		}
	})
}

// entityTypes is a set of entity type names. Its is method selects the
// entities of those types; goals given the set itself also let the world
// skip entities of other types before opening them, as Java's
// getEntitiesOfClass visits only the class asked for.
type entityTypes []string

// is reports whether e has one of the types of the set.
func (t entityTypes) is(e world.Entity) bool {
	return slices.Contains(t, e.H().Type().EncodeEntity())
}

var (
	playerTypes           = entityTypes{"minecraft:player"}
	itemEntityTypes       = entityTypes{"minecraft:item"}
	villagerTypes         = entityTypes{"minecraft:villager", "minecraft:villager_v2", "minecraft:wandering_trader"}
	raiderTypes           = entityTypes{"minecraft:evoker", "minecraft:vindicator", "minecraft:pillager", "minecraft:ravager", "minecraft:witch"}
	abstractSkeletonTypes = entityTypes{"minecraft:skeleton", "minecraft:stray", "minecraft:wither_skeleton", "minecraft:bogged", "minecraft:parched"}
)

// nearestOfEntityTypes is nearestOfTypes for the entities of a set of
// types, visiting only those.
func nearestOfEntityTypes(m *Mob, c targeting, box cube.BBox, types entityTypes, isType func(world.Entity) bool) world.Entity {
	return nearestTarget(m, c, func(yield func(world.Entity) bool) {
		for e := range m.tx.EntitiesOfTypesWithin(box, types) {
			if isType(e) && !yield(e) {
				return
			}
		}
	})
}

// entityIs returns a function that reports if an entity has one of the
// entity type names passed.
func entityIs(names ...string) func(world.Entity) bool {
	return func(e world.Entity) bool {
		id := e.H().Type().EncodeEntity()
		for _, name := range names {
			if id == name {
				return true
			}
		}
		return false
	}
}
