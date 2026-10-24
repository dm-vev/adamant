package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// dragonTarget is a fly target location that may be unset, as Java's
// nullable Vec3.
type dragonTarget struct {
	pos mgl64.Vec3
	set bool
}

func (t dragonTarget) distanceSqr(pos mgl64.Vec3) float64 {
	if !t.set {
		return 0
	}
	return t.pos.Sub(pos).LenSqr()
}

// dragonPhases holds the state of each phase instance of the
// EnderDragonPhaseManager. Java keeps one instance per phase for the life of
// the dragon, so a phase keeps some state, as its direction or flame count,
// between the times it runs.
type dragonPhases struct {
	holdPath      dragonPath
	holdTarget    dragonTarget
	holdClockwise bool

	strafeCharge    int
	strafePath      dragonPath
	strafeTarget    dragonTarget
	strafeAttack    *world.EntityHandle
	strafeClockwise bool

	approachPath   dragonPath
	approachTarget dragonTarget

	landingTarget dragonTarget

	takeoffFirst  bool
	takeoffPath   dragonPath
	takeoffTarget dragonTarget

	scanTime int

	attackTicks int

	flameTicks, flameCount int
	flame                  *world.EntityHandle

	chargeTarget dragonTarget
	chargeSince  int

	dyingTarget dragonTarget
	dyingTime   int

	hoverTarget dragonTarget
}

// dragonSitting ports DragonPhaseInstance.isSitting.
func dragonSitting(p DragonPhase) bool {
	switch p {
	case DragonSittingScanning, DragonSittingAttacking, DragonSittingFlaming, DragonHovering:
		return true
	}
	return false
}

// setPhase ports EnderDragonPhaseManager.setPhase: the old phase ends and the
// new one begins, unless the dragon is in it already.
func (b *EnderDragonBehaviour) setPhase(tx *world.Tx, p DragonPhase) {
	if b.phaseSet && b.phase == p {
		return
	}
	if b.phaseSet {
		b.endPhase(tx)
	}
	b.phase, b.phaseSet = p, true
	ph := &b.ph
	switch p {
	case DragonHoldingPattern:
		ph.holdPath, ph.holdTarget = dragonPath{}, dragonTarget{}
	case DragonStrafePlayer:
		ph.strafeCharge, ph.strafeTarget, ph.strafePath, ph.strafeAttack = 0, dragonTarget{}, dragonPath{}, nil
	case DragonLandingApproach:
		ph.approachPath, ph.approachTarget = dragonPath{}, dragonTarget{}
	case DragonLanding:
		ph.landingTarget = dragonTarget{}
	case DragonTakeoff:
		ph.takeoffFirst, ph.takeoffPath, ph.takeoffTarget = true, dragonPath{}, dragonTarget{}
	case DragonSittingScanning:
		ph.scanTime = 0
	case DragonSittingAttacking:
		ph.attackTicks = 0
	case DragonSittingFlaming:
		ph.flameTicks = 0
		ph.flameCount++
	case DragonChargingPlayer:
		ph.chargeTarget, ph.chargeSince = dragonTarget{}, 0
	case DragonDying:
		ph.dyingTarget, ph.dyingTime = dragonTarget{}, 0
	case DragonHovering:
		ph.hoverTarget = dragonTarget{}
	}
}

// endPhase ports DragonPhaseInstance.end: only the flaming phase puts out its
// flame.
func (b *EnderDragonBehaviour) endPhase(tx *world.Tx) {
	if b.phase != DragonSittingFlaming || b.ph.flame == nil {
		return
	}
	if tx != nil {
		if flame, ok := b.ph.flame.Entity(tx); ok {
			_ = flame.(*Ent).CloseIn(tx)
		}
	}
	b.ph.flame = nil
}

// flyTarget ports DragonPhaseInstance.getFlyTargetLocation.
func (b *EnderDragonBehaviour) flyTarget() dragonTarget {
	ph := &b.ph
	switch b.phase {
	case DragonHoldingPattern:
		return ph.holdTarget
	case DragonStrafePlayer:
		return ph.strafeTarget
	case DragonLandingApproach:
		return ph.approachTarget
	case DragonLanding:
		return ph.landingTarget
	case DragonTakeoff:
		return ph.takeoffTarget
	case DragonChargingPlayer:
		return ph.chargeTarget
	case DragonDying:
		return ph.dyingTarget
	case DragonHovering:
		return ph.hoverTarget
	}
	return dragonTarget{}
}

