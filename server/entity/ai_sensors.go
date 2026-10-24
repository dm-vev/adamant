package entity

import (
	"sort"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the sensors of net.minecraft.world.entity.ai.sensing that
// brains of many species share.

// nearestLivingEntitySensor ports NearestLivingEntitySensor: the living
// entities within the follow range of a mob, nearest first, and which of
// them it sees.
type nearestLivingEntitySensor struct{}

func (nearestLivingEntitySensor) scanRate() int { return 0 }
func (nearestLivingEntitySensor) requires() []memoryType {
	return []memoryType{memNearestLivingEntities, memNearestVisibleLivingEntities}
}

func (nearestLivingEntitySensor) doTick(m *Mob, b *Brain) {
	r := m.AI.FollowRange
	box := m.BBox().Grow(r)
	type found struct {
		h    *world.EntityHandle
		dist float64
	}
	var all []found
	for e := range m.tx.EntitiesWithin(box.Grow(2)) {
		if e.H() == m.H() || !entityAlive(e) {
			continue
		}
		if _, living := e.(Living); !living || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			continue
		}
		all = append(all, found{e.H(), m.DistanceToSqr(e)})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].dist < all[j].dist })
	handles := make([]*world.EntityHandle, len(all))
	for i, f := range all {
		handles[i] = f.h
	}
	b.set(memNearestLivingEntities, handles)
	b.set(memNearestVisibleLivingEntities, &nearestVisibleLiving{entities: handles, seen: map[*world.EntityHandle]bool{}, mob: m.H()})
}

// nearestLiving returns the living entities near the mob, nearest first.
func nearestLiving(m *Mob) []world.Entity {
	v, ok := m.Brain().get(memNearestLivingEntities)
	if !ok {
		return nil
	}
	var out []world.Entity
	for _, h := range v.([]*world.EntityHandle) {
		if e, ok := h.Entity(m.tx); ok {
			out = append(out, e)
		}
	}
	return out
}

// sensorAttackable ports Sensor.isEntityAttackable: the mob may attack the
// entity within its follow range, seeing it unless it is its attack target
// already.
func sensorAttackable(m *Mob, e world.Entity) bool {
	c := forCombat()
	c.rangeLimit = m.AI.FollowRange
	if v, ok := m.Brain().get(memAttackTarget); ok && v == e.H() {
		c.ignoreInvis = true
	}
	return c.test(m, e)
}

// hurtBySensor ports HurtBySensor: what last hurt the mob and the living
// entity that dealt it.
type hurtBySensor struct{}

func (hurtBySensor) scanRate() int          { return 0 }
func (hurtBySensor) requires() []memoryType { return []memoryType{memHurtBy, memHurtByEntity} }

func (hurtBySensor) doTick(m *Mob, b *Brain) {
	if src := m.LastDamage(); src != nil {
		b.set(memHurtBy, src)
		if attacker := mobKiller(src); attacker != nil {
			if _, living := attacker.(Living); living {
				b.setEntity(memHurtByEntity, attacker)
			}
		}
	} else {
		b.erase(memHurtBy)
	}
	if v, ok := b.get(memHurtByEntity); ok {
		if e, ok := v.(*world.EntityHandle).Entity(m.tx); !ok || !entityAlive(e) {
			b.erase(memHurtByEntity)
		}
	}
}

// temptingSensor ports TemptingSensor: the nearest player within the tempt
// range of the mob holding what tempts it.
type temptingSensor struct {
	items func(m *Mob, s item.Stack) bool
	// rangeLimit is the tempt range attribute, 10 if zero.
	rangeLimit float64
}

func (temptingSensor) scanRate() int          { return 0 }
func (temptingSensor) requires() []memoryType { return []memoryType{memTemptingPlayer} }

func (s temptingSensor) doTick(m *Mob, b *Brain) {
	c := forNonCombat()
	c.ignoreSight, c.rangeLimit = true, 10
	if s.rangeLimit > 0 {
		c.rangeLimit = s.rangeLimit
	}
	var best world.Entity
	bestDist := 0.0
	for p := range m.tx.Players() {
		if spectator(p) || !c.test(m, p) || ridingOn(m.tx, p, m.H()) {
			continue
		}
		u, ok := p.(item.User)
		if !ok {
			continue
		}
		main, off := u.HeldItems()
		if !s.items(m, main) && !s.items(m, off) {
			continue
		}
		if d := m.DistanceToSqr(p); best == nil || d < bestDist {
			best, bestDist = p, d
		}
	}
	b.setEntity(memTemptingPlayer, best)
}

