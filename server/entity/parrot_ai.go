package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's Parrot and ShoulderRidingEntity.

func (b *parrotBehaviour) tamableState() *tamable { return &b.tamable }

// wantsToAttack ports TamableAnimal.wantsToAttack, which parrots keep.
func (b *parrotBehaviour) wantsToAttack(*Mob, world.Entity, world.Entity) bool { return true }

// canFlyToOwner ports Parrot.canFlyToOwner.
func (b *parrotBehaviour) canFlyToOwner() bool { return true }

// omnidirectionalAirMover ports Parrot.omnidirectionalAirMover.
func (b *parrotBehaviour) omnidirectionalAirMover() bool { return true }

// registerGoals ports Parrot.registerGoals and the navigation and move
// control of its constructor.
func (b *parrotBehaviour) registerGoals(m *Mob) {
	ai := m.AI
	ai.nav.SetFlying()
	ai.nav.SetCanOpenDoors(false)
	ai.nav.SetCanFloat(true)
	ai.moveOverride = flyingMoveControl{maxTurn: 10}
	ai.SetPathfindingMalus(PathFireInNeighbour, -1)
	ai.SetPathfindingMalus(PathFire, -1)
	ai.SetPathfindingMalus(PathCocoa, -1)
	b.panic = NewPanicGoal(1.25)
	ai.Goals.Add(0, tamableAnimalPanicGoal{b.panic})
	ai.Goals.Add(0, NewFloatGoal(m))
	ai.Goals.Add(1, NewLookAtPlayerGoal(8))
	ai.Goals.Add(2, newSitWhenOrderedToGoal())
	ai.Goals.Add(2, newFollowOwnerGoal(1, 5, 1))
	wander := NewRandomStrollGoal(1)
	wander.Position = b.wanderPosition
	ai.Goals.Add(2, wander)
	ai.Goals.Add(3, &landOnOwnersShoulderGoal{parrot: b})
	ai.Goals.Add(3, newFollowMobGoal(1, 3, 7))
}

// beforeAiStep ports what Parrot.aiStep does before the AI: a parrot away
// from the jukebox it danced to stops dancing, and it now and then imitates
// a mob nearby.
func (b *parrotBehaviour) beforeAiStep(m *Mob) {
	if j := b.jukebox; j == nil || j.Vec3Centre().Sub(m.data.Pos).LenSqr() >= 3.46*3.46 || !isJukebox(m.tx.Block(*j)) {
		b.party, b.jukebox = false, nil
	}
	if m.Random().NextInt(400) == 0 {
		imitateNearbyMobs(m.tx, m.Self, m.Random())
	}
}

func isJukebox(b world.Block) bool {
	_, ok := b.(block.Jukebox)
	return ok
}

// afterAiStep ports Parrot.calculateFlapping: a parrot flaps to fall slowly.
func (b *parrotBehaviour) afterAiStep(m *Mob) {
	if !m.OnGround() && m.data.Vel[1] < 0 {
		m.data.Vel[1] *= .6
	}
}

// imitateNearbyMobs ports Parrot.imitateNearbyMobs: half the time, a parrot
// makes the sound of one of the mobs within 20 blocks it imitates.
func imitateNearbyMobs(tx *world.Tx, e world.Entity, r interface {
	NextInt(int32) int32
	NextFloat() float32
}) bool {
	if l, ok := e.(Living); ok && l.Dead() || r.NextInt(2) != 0 {
		return false
	}
	box := e.H().Type().BBox(e).Translate(e.Position()).Grow(20)
	var sounds []string
	for other := range tx.EntitiesWithin(box) {
		if name, ok := parrotImitations[other.H().Type().EncodeEntity()]; ok && isMobEntity(other) {
			sounds = append(sounds, name)
		}
	}
	if len(sounds) == 0 {
		return false
	}
	name := sounds[r.NextInt(int32(len(sounds)))]
	tx.PlaySound(e.Position(), sound.Custom{Name: name, Volume: .7, Pitch: float64((r.NextFloat()-r.NextFloat())*.2 + 1)})
	return true
}