// flySpeed ports DragonPhaseInstance.getFlySpeed.
func (b *EnderDragonBehaviour) flySpeed() float32 {
	switch b.phase {
	case DragonLanding:
		return 1.5
	case DragonChargingPlayer, DragonDying:
		return 3
	case DragonHovering:
		return 1
	}
	return .6
}

// turnSpeed ports DragonPhaseInstance.getTurnSpeed.
func (b *EnderDragonBehaviour) turnSpeed(vel mgl64.Vec3) float32 {
	rotSpeed := float32(math.Hypot(vel[0], vel[2])) + 1
	dist := min(rotSpeed, 40)
	if b.phase == DragonLanding {
		return dist / rotSpeed
	}
	return .7 / dist / rotSpeed
}

// tickPhase ports DragonPhaseInstance.doServerTick.
func (b *EnderDragonBehaviour) tickPhase(e *Ent, tx *world.Tx) {
	switch b.phase {
	case DragonHoldingPattern:
		b.tickHoldingPattern(e, tx)
	case DragonStrafePlayer:
		b.tickStrafe(e, tx)
	case DragonLandingApproach:
		b.tickLandingApproach(e, tx)
	case DragonLanding:
		b.tickLanding(e, tx)
	case DragonTakeoff:
		b.tickTakeoff(e, tx)
	case DragonSittingScanning:
		b.tickSittingScanning(e, tx)
	case DragonSittingAttacking:
		b.ph.attackTicks++
		if b.ph.attackTicks-1 >= 40 {
			b.setPhase(tx, DragonSittingFlaming)
		}
	case DragonSittingFlaming:
		b.tickSittingFlaming(e, tx)
	case DragonChargingPlayer:
		b.tickCharge(e, tx)
	case DragonDying:
		b.tickDying(e, tx)
	case DragonHovering:
		if !b.ph.hoverTarget.set {
			b.ph.hoverTarget = dragonTarget{e.Position(), true}
		}
	}
}

// podium returns the top of the End podium, as getHeightmapPos of the fight
// origin.
func (b *EnderDragonBehaviour) podium(tx *world.Tx, noLeaves bool) cube.Pos {
	return cube.Pos{b.origin[0], tx.JavaMotionBlockingHeight(b.origin[0], b.origin[2], noLeaves), b.origin[2]}
}

// distToCenterSqr ports Vec3i.distToCenterSqr.
func distToCenterSqr(p cube.Pos, pos mgl64.Vec3) float64 {
	return p.Vec3Centre().Sub(pos).LenSqr()
}

// navigate ports navigateToNextPathNode of the flying phases: the next node
// becomes the target, up to 20 blocks above it.
func navigate(path *dragonPath, target *dragonTarget) {
	if !path.live() {
		return
	}
	current := path.nextPos()
	path.advance()
	y := float64(current[1]) + float64(rand.Float32()*20)
	*target = dragonTarget{mgl64.Vec3{float64(current[0]), y, float64(current[2])}, true}
}

// circleNode ports the ring a flying phase circles on: one node on, or six
// across now and then, and only on the inner rings once no crystal is left.
func (b *EnderDragonBehaviour) circleNode(current int, clockwise *bool, allowOuter bool) int {
	target := current
	if rand.IntN(8) == 0 {
		*clockwise = !*clockwise
		target += 6
	}
	if *clockwise {
		target++
	} else {
		target--
	}
	if allowOuter {
		target %= 12
		if target < 0 {
			target += 12
		}
	} else {
		target -= 12
		target &= 7
		target += 12
	}
	return target
}

// tickHoldingPattern ports DragonHoldingPatternPhase.doServerTick.
func (b *EnderDragonBehaviour) tickHoldingPattern(e *Ent, tx *world.Tx) {
	if dist := b.ph.holdTarget.distanceSqr(e.Position()); dist < 100 || dist > 22500 {
		b.holdingPatternTarget(e, tx)
	}
}

