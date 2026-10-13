package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
)

// villagerHostileDistances ports VillagerHostilesSensor: the mobs villagers
// are scared of and how close they have to be.
var villagerHostileDistances = map[string]float64{
	"minecraft:drowned": 8, "minecraft:evoker": 12, "minecraft:husk": 8, "minecraft:illusioner": 12,
	"minecraft:pillager": 15, "minecraft:ravager": 12, "minecraft:vex": 8, "minecraft:vindicator": 10,
	"minecraft:zoglin": 10, "minecraft:zombie": 8, "minecraft:zombie_villager": 8,
}

// villagerPanicState is a villager's PANIC activity: HurtBySensor's memory of
// the last damage and its attacker.
type villagerPanicState struct {
	hurt      panicState
	hurtBy    *world.EntityHandle
	panicking bool
	navigator Navigator
}

// nearestHostile ports VillagerHostilesSensor: the nearest visible mob the
// villager is scared of.
func (v *Villager) nearestHostile(tx *world.Tx) world.Entity {
	var nearest world.Entity
	best := 0.0
	for other := range tx.EntitiesWithin(cube.Box(-15, -15, -15, 15, 15, 15).Translate(v.Position())) {
		distance, ok := villagerHostileDistances[other.H().Type().EncodeEntity()]
		if !ok {
			continue
		}
		if l, living := other.(Living); living && l.Dead() {
			continue
		}
		d := other.Position().Sub(v.Position()).LenSqr()
		if d > distance*distance || nearest != nil && d >= best || !lineVisible(v.Ent, tx, other) {
			continue
		}
		nearest, best = other, d
	}
	return nearest
}

// tickPanic runs VillagerPanicTrigger, VillagerCalmDown and the panic
// package: a hurt villager or one near a hostile mob runs away from it at
// 1.5 times its speed, and every five seconds panicking villagers that
// slept recently summon an iron golem if three of them agree. It reports
// whether the villager panics.
func (v *Villager) tickPanic(tx *world.Tx, current int64) bool {
	p := &v.behaviour().panic
	hurt := p.hurt.source != nil && current-p.hurt.stamp <= 40
	hostile := v.nearestHostile(tx)
	var attacker world.Entity
	if p.hurtBy != nil {
		attacker, _ = p.hurtBy.Entity(tx)
	}
	if !p.panicking {
		if !hurt && hostile == nil {
			return false
		}
		p.panicking, p.navigator = true, Navigator{}
	} else if !hurt && hostile == nil && (attacker == nil || attacker.Position().Sub(v.Position()).LenSqr() > 36) {
		p.panicking, p.hurtBy = false, nil
		return false
	}
	if current%100 == 0 {
		v.spawnGolemIfNeeded(tx, current, 3)
	}
	if p.navigator.Index >= len(p.navigator.Path) {
		v.choosePanicTarget(tx, hostile, attacker)
	}
	vel, rot, _ := steer(v.Ent, &p.navigator, groundSpeed(.5, .75), .6)
	v.data.Vel, v.data.Rot = vel, rot
	return true
}

// choosePanicTarget ports SetWalkTargetAwayFrom for the nearest hostile and
// the attacker within six blocks, and VillageBoundRandomStroll otherwise.
func (v *Villager) choosePanicTarget(tx *world.Tx, hostile, attacker world.Entity) {
	random := java26.NewLegacyRandom(rand.Int64())
	m := randomPosMob{tx: tx, pos: v.Position(), home: noHome, profile: GroundProfile{Width: .6, Height: 1.95}, random: &random}
	var goal cube.Pos
	ok := false
	switch {
	case hostile != nil && hostile.Position().Sub(v.Position()).LenSqr() < 36:
		goal, ok = m.randomPosAway(16, 7, hostile.Position())
	case attacker != nil && attacker.Position().Sub(v.Position()).LenSqr() < 36:
		goal, ok = m.randomPosAway(16, 7, attacker.Position())
	default:
		goal, ok = m.randomPos(2, 2)
	}
	if !ok {
		return
	}
	if path := FindGroundPath(tx, cube.PosFromVec3(v.Position()), goal, m.profile); len(path) != 0 {
		v.behaviour().panic.navigator = Navigator{Path: path, Target: goal, HasTarget: true, LastComputeTick: navigatorTick(v.Ent)}
	}
}