// parrotImitations ports Parrot.MOB_SOUND_MAP. Bedrock parrots imitate happy
// ghasts, which Java leaves silent.
var parrotImitations = map[string]string{
	"minecraft:blaze": "entity.parrot.imitate.blaze", "minecraft:bogged": "entity.parrot.imitate.bogged",
	"minecraft:breeze": "entity.parrot.imitate.breeze", "minecraft:camel_husk": "entity.parrot.imitate.camel_husk",
	"minecraft:cave_spider": "entity.parrot.imitate.spider", "minecraft:creaking": "entity.parrot.imitate.creaking",
	"minecraft:creeper": "entity.parrot.imitate.creeper", "minecraft:drowned": "entity.parrot.imitate.drowned",
	"minecraft:elder_guardian": "entity.parrot.imitate.elder_guardian", "minecraft:ender_dragon": "entity.parrot.imitate.ender_dragon",
	"minecraft:endermite": "entity.parrot.imitate.endermite", "minecraft:evoker": "entity.parrot.imitate.evoker",
	"minecraft:ghast": "entity.parrot.imitate.ghast", "minecraft:guardian": "entity.parrot.imitate.guardian",
	"minecraft:happy_ghast": "mob.imitate.happy_ghast", "minecraft:hoglin": "entity.parrot.imitate.hoglin",
	"minecraft:husk": "entity.parrot.imitate.husk", "minecraft:illusioner": "entity.parrot.imitate.illusioner",
	"minecraft:magma_cube": "entity.parrot.imitate.magma_cube", "minecraft:parched": "entity.parrot.imitate.parched",
	"minecraft:phantom": "entity.parrot.imitate.phantom", "minecraft:piglin": "entity.parrot.imitate.piglin",
	"minecraft:piglin_brute": "entity.parrot.imitate.piglin_brute", "minecraft:pillager": "entity.parrot.imitate.pillager",
	"minecraft:ravager": "entity.parrot.imitate.ravager", "minecraft:shulker": "entity.parrot.imitate.shulker",
	"minecraft:silverfish": "entity.parrot.imitate.silverfish", "minecraft:skeleton": "entity.parrot.imitate.skeleton",
	"minecraft:slime": "entity.parrot.imitate.slime", "minecraft:spider": "entity.parrot.imitate.spider",
	"minecraft:stray": "entity.parrot.imitate.stray", "minecraft:vex": "entity.parrot.imitate.vex",
	"minecraft:vindicator": "entity.parrot.imitate.vindicator", "minecraft:warden": "entity.parrot.imitate.warden",
	"minecraft:witch": "entity.parrot.imitate.witch", "minecraft:wither": "entity.parrot.imitate.wither",
	"minecraft:wither_skeleton": "entity.parrot.imitate.wither_skeleton", "minecraft:zoglin": "entity.parrot.imitate.zoglin",
	"minecraft:zombie": "entity.parrot.imitate.zombie", "minecraft:zombie_horse": "entity.parrot.imitate.zombie_horse",
	"minecraft:zombie_nautilus": "entity.parrot.imitate.zombie_nautilus", "minecraft:zombie_villager": "entity.parrot.imitate.zombie_villager",
}

// wanderPosition ports ParrotWanderGoal.getPosition: a parrot mostly flies
// to a perch on leaves or a log nearby, a parrot in water to land, and
// otherwise to a spot in the air ahead of it.
func (b *parrotBehaviour) wanderPosition(m *Mob) (mgl64.Vec3, bool) {
	pos, ok := mgl64.Vec3{}, false
	if m.InWater() {
		pos, ok = LandRandomPos(m, 15, 15)
	}
	// WaterAvoidingRandomStrollGoal.probability.
	if m.Random().NextFloat() >= .001 {
		pos, ok = parrotTreePos(m)
	}
	if ok {
		return pos, true
	}
	return waterAvoidingRandomFlyingPos(m)
}

