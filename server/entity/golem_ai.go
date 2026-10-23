package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's IronGolem and SnowGolem.

func (b *golemState) mobAI() *MobAI { return &b.ai }

// isEnemy reports if an entity implements Java's Enemy: every Monster, and
// slimes, magma cubes, ghasts, phantoms, shulkers, hoglins and the ender
// dragon.
func isEnemy(e world.Entity) bool {
	id := e.H().Type().EncodeEntity()
	switch id {
	case "minecraft:slime", "minecraft:magma_cube", "minecraft:ghast", "minecraft:phantom", "minecraft:shulker",
		"minecraft:hoglin", "minecraft:ender_dragon":
		return true
	}
	return monsterClass[id]
}

func (b *golemState) registerGoals(m *Mob) {
	if b.kind == snowKind {
		b.registerSnowGolemGoals(m)
		return
	}
	b.registerIronGolemGoals(m)
}

// registerIronGolemGoals ports IronGolem.registerGoals.
func (b *golemState) registerIronGolemGoals(m *Mob) {
	ai := m.AI
	ai.Goals.Add(1, NewMeleeAttackGoal(1, true))
	ai.Goals.Add(2, &moveTowardsTargetGoal{goalFlags: goalFlags{GoalMove}, speed: .9, within: 32})
	back := NewRandomStrollGoal(.6)
	back.Interval, back.CheckNoActionTime = 10, false
	back.Position = moveBackToVillagePosition
	ai.Goals.Add(2, moveBackToVillageGoal{back})
	stroll := NewRandomStrollGoal(.6)
	stroll.Interval, stroll.CheckNoActionTime = 240, false
	stroll.Position = golemStrollInVillagePosition
	ai.Goals.Add(4, stroll)
	ai.Goals.Add(5, &offerFlowerGoal{goalFlags: goalFlags{GoalMove | GoalLook}, golem: b})
	ai.Goals.Add(7, NewLookAtPlayerGoal(6))
	ai.Goals.Add(8, NewRandomLookAroundGoal())
	ai.Targets.Add(1, &defendVillageTargetGoal{targetGoal: newTargetGoal(false, true)})
	ai.Targets.Add(2, NewHurtByTargetGoal())
	angry := NewNearestAttackableTargetGoal(nil, true)
	angry.Selector = func(m *Mob, target world.Entity) bool { return b.angryAt(m, target) }
	ai.Targets.Add(3, angry)
	enemies := NewNearestAttackableTargetGoal(func(e world.Entity) bool {
		return isEnemy(e) && e.H().Type().EncodeEntity() != "minecraft:creeper"
	}, false)
	enemies.RandomInterval = reducedTickDelay(5)
	ai.Targets.Add(3, enemies)
	ai.Targets.Add(4, &resetUniversalAngerTargetGoal{lastHurtByPlayer: -1})
}

// registerSnowGolemGoals ports SnowGolem.registerGoals.
func (b *golemState) registerSnowGolemGoals(m *Mob) {
	ai := m.AI
	ai.Goals.Add(1, NewRangedAttackGoal(1.25, 20, 10, snowGolemThrow))
	stroll := NewWaterAvoidingRandomStrollGoal(1)
	stroll.Probability = 1.0000001e-5
	ai.Goals.Add(2, stroll)
	ai.Goals.Add(3, NewLookAtPlayerGoal(6))
	ai.Goals.Add(4, NewRandomLookAroundGoal())
	ai.Targets.Add(1, NewNearestAttackableTargetGoal(isEnemy, true))
}

// canAttackEntity ports IronGolem.canAttack: a golem a player built never
// attacks players, and no iron golem attacks creepers.
func (b *golemState) canAttackEntity(_ *Mob, target world.Entity) bool {
	if b.kind != ironKind {
		return true
	}
	if b.playerCreated && isPlayerEntity(target) {
		return false
	}
	return target.H().Type().EncodeEntity() != "minecraft:creeper"
}

// doHurtTarget ports IronGolem.doHurtTarget: an iron golem hits for 7.5 to
// 21.5 damage and flings its target up.
func (b *golemState) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	golemLiving{m.Ent, b}.viewAction(SwingArmAction{})
	damage := 7.5 + float64(m.Random().NextInt(15))
	if _, hurt := l.Hurt(damage, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	if v, ok := target.(interface {
		Velocity() mgl64.Vec3
		SetVelocity(mgl64.Vec3)
	}); ok {
		v.SetVelocity(v.Velocity().Add(mgl64.Vec3{0, .4}))
	}
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.iron_golem.attack", Volume: 1, Pitch: 1})
	return true
}

