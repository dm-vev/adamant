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

	"minecraft:player": math.Inf(1),
}

const (
	// interestInterval is the ticks between the checks of whether the
	// entities kept out of range came within it, which are shown soon.
	interestInterval = 5
	// interestRound is the ticks over which every entity shown is checked
	// once for having gone out of range, a part each tick: one shown a
	// while longer costs only its packets.
	interestRound = 20
	// interestMargin is how far past its range an entity shown goes before
	// it is hidden, so that one at the edge does not come and go.
	interestMargin = 8.0
)

// typeRange returns how far horizontally from the player of a session an
// entity of a type is shown.
func typeRange(t world.EntityType) float64 {
	name := t.EncodeEntity()
	if n, ok := t.(interface{ NetworkEncodeEntity() string }); ok {
		// The type the client knows the entity by.
		name = n.NetworkEncodeEntity()
	}
	if r, ok := trackingRanges[name]; ok {
		return r
	}
	return defaultTrackingRange
}

// withinRange reports whether e is within its range of pos, widened by
// margin. An entity ridden is seen as far as its rider, which may be a
// player, as Java takes the range of a vehicle from its passengers.
func withinRange(e world.Entity, pos mgl64.Vec3, margin float64) bool {
	return withinRangeOf(e, typeRange(e.H().Type()), pos, margin)
}

// withinRangeOf is withinRange of an entity of range r.
func withinRangeOf(e world.Entity, r float64, pos mgl64.Vec3, margin float64) bool {
	if math.IsInf(r, 1) {
		return true
	}
	r += margin
	if d := e.Position().Sub(pos); d[0]*d[0]+d[2]*d[2] <= r*r {
		return true
	}
	if _, ok := e.(Controllable); ok {
		return true
	}
	if linker, ok := e.(entity.Linker); ok {
		for _, link := range linker.Links() {
			if link.Rider != nil && link.Rider != e.H() {
				return true
			}
		}
	}
	return false
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
// session, at pos: it hides the entities shown that went out of range, a
// part of them each tick, and every interestInterval ticks shows those
// kept out of range that came within it.
func (s *Session) updateInterest(tx *world.Tx, pos mgl64.Vec3) {
	s.viewPos.Store(&pos)
	part := s.interestTick % interestRound
	if part == 0 {
		// The entities to check over the next round. Players and the like
		// are seen as far as the chunks: their positions are not looked up.
		s.entityMutex.RLock()
		shown := s.interestShown[:0]
		for h := range s.entityRuntimeIDs {
			if r := typeRange(h.Type()); h != s.ent && !math.IsInf(r, 1) {
				shown = append(shown, rangedEntity{h: h, r: r})
			}
		}
		s.entityMutex.RUnlock()
		s.interestShown = shown
	}
	n := len(s.interestShown)
	for _, re := range s.interestShown[part*n/interestRound : (part+1)*n/interestRound] {
		if e, ok := re.h.Entity(tx); ok && !withinRangeOf(e, re.r, pos, interestMargin) && !s.entityHidden(e) {
			s.hideFar(e)
		}
	}
	if part == interestRound-1 {
		clear(s.interestShown)
	}
	s.interestTick++
	if s.interestTick%interestInterval != 0 || !s.hidesFar.Load() {
		return
	}
	s.entityMutex.RLock()
	far := s.interestFar[:0]
	for h := range s.farEntities {
		far = append(far, h)
	}
	s.entityMutex.RUnlock()
	for _, h := range far {
		if e, ok := h.Entity(tx); ok && withinRange(e, pos, 0) {
			s.showNear(e)
		}
	}
	clear(far)
	s.interestFar = far[:0]
}

// rangedEntity is an entity shown and its range.
type rangedEntity struct {
	h *world.EntityHandle
	r float64
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