func (b *EnderDragonBehaviour) holdingPatternTarget(e *Ent, tx *world.Tx) {
	ph := &b.ph
	if ph.holdPath.finished() {
		egg := b.podium(tx, true)
		crystals := 0
		if b.fight != nil {
			crystals = b.fight.AliveCrystals()
		}
		if rand.IntN(crystals+3) == 0 {
			b.setPhase(tx, DragonLandingApproach)
			return
		}
		player := dragonNearestPlayer(tx, e, egg.Vec3(), 0, false, nil)
		distSqr := 64.0
		if player != nil {
			distSqr = distToCenterSqr(egg, player.Position()) / 512
		}
		if player != nil && (rand.IntN(int(distSqr+2)) == 0 || rand.IntN(crystals+2) == 0) {
			b.strafePlayer(e, tx, player)
			return
		}
	}
	if ph.holdPath.missingOrDone() {
		current := b.closestNode(tx, e)
		target := b.circleNode(current, &ph.holdClockwise, b.fight != nil && b.fight.AliveCrystals() >= 0)
		ph.holdPath = b.findPath(tx, current, target, nil)
		if ph.holdPath.valid {
			ph.holdPath.advance()
		}
	}
	navigate(&ph.holdPath, &ph.holdTarget)
}

// strafePlayer ports DragonHoldingPatternPhase.strafePlayer.
func (b *EnderDragonBehaviour) strafePlayer(e *Ent, tx *world.Tx, player world.Entity) {
	b.setPhase(tx, DragonStrafePlayer)
	b.setStrafeTarget(e, tx, player)
}

// setStrafeTarget ports DragonStrafePlayerPhase.setTarget: a path to the
// node nearest the target, ending above it.
func (b *EnderDragonBehaviour) setStrafeTarget(e *Ent, tx *world.Tx, target world.Entity) {
	ph := &b.ph
	ph.strafeAttack = target.H()
	current := b.closestNode(tx, e)
	tp := target.Position()
	targetNode := b.closestNodeTo(tx, tp[0], tp[1], tp[2])
	fx, fz := int(math.Floor(tp[0])), int(math.Floor(tp[2]))
	xd, zd := float64(fx)-e.Position()[0], float64(fz)-e.Position()[2]
	ho := math.Min(float64(float32(.4))+math.Sqrt(xd*xd+zd*zd)/80-1, 10)
	final := cube.Pos{fx, int(math.Floor(tp[1] + ho)), fz}
	ph.strafePath = b.findPath(tx, current, targetNode, &final)
	if ph.strafePath.valid {
		ph.strafePath.advance()
		navigate(&ph.strafePath, &ph.strafeTarget)
	}
}

// tickStrafe ports DragonStrafePlayerPhase.doServerTick: the dragon flies at
// its target and shoots a fireball once it has faced it for five ticks.
func (b *EnderDragonBehaviour) tickStrafe(e *Ent, tx *world.Tx) {
	ph := &b.ph
	var attack world.Entity
	if ph.strafeAttack != nil {
		attack, _ = ph.strafeAttack.Entity(tx)
	}
	if attack == nil {
		b.setPhase(tx, DragonHoldingPattern)
		return
	}
	pos, tp := e.Position(), attack.Position()
	if ph.strafePath.finished() {
		xd, zd := tp[0]-pos[0], tp[2]-pos[2]
		ho := math.Min(float64(float32(.4))+math.Sqrt(xd*xd+zd*zd)/80-1, 10)
		ph.strafeTarget = dragonTarget{mgl64.Vec3{tp[0], tp[1] + ho, tp[2]}, true}
	}
	if dist := ph.strafeTarget.distanceSqr(pos); dist < 100 || dist > 22500 {
		if ph.strafePath.missingOrDone() {
			current := b.closestNode(tx, e)
			target := b.circleNode(current, &ph.strafeClockwise, b.fight != nil && b.fight.AliveCrystals() > 0)
			ph.strafePath = b.findPath(tx, current, target, nil)
			if ph.strafePath.valid {
				ph.strafePath.advance()
			}
		}
		navigate(&ph.strafePath, &ph.strafeTarget)
	}
	if tp.Sub(pos).LenSqr() >= 4096 || !dragonSees(tx, e, attack) {
		if ph.strafeCharge > 0 {
			ph.strafeCharge--
		}
		return
	}
	ph.strafeCharge++
	aim := javaNormalize(mgl64.Vec3{tp[0] - pos[0], 0, tp[2] - pos[2]})
	yaw := float32(e.Rotation().Yaw()) * float32(math.Pi/180)
	dir := javaNormalize(mgl64.Vec3{float64(vanilla.Sin(float64(yaw))), 0, -float64(vanilla.Cos(float64(yaw)))})
	dot := float32(dir.Dot(aim))
	angle := float32(math.Acos(float64(dot))*180/math.Pi) + .5
	if ph.strafeCharge < 5 || angle < 0 || angle >= 10 {
		return
	}
	head := b.parts[dragonPartHead]
	view := dragonViewVector(0, float32(e.Rotation().Yaw()))
	start := mgl64.Vec3{head[0] - view[0], head[1] + .5 + .5, head[2] - view[2]}
	targetY := tp[1] + attack.H().Type().BBox(attack).Height()*.5
	spawnDragonFireballFrom(e, start, mgl64.Vec3{tp[0], targetY, tp[2]}, tx)
	ph.strafeCharge = 0
	if ph.strafePath.valid {
		ph.strafePath.finish()
	}
	b.setPhase(tx, DragonHoldingPattern)
}