// afterAiStep runs what IronGolem.aiStep and SnowGolem.aiStep do after the
// AI: an iron golem's anger runs out and it turns on enemies pushing it,
// and a snow golem melts where it is hot and leaves snow where it walks.
func (b *golemState) afterAiStep(m *Mob) {
	if b.kind == ironKind {
		if b.offerFlowerTick > 0 {
			b.offerFlowerTick--
		}
		b.updatePersistentAnger(m, true)
		// IronGolem.doPush: an enemy the golem pushes away one time in 20
		// becomes its target.
		box := m.BBox()
		for e := range m.tx.EntitiesWithin(box) {
			if e.H() == m.H() || !isEnemy(e) || e.H().Type().EncodeEntity() == "minecraft:creeper" || !pushable(e) {
				continue
			}
			if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) && m.Random().NextInt(20) == 0 {
				m.SetTarget(e)
			}
		}
		return
	}
	pos := m.BlockPos()
	if m.tx.Temperature(pos) > 1 {
		golemLiving{m.Ent, b}.Hurt(1, golemFireDamage{})
	}
	// LivingEntity.baseTick: water and rain hurt a snow golem.
	top := cube.PosFromVec3(m.data.Pos.Add(mgl64.Vec3{0, m.BBox().Height()}))
	if m.InWater() || m.tx.RainingAt(pos) || m.tx.RainingAt(top) {
		golemLiving{m.Ent, b}.Hurt(1, DrowningDamageSource{})
	}
	if mobGriefingDisabled(m.tx) {
		return
	}
	for i := range 4 {
		x := int(math.Floor(m.data.Pos[0] + float64(i%2*2-1)*.25))
		z := int(math.Floor(m.data.Pos[2] + float64(i/2%2*2-1)*.25))
		p := cube.Pos{x, int(math.Floor(m.data.Pos[1])), z}
		if _, air := m.tx.Block(p).(block.Air); air && snowLayerSurvives(m.tx, p) {
			m.tx.SetBlock(p, block.SnowLayer{Layers: 1}, nil)
			m.tx.EmitGameEvent(world.GameEventBlockPlace, p.Vec3Centre(), world.GameEventContext{Source: m.Self})
		}
	}
}

// snowLayerSurvives ports SnowLayerBlock.canSurvive: snow lies on the full
// top face of a block, or on honey and soul sand, but never on ice.
func snowLayerSurvives(tx *world.Tx, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	b := tx.Block(below)
	switch b := b.(type) {
	case block.Ice, block.PackedIce, block.Barrier:
		return false
	case block.HoneyBlock, block.SoulSand:
		return true
	case block.SnowLayer:
		return b.Layers == 8
	}
	for _, bb := range b.Model().BBox(below, tx) {
		if bb.Min()[0] <= 0 && bb.Min()[2] <= 0 && bb.Max()[0] >= 1 && bb.Max()[2] >= 1 && bb.Max()[1] >= 1 {
			return true
		}
	}
	return false
}

// snowGolemThrow ports SnowGolem.performRangedAttack: a snow golem throws a
// snowball at the eyes of its target, aimed a little higher with distance.
func snowGolemThrow(m *Mob, target world.Entity, _ float64) {
	tp := target.Position()
	xd := tp[0] - m.data.Pos[0]
	zd := tp[2] - m.data.Pos[2]
	pos := mgl64.Vec3{m.data.Pos[0], m.EyeY() - .1, m.data.Pos[2]}
	yd := entityEyeY(target) - 1.1 - pos[1]
	yo := math.Sqrt(xd*xd+zd*zd) * float64(float32(.2))
	// SnowGolem.rangedAttackUncertainty is 12.
	velocity := shootDirection(m, mgl64.Vec3{xd, yd + yo, zd}, 1.6, 12)
	m.tx.AddEntity(newGolemSnowball(world.EntitySpawnOpts{Position: pos, Velocity: velocity}, m.Self))
	r := m.Random()
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.snow_golem.shoot", Volume: 1, Pitch: float64(.4 / (r.NextFloat()*.4 + .8))})
}

// moveTowardsTargetGoal ports MoveTowardsTargetGoal: a golem walks towards a
// target within 32 blocks.
type moveTowardsTargetGoal struct {
	goalFlags
	speed, within float64
	target        *world.EntityHandle
	wanted        mgl64.Vec3
}