// parrotTreePos ports ParrotWanderGoal.getTreePos: the first free block on
// leaves or a log within three blocks around and six up or down, with air
// above it.
func parrotTreePos(m *Mob) (mgl64.Vec3, bool) {
	mobPos := m.BlockPos()
	x, y, z := m.data.Pos[0], m.data.Pos[1], m.data.Pos[2]
	minX, minY, minZ := int(math.Floor(x-3)), int(math.Floor(y-6)), int(math.Floor(z-3))
	maxX, maxY, maxZ := int(math.Floor(x+3)), int(math.Floor(y+6)), int(math.Floor(z+3))
	// BlockPos.betweenClosed walks x first, then y, then z.
	for zz := minZ; zz <= maxZ; zz++ {
		for yy := minY; yy <= maxY; yy++ {
			for xx := minX; xx <= maxX; xx++ {
				pos := cube.Pos{xx, yy, zz}
				if pos == mobPos {
					continue
				}
				below := pathBlockAt(m.tx, pos.Side(cube.FaceDown))
				if !blockInTag(below, "minecraft:leaves") && !blockInTag(below, "minecraft:logs") {
					continue
				}
				if isAir(pathBlockAt(m.tx, pos)) && isAir(pathBlockAt(m.tx, pos.Side(cube.FaceUp))) {
					return mgl64.Vec3{float64(xx) + .5, float64(yy), float64(zz) + .5}, true
				}
			}
		}
	}
	return mgl64.Vec3{}, false
}

// waterAvoidingRandomFlyingPos ports WaterAvoidingRandomFlyingGoal.getPosition:
// a spot ahead of the mob hovering over the ground, or else in the air.
func waterAvoidingRandomFlyingPos(m *Mob) (mgl64.Vec3, bool) {
	dir := m.data.Rot.Vec3()
	if pos, ok := HoverRandomPos(m, 8, 7, dir[0], dir[2], math.Pi/2, 3, 1); ok {
		return pos, true
	}
	return AirAndWaterRandomPos(m, 8, 4, -2, dir[0], dir[2], math.Pi/2)
}

// shoulderCarrier is implemented by players a parrot may sit on, as
// ServerPlayer.setEntityOnShoulder.
type shoulderCarrier interface {
	world.Entity
	// SetEntityOnShoulder seats the entity saved as data on a free shoulder
	// and reports if it did.
	SetEntityOnShoulder(data map[string]any) bool
}

// landOnOwnersShoulderGoal ports LandOnOwnersShoulderGoal: a parrot that is
// not ordered to sit lands on the shoulder of its owner when it touches it.
type landOnOwnersShoulderGoal struct {
	goalFlags
	parrot            *parrotBehaviour
	sittingOnShoulder bool
}

func (g *landOnOwnersShoulderGoal) CanUse(m *Mob) bool {
	b := g.parrot
	owner, ok := b.ownerEntity(m.tx).(shoulderCarrier)
	if !ok || spectator(owner) || entityInWater(m.tx, owner) || inPowderSnow(m.tx, owner) {
		return false
	}
	if f, ok := owner.(interface{ Flying() bool }); ok && f.Flying() {
		return false
	}
	// ShoulderRidingEntity.canSitOnShoulder.
	return !b.orderedToSit && b.rideCooldown > 100
}

func (g *landOnOwnersShoulderGoal) IsInterruptable() bool { return !g.sittingOnShoulder }

func (g *landOnOwnersShoulderGoal) Start(*Mob) { g.sittingOnShoulder = false }