// dragonViewVector ports Entity.calculateViewVector.
func dragonViewVector(xRot, yRot float32) mgl64.Vec3 {
	xRad := xRot * float32(math.Pi/180)
	yRad := -yRot * float32(math.Pi/180)
	xCos, xSin := vanilla.Cos(float64(yRad)), vanilla.Sin(float64(yRad))
	yCos, ySin := vanilla.Cos(float64(xRad)), vanilla.Sin(float64(xRad))
	return mgl64.Vec3{float64(xSin * yCos), float64(-ySin), float64(xCos * yCos)}
}

// headLookVector ports EnderDragon.getHeadLookVector.
func (b *EnderDragonBehaviour) headLookVector(e *Ent, tx *world.Tx) mgl64.Vec3 {
	yaw := float32(e.Rotation().Yaw())
	switch {
	case b.phase == DragonLanding || b.phase == DragonTakeoff:
		egg := b.podium(tx, true)
		dist := max(float32(math.Sqrt(distToCenterSqr(egg, e.Position())))/4, 1)
		yOffset := 6 / dist
		return dragonViewVector(-yOffset*1.5*5, yaw)
	case dragonSitting(b.phase):
		return dragonViewVector(-45, yaw)
	}
	return dragonViewVector(float32(e.Rotation().Pitch()), yaw)
}

// tickLandingApproach ports DragonLandingApproachPhase.doServerTick.
func (b *EnderDragonBehaviour) tickLandingApproach(e *Ent, tx *world.Tx) {
	ph := &b.ph
	if dist := ph.approachTarget.distanceSqr(e.Position()); dist >= 100 && dist <= 22500 {
		return
	}
	if ph.approachPath.missingOrDone() {
		current := b.closestNode(tx, e)
		egg := b.podium(tx, true)
		var targetNode int
		if player := dragonNearestPlayer(tx, e, egg.Vec3(), 0, false, nil); player != nil {
			aim := javaNormalize(mgl64.Vec3{player.Position()[0], 0, player.Position()[2]})
			targetNode = b.closestNodeTo(tx, -aim[0]*40, 105, -aim[2]*40)
		} else {
			targetNode = b.closestNodeTo(tx, 40, float64(egg[1]), 0)
		}
		ph.approachPath = b.findPath(tx, current, targetNode, &egg)
		if ph.approachPath.valid {
			ph.approachPath.advance()
		}
	}
	navigate(&ph.approachPath, &ph.approachTarget)
	if ph.approachPath.finished() {
		b.setPhase(tx, DragonLanding)
	}
}

// tickLanding ports DragonLandingPhase.doServerTick.
func (b *EnderDragonBehaviour) tickLanding(e *Ent, tx *world.Tx) {
	ph := &b.ph
	if !ph.landingTarget.set {
		egg := b.podium(tx, true)
		ph.landingTarget = dragonTarget{mgl64.Vec3{float64(egg[0]) + .5, float64(egg[1]), float64(egg[2]) + .5}, true}
	}
	if ph.landingTarget.distanceSqr(e.Position()) < 1 {
		ph.flameCount = 0
		b.setPhase(tx, DragonSittingScanning)
	}
}

