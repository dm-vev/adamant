package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// villagerHostileDistances ports VillagerHostilesSensor: the mobs villagers
// are scared of and how close they have to be.
var villagerHostileDistances = map[string]float64{
	"minecraft:drowned": 8, "minecraft:evoker": 12, "minecraft:husk": 8, "minecraft:illusioner": 12,
	"minecraft:pillager": 15, "minecraft:ravager": 12, "minecraft:vex": 8, "minecraft:vindicator": 10,
	"minecraft:zoglin": 10, "minecraft:zombie": 8, "minecraft:zombie_villager": 8,
}

// Panicking reports whether the villager panics.
func (v *Villager) Panicking() bool { return v.behaviour().br.isActive(activityPanic) }

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
	random := tx.RegionRandom()
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