func (g *moveTowardsTargetGoal) CanUse(m *Mob) bool {
	target := m.Target()
	if target == nil || m.DistanceToSqr(target) > g.within*g.within {
		return false
	}
	pos, ok := DefaultRandomPosTowards(m, 16, 7, target.Position(), math.Pi/2)
	if !ok {
		return false
	}
	g.target, g.wanted = target.H(), pos
	return true
}

func (g *moveTowardsTargetGoal) CanContinueToUse(m *Mob) bool {
	target, ok := g.target.Entity(m.tx)
	return !m.AI.nav.IsDone() && ok && entityAlive(target) && m.DistanceToSqr(target) < g.within*g.within
}

func (g *moveTowardsTargetGoal) Stop(*Mob) { g.target = nil }

func (g *moveTowardsTargetGoal) Start(m *Mob) {
	m.AI.nav.MoveToPos(m, g.wanted[0], g.wanted[1], g.wanted[2], g.speed)
}

// moveBackToVillageGoal ports MoveBackToVillageGoal: a golem outside a
// village strolls back towards the nearest village section.
type moveBackToVillageGoal struct{ *RandomStrollGoal }

func (g moveBackToVillageGoal) CanUse(m *Mob) bool {
	return !m.tx.IsVillage(m.BlockPos()) && g.RandomStrollGoal.CanUse(m)
}

// moveBackToVillagePosition ports MoveBackToVillageGoal.getPosition with
// BehaviorUtils.findSectionClosestToVillage.
func moveBackToVillagePosition(m *Mob) (mgl64.Vec3, bool) {
	pos := m.BlockPos()
	section := cube.Pos{pos[0] >> 4, pos[1] >> 4, pos[2] >> 4}
	best, bestDistance := section, m.tx.VillageDistance(pos)
	closest := bestDistance
	for x := -2; x <= 2; x++ {
		for y := -2; y <= 2; y++ {
			for z := -2; z <= 2; z++ {
				s := section.Add(cube.Pos{x, y, z})
				if d := m.tx.VillageDistance(cube.Pos{s[0] << 4, s[1] << 4, s[2] << 4}); d < closest && d < bestDistance {
					best, bestDistance = s, d
				}
			}
		}
	}
	if best == section {
		return mgl64.Vec3{}, false
	}
	centre := mgl64.Vec3{float64(best[0]<<4) + 8.5, float64(best[1]<<4) + 8, float64(best[2]<<4) + 8.5}
	return DefaultRandomPosTowards(m, 10, 7, centre, math.Pi/2)
}

// golemStrollInVillagePosition ports
// GolemRandomStrollInVillageGoal.getPosition: an iron golem strolls towards
// a villager that wants a golem, a POI of the village or anywhere nearby.
func golemStrollInVillagePosition(m *Mob) (mgl64.Vec3, bool) {
	r := m.Random()
	if r.NextFloat() < .3 {
		return LandRandomPos(m, 10, 7)
	}
	var pos mgl64.Vec3
	var ok bool
	if r.NextFloat() < .7 {
		if pos, ok = golemTowardsVillager(m); !ok {
			pos, ok = golemTowardsPOI(m)
		}
	} else if pos, ok = golemTowardsPOI(m); !ok {
		pos, ok = golemTowardsVillager(m)
	}
	if !ok {
		return LandRandomPos(m, 10, 7)
	}
	return pos, true
}

// golemTowardsVillager ports getPositionTowardsVillagerWhoWantsGolem.
func golemTowardsVillager(m *Mob) (mgl64.Vec3, bool) {
	var villagers []world.Entity
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(32)) {
		if v, ok := e.(*Villager); ok && v.WantsToSpawnGolem(m.tx.CurrentTick()) {
			villagers = append(villagers, e)
		}
	}
	if len(villagers) == 0 {
		return mgl64.Vec3{}, false
	}
	return LandRandomPosTowards(m, 10, 7, villagers[m.Random().NextInt(int32(len(villagers)))].Position())
}