// tickTakeoff ports DragonTakeoffPhase.doServerTick.
func (b *EnderDragonBehaviour) tickTakeoff(e *Ent, tx *world.Tx) {
	ph := &b.ph
	if !ph.takeoffFirst && ph.takeoffPath.valid {
		egg := b.podium(tx, true)
		if distToCenterSqr(egg, e.Position()) >= 100 {
			b.setPhase(tx, DragonHoldingPattern)
		}
		return
	}
	ph.takeoffFirst = false
	current := b.closestNode(tx, e)
	look := b.headLookVector(e, tx)
	target := b.closestNodeTo(tx, -look[0]*40, 105, -look[2]*40)
	if b.fight != nil && b.fight.AliveCrystals() > 0 {
		target %= 12
		if target < 0 {
			target += 12
		}
	} else {
		target -= 12
		target &= 7
		target += 12
	}
	ph.takeoffPath = b.findPath(tx, current, target, nil)
	if ph.takeoffPath.valid {
		ph.takeoffPath.advance()
		navigate(&ph.takeoffPath, &ph.takeoffTarget)
	}
}

// tickSittingScanning ports DragonSittingScanningPhase.doServerTick: the
// dragon turns to the nearest player close by, roars at it after 25 ticks,
// and takes off after 100 without one, charging at one within 150 blocks.
func (b *EnderDragonBehaviour) tickSittingScanning(e *Ent, tx *world.Tx) {
	ph := &b.ph
	ph.scanTime++
	pos := e.Position()
	target := dragonNearestPlayer(tx, e, pos, 20, true, func(t world.Entity) bool {
		return math.Abs(t.Position()[1]-pos[1]) <= 10
	})
	if target != nil {
		if ph.scanTime > 25 {
			b.setPhase(tx, DragonSittingAttacking)
			return
		}
		tp := target.Position()
		aim := javaNormalize(mgl64.Vec3{tp[0] - pos[0], 0, tp[2] - pos[2]})
		yaw := float32(e.Rotation().Yaw())
		yawRad := yaw * float32(math.Pi/180)
		dir := javaNormalize(mgl64.Vec3{float64(vanilla.Sin(float64(yawRad))), 0, -float64(vanilla.Cos(float64(yawRad)))})
		dot := float32(dir.Dot(aim))
		angle := float32(math.Acos(float64(dot))*180/math.Pi) + .5
		if angle < 0 || angle > 10 {
			head := b.parts[dragonPartHead]
			xd, zd := tp[0]-head[0], tp[2]-head[2]
			delta := mgl64.Clamp(wrapDegrees64(180-vanilla.Atan2(xd, zd)*180/float64(float32(math.Pi))-float64(yaw)), -100, 100)
			b.yRotA *= .8
			dist := float32(math.Sqrt(xd*xd+zd*zd)) + 1
			rotSpeed := dist
			if dist > 40 {
				dist = 40
			}
			b.yRotA += float32(delta) * (.7 / dist / rotSpeed)
			e.data.Rot[0] = float64(yaw + b.yRotA)
		}
		return
	}
	if ph.scanTime >= 100 {
		target = dragonNearestPlayer(tx, e, pos, 150, true, nil)
		b.setPhase(tx, DragonTakeoff)
		if target != nil {
			b.setPhase(tx, DragonChargingPlayer)
			b.ph.chargeTarget = dragonTarget{target.Position(), true}
		}
	}
}

// tickSittingFlaming ports DragonSittingFlamingPhase.doServerTick: ten
// ticks in, the dragon breathes a cloud of dragon's breath before its head.
func (b *EnderDragonBehaviour) tickSittingFlaming(e *Ent, tx *world.Tx) {
	ph := &b.ph
	ph.flameTicks++
	if ph.flameTicks >= 200 {
		if ph.flameCount >= 4 {
			b.setPhase(tx, DragonTakeoff)
		} else {
			b.setPhase(tx, DragonSittingScanning)
		}
		return
	}
	if ph.flameTicks != 10 {
		return
	}
	head, pos := b.parts[dragonPartHead], e.Position()
	look := javaNormalize(mgl64.Vec3{head[0] - pos[0], 0, head[2] - pos[2]})
	x, z := head[0]+look[0]*5/2, head[2]+look[2]*5/2
	initialY := head[1] + .5
	y := initialY
	for {
		if _, air := tx.Block(cube.Pos{int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))}).(block.Air); !air {
			break
		}
		if y--; y < 0 {
			y = initialY
			break
		}
	}
	y = math.Floor(y) + 1
	ph.flame = tx.AddEntity(newDragonBreathCloud(mgl64.Vec3{x, y, z}, e.H(), 5, 200, 0, 0)).H()
}