// Panicking reports whether the villager panics.
func (v *Villager) Panicking() bool { return v.behaviour().panic.panicking }

// senseGolems ports GolemSensor: every ten seconds a villager notices an iron
// golem within 16 blocks and wants no new golem for 30 seconds.
func (v *Villager) senseGolems(tx *world.Tx, current int64) {
	if current%200 != 0 {
		return
	}
	for e := range tx.EntitiesWithin(cube.Box(-16, -16, -16, 16, 16, 16).Translate(v.Position())) {
		if e.H().Type() == IronGolemType {
			v.MarkGolemDetected(current)
			return
		}
	}
}

// spawnGolemIfNeeded ports Villager.spawnGolemIfNeeded: if enough villagers
// within ten blocks want a golem, one is summoned and they all notice it.
func (v *Villager) spawnGolemIfNeeded(tx *world.Tx, current int64, needed int) {
	if !v.WantsToSpawnGolem(current) {
		return
	}
	var nearby []*Villager
	wanting := 0
	for e := range tx.EntitiesWithin(VillagerType.BBox(v).Translate(v.Position()).Grow(10)) {
		other, ok := e.(*Villager)
		if !ok {
			continue
		}
		nearby = append(nearby, other)
		if wanting < 5 && other.WantsToSpawnGolem(current) {
			wanting++
		}
	}
	if wanting < needed || !summonIronGolem(tx, cube.PosFromVec3(v.Position())) {
		return
	}
	for _, other := range nearby {
		other.MarkGolemDetected(current)
	}
}

// summonIronGolem ports SpawnUtil.trySpawnMob with the LEGACY_IRON_GOLEM
// strategy: ten attempts within eight blocks horizontally, searching six
// blocks up and down for ground to stand on.
func summonIronGolem(tx *world.Tx, pos cube.Pos) bool {
	random := tx.JavaRandom()
	for range 10 {
		search := pos.Add(cube.Pos{int(random.NextInt(17)) - 8, 6, int(random.NextInt(17)) - 8})
		if !tx.World().WithinBorder(search) {
			continue
		}
		above := tx.Block(search)
		found := false
		for range 13 {
			search = search.Side(cube.FaceDown)
			current := tx.Block(search)
			if legacyIronGolemFloor(tx, search, current, above) {
				search, found = search.Side(cube.FaceUp), true
				break
			}
			above = current
		}
		if !found || !ironGolemFits(tx, search) {
			continue
		}
		tx.AddEntity(NewIronGolem(world.EntitySpawnOpts{Position: search.Vec3Middle(), SpawnReason: "mob_summoned"}))
		return true
	}
	return false
}

// legacyIronGolemFloor ports SpawnUtil.Strategy.LEGACY_IRON_GOLEM: a solid
// block, or powder snow, below air or liquid that is none of the blocks
// golems were never summoned on.
func legacyIronGolemFloor(tx *world.Tx, pos cube.Pos, b, above world.Block) bool {
	switch b.(type) {
	case block.Cobweb, block.Cactus, block.GlassPane, block.StainedGlassPane, block.StainedGlass, block.Leaves, block.Conduit,
		block.Ice, block.TNT, block.Glowstone, block.Beacon, block.SeaLantern, block.FrostedIce, block.TintedGlass, block.Glass:
		return false
	}
	_, air := above.(block.Air)
	_, liquid := tx.Liquid(pos.Side(cube.FaceUp))
	if !air && !liquid {
		return false
	}
	_, snow := b.(block.PowderSnow)
	return snow || len(b.Model().BBox(pos, tx)) != 0
}

// ironGolemFits ports IronGolem.checkSpawnObstruction: ground to stand on, a
// body's height of free space without liquid above the feet, and no other
// entity in the way.
func ironGolemFits(tx *world.Tx, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	if !tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) {
		return false
	}
	for y := range 3 {
		p := pos.Add(cube.Pos{0, y, 0})
		if !isPassable(tx, p) {
			return false
		}
		if _, liquid := tx.Liquid(p); liquid && y > 0 {
			return false
		}
	}
	box := IronGolemType.BBox(nil).Translate(pos.Vec3Middle())
	for e := range tx.EntitiesWithin(box.Grow(2)) {
		if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			return false
		}
	}
	return true
}