// golemTowardsPOI ports getPositionTowardsPoi: an occupied POI within eight
// blocks of the centre of a random village section within two sections.
func golemTowardsPOI(m *Mob) (mgl64.Vec3, bool) {
	sections := m.tx.VillageSections(m.BlockPos(), 2)
	if len(sections) == 0 {
		return mgl64.Vec3{}, false
	}
	r := m.Random()
	centre := sections[r.NextInt(int32(len(sections)))].Add(cube.Pos{8, 8, 8})
	var pois []cube.Pos
	for pos, poi := range m.tx.POIsInRange(centre, 8) {
		if len(poi.Owners) > 0 || poi.HasOwner {
			pois = append(pois, pos)
		}
	}
	if len(pois) == 0 {
		return mgl64.Vec3{}, false
	}
	target := pois[r.NextInt(int32(len(pois)))]
	return LandRandomPosTowards(m, 10, 7, mgl64.Vec3{float64(target[0]) + .5, float64(target[1]), float64(target[2]) + .5})
}

// offerFlowerGoal ports OfferFlowerGoal: by day an iron golem now and then
// holds out a poppy to a villager or copper golem within six blocks for ten
// seconds, and a copper golem without one takes it.
type offerFlowerGoal struct {
	goalFlags
	golem  *golemState
	entity *world.EntityHandle
	tick   int
}

func (g *offerFlowerGoal) box(m *Mob) cube.BBox { return m.BBox().GrowVec3(mgl64.Vec3{6, 2, 6}) }

func (g *offerFlowerGoal) CanUse(m *Mob) bool {
	if m.tx.IsDarkOutside() || m.Random().NextInt(8000) != 0 {
		return false
	}
	c := forNonCombat()
	c.rangeLimit = 6
	e := nearestOfTypes(m, c, g.box(m), func(e world.Entity) bool {
		return entityTypeInTag(e, "minecraft:candidate_for_iron_golem_gift")
	})
	if e == nil {
		return false
	}
	g.entity = e.H()
	return true
}

func (g *offerFlowerGoal) CanContinueToUse(*Mob) bool { return g.tick > 0 }

func (g *offerFlowerGoal) Start(m *Mob) {
	g.tick = adjustedTickDelay(g, 400)
	g.golem.offerFlowerTick = 400
	m.tx.PlayEntityAnimation(m.Self, world.NewEntityAnimation("animation.iron_golem.offer_flower"))
	m.updateState()
}

func (g *offerFlowerGoal) Stop(m *Mob) {
	g.golem.offerFlowerTick = 0
	m.updateState()
	if e, ok := g.entity.Entity(m.tx); ok && g.tick == 0 && entityTypeInTag(e, "minecraft:accepts_iron_golem_gift") {
		if copper, ok := e.(*CopperGolem); ok && copper.Antenna().Empty() && g.box(m).IntersectsWith(e.H().Type().BBox(e).Translate(e.Position())) {
			copper.SetAntenna(item.NewStack(block.Flower{Type: block.Poppy()}, 1))
		}
	}
	g.entity = nil
}

func (g *offerFlowerGoal) Tick(m *Mob) {
	if e, ok := g.entity.Entity(m.tx); ok {
		m.AI.look.SetLookAtEntityWith(e, 30, 30)
	}
	g.tick--
}

// defendVillageTargetGoal ports DefendVillageTargetGoal: an iron golem
// targets a player a villager within ten blocks hates.
type defendVillageTargetGoal struct {
	targetGoal
	potential *world.EntityHandle
}

func (g *defendVillageTargetGoal) CanUse(m *Mob) bool {
	box := m.BBox().GrowVec3(mgl64.Vec3{10, 8, 10})
	c := forCombat()
	c.rangeLimit = 64
	var villagers, players []world.Entity
	for e := range m.tx.EntitiesWithin(box) {
		if !c.test(m, e) {
			continue
		}
		if _, ok := e.(*Villager); ok {
			villagers = append(villagers, e)
		} else if isPlayerEntity(e) {
			players = append(players, e)
		}
	}
	for _, v := range villagers {
		for _, p := range players {
			if v.(*Villager).Reputation(p) <= -100 {
				g.potential = p.H()
			}
		}
	}
	if g.potential == nil {
		return false
	}
	p, ok := g.potential.Entity(m.tx)
	return ok && !creativeOrSpectator(p)
}

func (g *defendVillageTargetGoal) Start(m *Mob) {
	if p, ok := g.potential.Entity(m.tx); ok {
		m.SetTarget(p)
	}
	g.start()
}

func (g *defendVillageTargetGoal) CanContinueToUse(m *Mob) bool { return g.canContinue(m) }

func (g *defendVillageTargetGoal) Stop(m *Mob) { g.stop(m) }