// tickCharge ports DragonChargePlayerPhase.doServerTick.
func (b *EnderDragonBehaviour) tickCharge(e *Ent, tx *world.Tx) {
	ph := &b.ph
	switch {
	case !ph.chargeTarget.set:
		b.setPhase(tx, DragonHoldingPattern)
	case ph.chargeSince > 0:
		ph.chargeSince++
		if ph.chargeSince-1 >= 10 {
			b.setPhase(tx, DragonHoldingPattern)
		}
	default:
		if dist := ph.chargeTarget.distanceSqr(e.Position()); dist < 100 || dist > 22500 {
			ph.chargeSince++
		}
	}
}

// tickDying ports DragonDeathPhase.doServerTick: the dragon flies to the
// podium and dies once it is there.
func (b *EnderDragonBehaviour) tickDying(e *Ent, tx *world.Tx) {
	ph := &b.ph
	ph.dyingTime++
	if !ph.dyingTarget.set {
		egg := b.podium(tx, false)
		ph.dyingTarget = dragonTarget{mgl64.Vec3{float64(egg[0]) + .5, float64(egg[1]), float64(egg[2]) + .5}, true}
	}
	if dist := ph.dyingTarget.distanceSqr(e.Position()); dist >= 100 && dist <= 22500 {
		b.health.AddHealth(1 - b.health.Health())
	} else {
		b.health.AddHealth(-b.health.Health())
	}
}

// crystalDestroyed ports DragonPhaseInstance.onCrystalDestroyed: only a
// circling dragon turns on the player.
func (b *EnderDragonBehaviour) crystalDestroyed(e *Ent, tx *world.Tx, player world.Entity) {
	if b.phase == DragonHoldingPattern && player != nil && canBeSeenAsEnemy(player) {
		b.strafePlayer(e, tx, player)
	}
}

// dragonNearestPlayer ports getNearestPlayer with the dragon's combat
// TargetingConditions: players it may attack, within the range from it if
// one is given, in its sight if asked, nearest to pos.
func dragonNearestPlayer(tx *world.Tx, dragon *Ent, pos mgl64.Vec3, maxRange float64, sight bool, selector func(world.Entity) bool) world.Entity {
	var nearest world.Entity
	best := math.MaxFloat64
	for p := range tx.Players() {
		if !canBeSeenAsEnemy(p) || (selector != nil && !selector(p)) {
			continue
		}
		if maxRange > 0 {
			visible := max(maxRange*visibilityPercent(p), 2)
			if p.Position().Sub(dragon.Position()).LenSqr() > visible*visible {
				continue
			}
		}
		if sight && !dragonSees(tx, dragon, p) {
			continue
		}
		if d := p.Position().Sub(pos).LenSqr(); d < best {
			nearest, best = p, d
		}
	}
	return nearest
}

// dragonSees ports LivingEntity.hasLineOfSight from the dragon's eyes.
func dragonSees(tx *world.Tx, dragon *Ent, target world.Entity) bool {
	pos := dragon.Position()
	return lineOfSight(tx, mgl64.Vec3{pos[0], pos[1] + dragonEyeHeight, pos[2]}, mgl64.Vec3{target.Position()[0], entityEyeY(target), target.Position()[2]})
}

func wrapDegrees64(v float64) float64 {
	v = math.Mod(v, 360)
	if v >= 180 {
		v -= 360
	}
	if v < -180 {
		v += 360
	}
	return v
}

func wrapDegrees32(v float32) float32 {
	v = float32(math.Mod(float64(v), 360))
	if v >= 180 {
		v -= 360
	}
	if v < -180 {
		v += 360
	}
	return v
}