func (g *landOnOwnersShoulderGoal) Tick(m *Mob) {
	b := g.parrot
	if g.sittingOnShoulder || b.inSittingPose || m.Ent.HolderLeashID() != 0 {
		return
	}
	owner, ok := b.ownerEntity(m.tx).(shoulderCarrier)
	if !ok || !m.BBox().IntersectsWith(owner.H().Type().BBox(owner).Translate(owner.Position())) {
		return
	}
	// ShoulderRidingEntity.setEntityOnShoulder.
	data := ParrotType.EncodeNBT(m.data)
	data["id"] = ParrotType.EncodeEntity()
	if owner.SetEntityOnShoulder(data) {
		g.sittingOnShoulder = true
		_ = m.Ent.CloseIn(m.tx)
	}
}

// inPowderSnow reports if an entity stands in powder snow.
func inPowderSnow(tx *world.Tx, e world.Entity) bool {
	_, ok := tx.Block(cube.PosFromVec3(e.Position())).(block.PowderSnow)
	return ok
}

// followMobGoal ports FollowMobGoal: a mob follows a visible mob of another
// species within areaSize blocks, and backs off one closer than stopDistance.
type followMobGoal struct {
	goalFlags
	speed, stopDistance, areaSize float64

	following        *world.EntityHandle
	timeToRecalcPath int
	oldWaterCost     float64
}

func newFollowMobGoal(speed, stopDistance, areaSize float64) *followMobGoal {
	return &followMobGoal{goalFlags: goalFlags{GoalMove | GoalLook}, speed: speed, stopDistance: stopDistance, areaSize: areaSize}
}

func (g *followMobGoal) CanUse(m *Mob) bool {
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(g.areaSize)) {
		if !isMobEntity(e) || e.H().Type() == m.H().Type() {
			continue
		}
		if i, ok := e.(interface{ Invisible() bool }); ok && i.Invisible() {
			continue
		}
		g.following = e.H()
		return true
	}
	return false
}

func (g *followMobGoal) CanContinueToUse(m *Mob) bool {
	e, ok := g.following.Entity(m.tx)
	return ok && !m.AI.nav.IsDone() && m.DistanceToSqr(e) > g.stopDistance*g.stopDistance
}

func (g *followMobGoal) Start(m *Mob) {
	g.timeToRecalcPath = 0
	g.oldWaterCost = m.PathfindingMalus(PathWater)
	m.AI.SetPathfindingMalus(PathWater, 0)
}

func (g *followMobGoal) Stop(m *Mob) {
	g.following = nil
	m.AI.nav.Stop()
	m.AI.SetPathfindingMalus(PathWater, g.oldWaterCost)
}

func (g *followMobGoal) Tick(m *Mob) {
	e, ok := g.following.Entity(m.tx)
	if !ok || m.Ent.HolderLeashID() != 0 {
		return
	}
	m.AI.look.SetLookAtEntityWith(e, 10, m.AI.MaxHeadPitch)
	if g.timeToRecalcPath--; g.timeToRecalcPath > 0 {
		return
	}
	g.timeToRecalcPath = adjustedTickDelay(g, 10)
	d := m.data.Pos.Sub(e.Position())
	distSqr := d.LenSqr()
	if distSqr > g.stopDistance*g.stopDistance {
		m.AI.nav.MoveToEntity(m, e, g.speed)
		return
	}
	m.AI.nav.Stop()
	// Java compares the squared distance with the stop distance itself, and
	// backs off from a mob looking right at it.
	lookingAt := false
	if other, ok := mobOf(e); ok && other.AI.started {
		w := other.AI.look.Wanted()
		lookingAt = w[0] == m.data.Pos[0] && w[1] == m.data.Pos[1] && w[2] == m.data.Pos[2]
	}
	if distSqr <= g.stopDistance || lookingAt {
		dx, dz := e.Position()[0]-m.data.Pos[0], e.Position()[2]-m.data.Pos[2]
		m.AI.nav.MoveToPos(m, m.data.Pos[0]-dx, m.data.Pos[1], m.data.Pos[2]-dz, g.speed)
	}
}
