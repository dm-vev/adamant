package session

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// An entity is shown to a session within a range of its player, measured
// horizontally, that depends on its type, as Java tracks entities: most
// mobs within 8 chunks, items and orbs 6, projectiles 4, a few large or
// lasting ones 10 or 16, players as far as the chunks the session views.
// A chunk the session views but farther than the range holds the entity
// unseen, and the world does not move it for the session.
const defaultTrackingRange = 8 * 16

// trackingRanges are the ranges of the types whose range is not the
// default, in blocks.
var trackingRanges = map[string]float64{
	"minecraft:item": 6 * 16, "minecraft:xp_orb": 6 * 16,

	"minecraft:arrow": 4 * 16, "minecraft:thrown_trident": 4 * 16, "minecraft:snowball": 4 * 16,
	"minecraft:egg": 4 * 16, "minecraft:ender_pearl": 4 * 16, "minecraft:xp_bottle": 4 * 16,
	"minecraft:splash_potion": 4 * 16, "minecraft:lingering_potion": 4 * 16, "minecraft:fireball": 4 * 16,
	"minecraft:small_fireball": 4 * 16, "minecraft:dragon_fireball": 4 * 16, "minecraft:wither_skull": 4 * 16,
	"minecraft:wind_charge_projectile": 4 * 16, "minecraft:breeze_wind_charge": 4 * 16, "minecraft:llama_spit": 4 * 16,
	"minecraft:fireworks_rocket": 4 * 16, "minecraft:eye_of_ender_signal": 4 * 16, "minecraft:fishing_hook": 4 * 16,

	"minecraft:ender_dragon": 10 * 16, "minecraft:wither": 10 * 16, "minecraft:ghast": 10 * 16,
	"minecraft:happy_ghast": 10 * 16, "minecraft:iron_golem": 10 * 16, "minecraft:falling_block": 10 * 16,
	"minecraft:tnt": 10 * 16, "minecraft:painting": 10 * 16, "minecraft:leash_knot": 10 * 16,
	"minecraft:armor_stand": 10 * 16, "minecraft:area_effect_cloud": 10 * 16, "minecraft:boat": 10 * 16,
	"minecraft:chest_boat": 10 * 16,

	"minecraft:warden": 16 * 16, "minecraft:lightning_bolt": 16 * 16, "minecraft:ender_crystal": 16 * 16,
}

const (
	// interestInterval is the ticks between the checks of which entities
	// are within range of the player of a session.
	interestInterval = 5
	// interestMargin is how far past its range an entity shown goes before
	// it is hidden, so that one at the edge does not come and go.
	interestMargin = 8.0
)

// trackingRange returns how far horizontally from the player of a session
// an entity is shown: that of its type, or of the farthest-seen entity
// riding it, as Java takes the range of a vehicle from its passengers.
func trackingRange(e world.Entity) float64 {
	if _, ok := e.(Controllable); ok {
		return math.Inf(1)
	}
	name := e.H().Type().EncodeEntity()
	if n, ok := e.H().Type().(interface{ NetworkEncodeEntity() string }); ok {
		// The type the client knows the entity by.
		name = n.NetworkEncodeEntity()
	}
	r, ok := trackingRanges[name]
	if !ok {
		r = defaultTrackingRange
	}
	if linker, ok := e.(entity.Linker); ok {
		for _, link := range linker.Links() {
			if link.Rider != nil && link.Rider != e.H() {
				// A player or anything else riding: the vehicle is seen as
				// far as its rider, which is a player at most.
				r = math.Inf(1)
			}
		}
	}
	return r
}

// withinRange reports whether e is within its range of pos, widened by
// margin.
func withinRange(e world.Entity, pos mgl64.Vec3, margin float64) bool {
	r := trackingRange(e)
	if math.IsInf(r, 1) {
		return true
	}
	r += margin
	d := e.Position().Sub(pos)
	return d[0]*d[0]+d[2]*d[2] <= r*r
}

// outOfRange reports whether the world shows the session an entity farther
// from its player than its range, recording it to be shown once it comes
// within range.
func (s *Session) outOfRange(e world.Entity) bool {
	pos := s.viewPos.Load()
	if pos == nil || e.H() == s.ent || withinRange(e, *pos, 0) {
		return false
	}
	s.entityMutex.Lock()
	if s.farEntities == nil {
		s.farEntities = make(map[*world.EntityHandle]struct{})
	}
	s.farEntities[e.H()] = struct{}{}
	s.hidesFar.Store(true)
	s.entityMutex.Unlock()
	entitiesOutOfRange.Inc()
	return true
}

// updateInterest is called on the ticks of the world of the player of the
// session, at pos: every interestInterval ticks it hides the entities shown
// that went out of range and shows those that came within it.
func (s *Session) updateInterest(tx *world.Tx, pos mgl64.Vec3) {
	s.viewPos.Store(&pos)
	if s.interestTick++; s.interestTick%interestInterval != 0 {
		return
	}
	s.entityMutex.RLock()
	shown, far := s.interestShown[:0], s.interestFar[:0]
	for h := range s.entityRuntimeIDs {
		if h != s.ent {
			shown = append(shown, h)
		}
	}
	for h := range s.farEntities {
		far = append(far, h)
	}
	s.entityMutex.RUnlock()
	for _, h := range shown {
		if e, ok := h.Entity(tx); ok && !s.entityHidden(e) && !withinRange(e, pos, interestMargin) {
			s.hideFar(e)
		}
	}
	for _, h := range far {
		if e, ok := h.Entity(tx); ok && withinRange(e, pos, 0) {
			s.showNear(e)
		}
	}
	clear(shown)
	clear(far)
	s.interestShown, s.interestFar = shown[:0], far[:0]
}

// hideFar hides an entity shown that went out of range, keeping it to be
// shown again once it comes back.
func (s *Session) hideFar(e world.Entity) {
	s.entityMutex.Lock()
	id, shown := s.entityRuntimeIDs[e.H()]
	if shown {
		delete(s.entityRuntimeIDs, e.H())
		delete(s.entities, id)
		delete(s.deathAnimations, id)
		delete(s.justShown, e.H())
	}
	if s.farEntities == nil {
		s.farEntities = make(map[*world.EntityHandle]struct{})
	}
	s.farEntities[e.H()] = struct{}{}
	s.hidesFar.Store(true)
	s.entityMutex.Unlock()
	if shown {
		s.writePacket(&v2193.RemoveActorPacket{EntityUniqueID: s.handleUniqueID(e.H())})
	}
}

// showNear shows an entity kept out of range that came within it, as the
// world shows an entity anew.
func (s *Session) showNear(e world.Entity) {
	s.entityMutex.Lock()
	delete(s.farEntities, e.H())
	s.hidesFar.Store(len(s.farEntities) > 0)
	s.entityMutex.Unlock()
	if s.entityHidden(e) {
		return
	}
	e.H().ResendMove()
	s.ViewEntity(e)
	s.ViewEntityState(e)
	s.ViewEntityItems(e)
	s.ViewEntityArmour(e)
}

// forgetFar drops an entity the world no longer shows the session from
// those kept out of range.
func (s *Session) forgetFar(h *world.EntityHandle) {
	if !s.hidesFar.Load() {
		return
	}
	s.entityMutex.Lock()
	delete(s.farEntities, h)
	s.hidesFar.Store(len(s.farEntities) > 0)
	s.entityMutex.Unlock()
}