// animalTemptation is the temptation of TemptingSensor.forAnimal: the food
// of the animal.
func animalTemptation(m *Mob, s item.Stack) bool {
	a, ok := m.Self.(breeder)
	return ok && a.breedingFood(s)
}

// adultSensor ports AdultSensor: the nearest adult of the kind of the mob it
// sees.
type adultSensor struct{}

func (adultSensor) scanRate() int { return 0 }
func (adultSensor) requires() []memoryType {
	return []memoryType{memNearestVisibleAdult, memNearestVisibleLivingEntities}
}

func (adultSensor) doTick(m *Mob, b *Brain) {
	v := visibleLiving(m)
	if v == nil {
		return
	}
	b.setEntity(memNearestVisibleAdult, v.findClosest(m, func(e world.Entity) bool {
		return e.H().Type() == m.H().Type() && !mobBaby(e)
	}))
}

// mobSensor ports MobSensor: while ready, the brain remembers for ttl ticks
// that one of the entities near the mob is one test picks out.
type mobSensor struct {
	rate  int
	test  func(m *Mob, e world.Entity) bool
	ready func(m *Mob) bool
	mem   memoryType
	ttl   int64
}

func (s mobSensor) scanRate() int          { return s.rate }
func (s mobSensor) requires() []memoryType { return []memoryType{memNearestLivingEntities, s.mem} }

func (s mobSensor) doTick(m *Mob, b *Brain) {
	if !s.ready(m) {
		b.erase(s.mem)
		return
	}
	if _, ok := b.get(memNearestLivingEntities); !ok {
		return
	}
	for _, e := range nearestLiving(m) {
		if s.test(m, e) {
			b.setWithExpiry(s.mem, true, s.ttl)
			return
		}
	}
}

// isInWaterSensor ports IsInWaterSensor.
type isInWaterSensor struct{}

func (isInWaterSensor) scanRate() int          { return 0 }
func (isInWaterSensor) requires() []memoryType { return []memoryType{memIsInWater} }
func (isInWaterSensor) doTick(m *Mob, b *Brain) {
	if m.InWater() {
		b.set(memIsInWater, true)
	} else {
		b.erase(memIsInWater)
	}
}

// playerSensor ports PlayerSensor: the players within the follow range of
// the mob, nearest first, the nearest it sees and the nearest it sees and
// may attack.
type playerSensor struct{}

func (playerSensor) scanRate() int { return 0 }
func (playerSensor) requires() []memoryType {
	return []memoryType{memNearestPlayers, memNearestVisiblePlayer, memNearestVisibleAttackablePlayer}
}

func (playerSensor) doTick(m *Mob, b *Brain) {
	r := m.AI.FollowRange
	type found struct {
		e    world.Entity
		dist float64
	}
	var players []found
	for p := range m.tx.Players() {
		if spectator(p) || !entityAlive(p) {
			continue
		}
		if d := m.DistanceToSqr(p); d < r*r {
			players = append(players, found{p, d})
		}
	}
	sort.SliceStable(players, func(i, j int) bool { return players[i].dist < players[j].dist })
	handles := make([]*world.EntityHandle, len(players))
	for i, p := range players {
		handles[i] = p.e.H()
	}
	b.set(memNearestPlayers, handles)
	targetable := forNonCombat()
	targetable.rangeLimit = r
	attackable := forCombat()
	attackable.rangeLimit = r
	var visible, attack world.Entity
	for _, p := range players {
		if visible == nil && targetable.test(m, p.e) {
			visible = p.e
		}
		if attack == nil && attackable.test(m, p.e) {
			attack = p.e
		}
	}
	b.setEntity(memNearestVisiblePlayer, visible)
	b.setEntity(memNearestVisibleAttackablePlayer, attack)
}

// nearestItemSensor ports NearestItemSensor: the nearest item entity within
// 32 blocks the mob wants and sees.
type nearestItemSensor struct {
	wants func(m *Mob, s item.Stack) bool
}

func (nearestItemSensor) scanRate() int          { return 0 }
func (nearestItemSensor) requires() []memoryType { return []memoryType{memNearestVisibleWantedItem} }

func (s nearestItemSensor) doTick(m *Mob, b *Brain) {
	box := m.BBox().GrowVec3(mgl64.Vec3{32, 16, 32})
	var best world.Entity
	bestDist := 0.0
	for e := range m.tx.EntitiesWithin(box) {
		stack, ok := pickableItem(e)
		if !ok || !s.wants(m, stack) || m.DistanceToSqr(e) >= 32*32 || !m.HasLineOfSight(e) {
			continue
		}
		if d := m.DistanceToSqr(e); best == nil || d < bestDist {
			best, bestDist = e, d
		}
	}
	b.setEntity(memNearestVisibleWantedItem, best)
}
