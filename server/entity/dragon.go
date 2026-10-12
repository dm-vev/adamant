package entity

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// DragonPhase is the Java EnderDragon phase ID.
type DragonPhase int32

const (
	DragonHoldingPattern DragonPhase = iota
	DragonStrafePlayer
	DragonLandingApproach
	DragonLanding
	DragonTakeoff
	DragonSittingFlaming
	DragonSittingScanning
	DragonSittingAttacking
	DragonChargingPlayer
	DragonDying
	DragonHovering
)

// EnderDragonBehaviourConfig configures an EnderDragon's persistent runtime state.
type EnderDragonBehaviourConfig struct {
	Health        float64
	Phase         DragonPhase
	FightOrigin   cube.Pos
	DeathTime     int
	SittingDamage float64
	Fight         world.DragonFight
	healthSet     bool
}

// NewEnderDragon creates an EnderDragon. The fight controller is attached by the world lifecycle.
func NewEnderDragon(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(EnderDragonType, EnderDragonBehaviourConfig{Phase: DragonHovering})
}

var EnderDragonType enderDragonType

type enderDragonType struct{}

func (enderDragonType) EncodeEntity() string { return "minecraft:ender_dragon" }
func (enderDragonType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		EnderDragonBehaviourConfig{Phase: DragonHovering}.Apply(data)
	}
	dragon := &EnderDragon{Ent: &Ent{tx: tx, handle: h, data: data}}
	if fight := tx.DragonFight(); fight != nil {
		fight.AttachDragon(dragon)
	}
	return dragon
}
func (enderDragonType) BBox(world.Entity) cube.BBox { return cube.Box(-2.5, 0, -1.5, 2.5, 3, 1.5) }
func (enderDragonType) DecodeNBT(m map[string]any, data *world.EntityData) {
	c := EnderDragonBehaviourConfig{Phase: DragonHovering}
	if _, ok := m["Health"]; ok {
		c.Health = float64(nbtconv.Float32(m, "Health"))
		c.healthSet = true
	}
	if _, ok := m["DragonPhase"]; ok {
		c.Phase = DragonPhase(nbtconv.Int32(m, "DragonPhase"))
	}
	if _, ok := m["DragonDeathTime"]; ok {
		c.DeathTime = int(nbtconv.Int32(m, "DragonDeathTime"))
	}
	if _, ok := m["sitting_damage_received"]; ok {
		c.SittingDamage = float64(nbtconv.Float32(m, "sitting_damage_received"))
	}
	if c.Phase < DragonHoldingPattern || c.Phase > DragonHovering {
		c.Phase = DragonHovering
	}
	c.Apply(data)
}
func (enderDragonType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*EnderDragonBehaviour)
	return map[string]any{
		"Health": float32(b.health.Health()), "DragonPhase": int32(b.phase),
		"DragonDeathTime": int32(b.deathTime), "sitting_damage_received": float32(b.sittingDamage),
	}
}
func (c EnderDragonBehaviourConfig) Apply(data *world.EntityData) { data.Data = c.New() }
func (c EnderDragonBehaviourConfig) New() *EnderDragonBehaviour {
	health := c.Health
	if !c.healthSet && health <= 0 {
		health = 200
	}
	phase := c.Phase
	if phase < DragonHoldingPattern || phase > DragonHovering {
		phase = DragonHovering
	}
	return &EnderDragonBehaviour{health: NewHealthManager(health, 200), movement: &MovementComputer{NoBlockCollisions: true}, phase: phase, origin: c.FightOrigin, deathTime: c.DeathTime, sittingDamage: c.SittingDamage, fight: c.Fight, firstTakeoffTick: phase == DragonTakeoff, flightHead: -1}
}

type EnderDragonBehaviour struct {
	health                                *HealthManager
	movement                              *MovementComputer
	phase                                 DragonPhase
	phaseTicks                            int
	tickCount                             int64
	origin                                cube.Pos
	deathTime                             int
	sittingDamage                         float64
	fight                                 world.DragonFight
	target                                mgl64.Vec3
	targetSet                             bool
	nearestCrystal                        *world.EntityHandle
	phaseTarget                           *world.EntityHandle
	clockwise, firstTakeoffTick           bool
	fireballCharge, flameCount, scanTicks int
	flightY                               [64]float64
	flightYaw                             [64]float64
	flightHead                            int
	yRotA                                 float64
	hurtTime                              int
	wingHitTicks                          map[*world.EntityHandle]int64
	dead                                  bool
	nodes                                 [24]cube.Pos
	nodesInitialized                      bool
	path                                  [25]mgl64.Vec3
	pathLen, pathIndex                    int
}

// EnderDragon runs the server-side dragon phase machine and flight behavior.
type EnderDragon struct{ *Ent }

func (d *EnderDragon) behaviour() *EnderDragonBehaviour { return d.Behaviour().(*EnderDragonBehaviour) }

// DragonPartBox describes one independently hittable Java dragon body part.
type DragonPartBox struct {
	Name string
	Box  cube.BBox
}

// PartBoxes returns the dragon's head, neck, body, three tail segments and
// two wings, positioned relative to its current yaw without allocating.
func (d *EnderDragon) PartBoxes() [8]DragonPartBox {
	b := d.behaviour()
	pos := d.Position()
	yaw := float32(d.Rotation().Yaw()) * float32(math.Pi/180)
	rotationSin, rotationCos := java26.Sin(float64(yaw)), java26.Cos(float64(yaw))
	y5, yaw5 := b.flightSample(5, d.Ent)
	y10, _ := b.flightSample(10, d.Ent)
	tilt := float32(y5-y10) * 10 * float32(math.Pi/180)
	tiltSin, tiltCos := java26.Sin(float64(tilt)), java26.Cos(float64(tilt))
	yOffset := float64(float32(y5 - pos[1]))
	if b.phase == DragonSittingFlaming || b.phase == DragonSittingScanning || b.phase == DragonSittingAttacking {
		yOffset = -1
	}
	headYaw := yaw - float32(b.yRotA)*.01
	headSin, headCos := java26.Sin(float64(headYaw)), java26.Cos(float64(headYaw))
	head := pos.Add(mgl64.Vec3{float64(headSin * 6.5 * tiltCos), yOffset + float64(tiltSin*6.5), float64(-headCos * 6.5 * tiltCos)})
	neck := pos.Add(mgl64.Vec3{float64(headSin * 5.5 * tiltCos), yOffset + float64(tiltSin*5.5), float64(-headCos * 5.5 * tiltCos)})
	body := pos.Add(mgl64.Vec3{float64(rotationSin * .5), 0, float64(-rotationCos * .5)})
	wing1 := pos.Add(mgl64.Vec3{float64(rotationCos * 4.5), 2, float64(rotationSin * 4.5)})
	wing2 := pos.Add(mgl64.Vec3{float64(-rotationCos * 4.5), 2, float64(-rotationSin * 4.5)})
	tailNames := [3]string{"tail1", "tail2", "tail3"}
	tail := [3]DragonPartBox{}
	for i := range 3 {
		y0, yaw0 := b.flightSample(12+i*2, d.Ent)
		deltaYaw := wrapDragonDegrees(yaw0 - yaw5)
		rot := yaw + float32(deltaYaw)*float32(math.Pi/180)
		distance := float32((i + 1) * 2)
		rotSin, rotCos := java26.Sin(float64(rot)), java26.Cos(float64(rot))
		center := pos.Add(mgl64.Vec3{
			float64(-(rotationSin*1.5 + rotSin*distance) * tiltCos),
			y0 - y5 - float64((distance+1.5)*tiltSin) + 1.5,
			float64((rotationCos*1.5 + rotCos*distance) * tiltCos),
		})
		tail[i] = dragonPartBox(tailNames[i], center, 2, 2, 2)
	}
	return [8]DragonPartBox{
		dragonPartBox("head", head, 1, 1, 1),
		dragonPartBox("neck", neck, 3, 3, 3),
		dragonPartBox("body", body, 5, 3, 3),
		tail[0], tail[1], tail[2],
		dragonPartBox("wing1", wing1, 4, 2, 2),
		dragonPartBox("wing2", wing2, 4, 2, 2),
	}
}

func dragonPartBox(name string, center mgl64.Vec3, width, height, depth float64) DragonPartBox {
	return DragonPartBox{Name: name, Box: cube.Box(center[0]-width/2, center[1]-height/2, center[2]-depth/2, center[0]+width/2, center[1]+height/2, center[2]+depth/2)}
}

func wrapDragonDegrees(value float64) float64 {
	value = math.Mod(value, 360)
	if value >= 180 {
		value -= 360
	} else if value < -180 {
		value += 360
	}
	return value
}

// PartAt resolves a point against the dragon's body parts, preferring head/neck.
func (d *EnderDragon) PartAt(pos mgl64.Vec3) (string, bool) {
	parts := d.PartBoxes()
	for _, part := range parts {
		min, max := part.Box.Min(), part.Box.Max()
		if pos[0] >= min[0] && pos[0] <= max[0] && pos[1] >= min[1] && pos[1] <= max[1] && pos[2] >= min[2] && pos[2] <= max[2] {
			return part.Name, true
		}
	}
	return "", false
}

// PartHitFrom returns the nearest part intersected by a finite ray.
func (d *EnderDragon) PartHitFrom(start, direction mgl64.Vec3, distance float64) (string, mgl64.Vec3, bool) {
	if direction.Len() == 0 || distance < 0 {
		return "", mgl64.Vec3{}, false
	}
	direction = direction.Normalize()
	nearest := distance
	name := ""
	for _, part := range d.PartBoxes() {
		if hitDistance, ok := dragonRayBox(part.Box, start, direction, nearest); ok {
			nearest, name = hitDistance, part.Name
		}
	}
	if name == "" {
		return "", mgl64.Vec3{}, false
	}
	return name, start.Add(direction.Mul(nearest)), true
}

func dragonRayBox(box cube.BBox, start, direction mgl64.Vec3, maxDistance float64) (float64, bool) {
	minimum, maximum := box.Min(), box.Max()
	near, far := 0.0, maxDistance
	for axis := range 3 {
		if math.Abs(direction[axis]) < 1e-9 {
			if start[axis] < minimum[axis] || start[axis] > maximum[axis] {
				return 0, false
			}
			continue
		}
		first := (minimum[axis] - start[axis]) / direction[axis]
		second := (maximum[axis] - start[axis]) / direction[axis]
		if first > second {
			first, second = second, first
		}
		near = max(near, first)
		far = min(far, second)
		if near > far {
			return 0, false
		}
	}
	return near, far >= 0 && near <= maxDistance
}

func dragonLineOfSight(tx *world.Tx, start, end mgl64.Vec3) bool {
	blocked := false
	trace.TraverseBlocks(start, end, func(pos cube.Pos) bool {
		if trace.BlockIntersects(pos, tx, tx.Block(pos), start, end) {
			blocked = true
			return false
		}
		return true
	})
	return !blocked
}

func (d *EnderDragon) Tick(tx *world.Tx, current int64) {
	d.Ent.Tick(tx, current)
	b := d.behaviour()
	if b.fight != nil && !b.dead {
		b.fight.UpdateDragon(tx, d)
	}
}

func (b *EnderDragonBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.dead {
		return nil
	}
	if b.health.Health() <= 0 {
		b.deathTime++
		if b.deathTime == 1 {
			tx.PlaySound(e.Position(), sound.Custom{Name: "entity.ender_dragon.death", Volume: 10, Pitch: 1})
		}
		xp := 500
		if b.fight == nil || !b.fight.HasPreviouslyKilledDragon() {
			xp = 12000
		}
		if b.deathTime > 150 && b.deathTime <= 200 && b.deathTime%5 == 0 {
			for _, orb := range NewExperienceOrbs(e.Position(), xp*8/100) {
				tx.AddEntity(orb)
			}
		}
		// Source tickDeath is separate from the still-living DYING flight
		// phase. Its ascent uses the widened 0.1F constant on every tick.
		b.movement.Drag, b.movement.Gravity = 0, 0
		m := b.movement.TickMovement(e, e.Position(), mgl64.Vec3{0, float64(float32(.1)), 0}, e.Rotation(), e.Rotation(), tx)
		e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
		if b.deathTime >= 200 {
			for _, orb := range NewExperienceOrbs(e.Position(), xp*20/100) {
				tx.AddEntity(orb)
			}
			if b.fight != nil {
				b.fight.SetDragonKilled(tx, &EnderDragon{Ent: e})
			}
			b.dead = true
			_ = e.CloseIn(tx)
			m.releaseViewers()
			return nil
		}
		return m
	}
	b.recordFlight(e)
	b.phaseTicks++
	b.tickCount++
	if b.hurtTime > 0 {
		b.hurtTime--
	}
	b.updateCrystals(e, tx)
	phase := b.phase
	b.tickPhase(e, tx)
	if b.phase != phase {
		b.tickPhase(e, tx)
	}
	b.moveTowardTarget(e)
	b.attackNearby(e, tx)
	b.movement.Drag = 0
	b.movement.Gravity = 0
	m := b.movement.TickMovement(e, e.Position(), e.data.Vel, e.data.Rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}

func (b *EnderDragonBehaviour) recordFlight(e *Ent) {
	y, yaw := e.Position()[1], e.Rotation().Yaw()
	if b.flightHead < 0 {
		for i := range b.flightY {
			b.flightY[i], b.flightYaw[i] = y, yaw
		}
		b.flightHead = 0
		return
	}
	b.flightHead = (b.flightHead + 1) & 63
	b.flightY[b.flightHead], b.flightYaw[b.flightHead] = y, yaw
}

func (b *EnderDragonBehaviour) flightSample(delay int, e *Ent) (float64, float64) {
	if b.flightHead < 0 {
		return e.Position()[1], e.Rotation().Yaw()
	}
	i := (b.flightHead - delay) & 63
	return b.flightY[i], b.flightYaw[i]
}

func (b *EnderDragonBehaviour) updateCrystals(e *Ent, tx *world.Tx) {
	if b.nearestCrystal != nil && b.nearestCrystal.Closed() {
		b.nearestCrystal = nil
	}
	if b.nearestCrystal != nil && b.tickCount%10 == 0 && b.health.Health() < b.health.MaxHealth() {
		b.health.AddHealth(1)
	}
	if rand.IntN(10) != 0 {
		return
	}
	var nearest *world.EntityHandle
	best := math.Inf(1)
	for candidate := range tx.Entities() {
		if candidate.H().Type().EncodeEntity() != "minecraft:ender_crystal" {
			continue
		}
		delta := candidate.Position().Sub(e.Position())
		if math.Abs(delta[0]) > 32 || math.Abs(delta[1]) > 32 || math.Abs(delta[2]) > 32 {
			continue
		}
		if distance := delta.LenSqr(); distance < best {
			nearest, best = candidate.H(), distance
		}
	}
	b.nearestCrystal = nearest
}
func (b *EnderDragonBehaviour) tickPhase(e *Ent, tx *world.Tx) {
	switch b.phase {
	case DragonHoldingPattern:
		b.advanceDragonPathTarget(e.Position())
		if !b.targetSet || b.pathIndex >= b.pathLen-1 || e.Position().Sub(b.target).LenSqr() > 22500 {
			b.findHoldingTarget(e, tx)
		}
	case DragonStrafePlayer:
		b.tickStrafe(e, tx)
	case DragonLandingApproach:
		b.tickLandingApproach(e, tx)
	case DragonLanding:
		target := b.dragonPodiumTarget(tx)
		b.target, b.targetSet = target, true
		if e.Position().Sub(target).LenSqr() < 1 {
			b.flameCount = 0
			b.setPhase(DragonSittingScanning)
		}
	case DragonTakeoff:
		b.tickTakeoff(e, tx)
	case DragonSittingScanning:
		b.tickSittingScan(e, tx)
	case DragonSittingAttacking:
		if b.phaseTicks >= 40 {
			b.setPhase(DragonSittingFlaming)
		}
	case DragonSittingFlaming:
		if b.phaseTicks >= 200 {
			if b.flameCount >= 4 {
				b.setPhase(DragonTakeoff)
			} else {
				b.setPhase(DragonSittingScanning)
			}
		} else if b.phaseTicks == 10 {
			b.flameAttack(e, tx)
		}
	case DragonChargingPlayer:
		if !b.targetSet {
			b.setPhase(DragonHoldingPattern)
		} else if b.phaseTicks > 40 || e.Position().Sub(b.target).LenSqr() < 100 || e.Position().Sub(b.target).LenSqr() > 22500 {
			b.setPhase(DragonHoldingPattern)
		}
	case DragonDying:
		if !b.targetSet {
			b.target, b.targetSet = b.dragonPodiumTarget(tx), true
		}
		distance := b.target.Sub(e.Position()).LenSqr()
		health := 1.0
		if distance < 100 || distance > 22500 || b.movement.CollidedHorizontally() || b.movement.CollidedVertically() {
			health = 0
		}
		b.health.AddHealth(health - b.health.Health())
	case DragonHovering:
		b.target, b.targetSet = e.Position(), true
	}
}

func (b *EnderDragonBehaviour) findHoldingTarget(e *Ent, tx *world.Tx) {
	if b.pathLen == 0 || b.pathIndex >= b.pathLen-1 {
		egg := b.dragonPodiumTarget(tx)
		crystals := 0
		if b.fight != nil {
			crystals = b.fight.AliveCrystals()
		}
		if rand.IntN(crystals+3) == 0 {
			b.setPhase(DragonLandingApproach)
			return
		}
		player := dragonNearestPlayer(tx, egg, 128)
		if player != nil {
			distSqr := player.Position().Sub(egg).LenSqr() / 512
			if rand.IntN(max(1, int(distSqr+2))) == 0 || rand.IntN(crystals+2) == 0 {
				b.startDragonStrafe(e, tx, player)
				return
			}
		}
		current := b.closestDragonNode(tx, e.Position()[0], e.Position()[1], e.Position()[2])
		target := current
		if rand.IntN(8) == 0 {
			b.clockwise = !b.clockwise
			target += 6
		}
		if b.clockwise {
			target++
		} else {
			target--
		}
		if b.fight != nil && b.fight.AliveCrystals() >= 0 {
			target %= 12
			if target < 0 {
				target += 12
			}
		} else {
			target -= 12
			target &= 7
			target += 12
		}
		b.findDragonPath(tx, current, target, nil)
		b.nextDragonPathTarget()
	}
}

func (b *EnderDragonBehaviour) startDragonStrafe(e *Ent, tx *world.Tx, player world.Entity) {
	b.phaseTarget = player.H()
	b.setPhase(DragonStrafePlayer)
	b.phaseTarget = player.H()
	b.initializeStrafePath(e, tx, player.Position())
}

func (b *EnderDragonBehaviour) initializeStrafePath(e *Ent, tx *world.Tx, target mgl64.Vec3) {
	current := b.closestDragonNode(tx, e.Position()[0], e.Position()[1], e.Position()[2])
	targetNode := b.closestDragonNode(tx, target[0], target[1], target[2])
	x, z := math.Floor(target[0]), math.Floor(target[2])
	dx, dz := x-e.Position()[0], z-e.Position()[2]
	distance := math.Hypot(dx, dz)
	height := math.Min(.4+distance/80-1, 10)
	final := mgl64.Vec3{x, math.Floor(target[1] + height), z}
	if b.findDragonPath(tx, current, targetNode, &final) {
		b.nextDragonPathTarget()
	}
}

func (b *EnderDragonBehaviour) tickStrafe(e *Ent, tx *world.Tx) {
	if b.phaseTarget == nil {
		b.setPhase(DragonHoldingPattern)
		return
	}
	player, ok := b.phaseTarget.Entity(tx)
	if !ok {
		b.setPhase(DragonHoldingPattern)
		return
	}
	if b.pathLen > 0 {
		b.advanceDragonPathTarget(e.Position())
	}
	if b.pathLen == 0 || b.pathIndex >= b.pathLen-1 {
		target := player.Position()
		distance := math.Hypot(target[0]-e.Position()[0], target[2]-e.Position()[2])
		height := math.Min(.4+distance/80-1, 10)
		b.target = mgl64.Vec3{target[0], target[1] + height, target[2]}
		b.targetSet = true
	}
	distSqr := player.Position().Sub(e.Position()).LenSqr()
	parts := (&EnderDragon{Ent: e}).PartBoxes()
	head := parts[0].Box
	headPos := head.Min().Add(head.Max()).Mul(.5)
	playerAim := player.Position().Add(mgl64.Vec3{0, .5, 0})
	if distSqr < 4096 && dragonLineOfSight(tx, headPos, playerAim) {
		b.fireballCharge++
		aim := playerAim.Sub(headPos)
		aim[1] = 0
		if aim.Len() > 0 {
			aim = aim.Normalize()
		}
		yaw := float32(e.Rotation().Yaw()) * float32(math.Pi/180)
		direction := mgl64.Vec3{float64(java26.Sin(float64(yaw))), 0, -float64(java26.Cos(float64(yaw)))}
		dot := max(-1.0, min(1.0, direction.Dot(aim)))
		angle := math.Acos(dot)*180/math.Pi + .5
		if b.fireballCharge >= 5 && angle < 10 {
			start := headPos.Sub(direction).Add(mgl64.Vec3{0, .5, 0})
			spawnDragonFireballFrom(e, start, playerAim, tx)
			b.fireballCharge = 0
			b.pathIndex = b.pathLen
			b.setPhase(DragonHoldingPattern)
		}
	} else if b.fireballCharge > 0 {
		b.fireballCharge--
	}
	if e.Position().Sub(b.target).LenSqr() < 100 || e.Position().Sub(b.target).LenSqr() > 22500 {
		b.findStrafeTarget(e, tx)
	}
}

func (b *EnderDragonBehaviour) findStrafeTarget(e *Ent, tx *world.Tx) {
	if b.pathLen == 0 || b.pathIndex >= b.pathLen-1 {
		current := b.closestDragonNode(tx, e.Position()[0], e.Position()[1], e.Position()[2])
		next := current
		if rand.IntN(8) == 0 {
			b.clockwise = !b.clockwise
			next += 6
		}
		if b.clockwise {
			next++
		} else {
			next--
		}
		if b.fight != nil && b.fight.AliveCrystals() > 0 {
			next %= 12
			if next < 0 {
				next += 12
			}
		} else {
			next -= 12
			next &= 7
			next += 12
		}
		if b.findDragonPath(tx, current, next, nil) {
			b.nextDragonPathTarget()
		}
		return
	}
	b.advanceDragonPathTarget(e.Position())
}

func (b *EnderDragonBehaviour) tickLandingApproach(e *Ent, tx *world.Tx) {
	if b.pathLen == 0 {
		egg := b.dragonPodiumTarget(tx)
		player := dragonNearestPlayer(tx, egg, 256)
		var targetNode int
		if player != nil {
			aim := mgl64.Vec3{player.Position()[0], 0, player.Position()[2]}
			if aim.Len() > 0 {
				aim = aim.Normalize()
			}
			targetNode = b.closestDragonNode(tx, -aim[0]*40, 105, -aim[2]*40)
		} else {
			targetNode = b.closestDragonNode(tx, 40, egg[1], 0)
		}
		final := mgl64.Vec3{math.Floor(egg[0]), math.Floor(egg[1]), math.Floor(egg[2])}
		current := b.closestDragonNode(tx, e.Position()[0], e.Position()[1], e.Position()[2])
		if b.findDragonPath(tx, current, targetNode, &final) {
			b.nextDragonPathTarget()
		}
	} else {
		b.advanceDragonPathTarget(e.Position())
	}
	if b.pathLen > 0 && b.pathIndex >= b.pathLen-1 && e.Position().Sub(b.target).LenSqr() < 100 {
		b.setPhase(DragonLanding)
	}
}

func (b *EnderDragonBehaviour) tickTakeoff(e *Ent, tx *world.Tx) {
	if !b.firstTakeoffTick && b.pathLen > 0 {
		b.advanceDragonPathTarget(e.Position())
		egg := b.dragonPodiumTarget(tx)
		if e.Position().Sub(egg).LenSqr() >= 100 {
			b.setPhase(DragonHoldingPattern)
		}
		return
	}
	b.firstTakeoffTick = false
	current := b.closestDragonNode(tx, e.Position()[0], e.Position()[1], e.Position()[2])
	yaw := float32(e.Rotation().Yaw()) * float32(math.Pi/180)
	look := mgl64.Vec3{float64(java26.Sin(float64(yaw))), 0, -float64(java26.Cos(float64(yaw)))}
	target := b.closestDragonNode(tx, -look[0]*40, 105, -look[2]*40)
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
	if b.findDragonPath(tx, current, target, nil) {
		b.pathIndex++
		b.nextDragonPathTarget()
	}
}

func (b *EnderDragonBehaviour) tickSittingScan(e *Ent, tx *world.Tx) {
	b.scanTicks++
	var player world.Entity
	for candidate := range tx.Entities() {
		if candidate.H().Type().EncodeEntity() != "minecraft:player" {
			continue
		}
		delta := candidate.Position().Sub(e.Position())
		if delta.LenSqr() <= 400 && math.Abs(delta[1]) <= 10 && (player == nil || delta.LenSqr() < player.Position().Sub(e.Position()).LenSqr()) {
			player = candidate
		}
	}
	if player != nil {
		if b.scanTicks > 25 {
			b.setPhase(DragonSittingAttacking)
			return
		}
		toPlayer := player.Position().Sub(e.Position())
		toPlayer[1] = 0
		if toPlayer.Len() > 0 {
			toPlayer = toPlayer.Normalize()
		}
		yaw := float32(e.Rotation().Yaw()) * float32(math.Pi/180)
		forward := mgl64.Vec3{float64(java26.Sin(float64(yaw))), 0, -float64(java26.Cos(float64(yaw)))}
		angle := math.Acos(max(-1.0, min(1.0, forward.Dot(toPlayer))))*180/math.Pi + .5
		if angle < 0 || angle > 10 {
			head := (&EnderDragon{Ent: e}).PartBoxes()[0].Box.Min().Add((&EnderDragon{Ent: e}).PartBoxes()[0].Box.Max()).Mul(.5)
			dx, dz := player.Position()[0]-head[0], player.Position()[2]-head[2]
			turn := max(-100.0, min(100.0, wrapDragonDegrees(180-math.Atan2(dx, dz)*180/math.Pi-e.Rotation().Yaw())))
			b.yRotA *= .8
			distance := math.Hypot(dx, dz) + 1
			clampedDistance := min(distance, 40)
			b.yRotA += turn * (.7 / clampedDistance / distance)
			rotation := e.data.Rot
			rotation[0] += b.yRotA
			e.data.Rot = rotation
		}
		return
	}
	if b.scanTicks >= 100 {
		player = dragonNearestPlayer(tx, e.Position(), 150)
		b.setPhase(DragonTakeoff)
		if player != nil {
			b.target = player.Position()
			b.targetSet = true
			b.setPhase(DragonChargingPlayer)
		}
	}
}

func (b *EnderDragonBehaviour) dragonPodiumTarget(tx *world.Tx) mgl64.Vec3 {
	x, z := b.origin[0], b.origin[2]
	y := tx.JavaMotionBlockingHeight(x, z, false)
	return mgl64.Vec3{float64(x) + .5, float64(y), float64(z) + .5}
}

func (b *EnderDragonBehaviour) moveTowardTarget(e *Ent) {
	if !b.targetSet {
		return
	}
	delta := b.target.Sub(e.Position())
	distanceSqr := delta.LenSqr()
	if distanceSqr == 0 {
		return
	}
	speed := .6
	switch b.phase {
	case DragonHovering:
		speed = 1
	case DragonLanding:
		speed = 1.5
	case DragonChargingPlayer:
		speed = 3
	case DragonDying:
		speed = 3
	}
	horizontal := math.Hypot(delta[0], delta[2])
	vertical := delta[1]
	if horizontal > 0 {
		vertical = max(-speed, min(speed, vertical/horizontal))
	}
	velocity := e.data.Vel
	velocity[1] += vertical * .01
	rotation := e.data.Rot
	yaw := float32(rotation.Yaw()) * float32(math.Pi/180)
	aim := delta.Normalize()
	yawSin, yawCos := java26.Sin(float64(yaw)), java26.Cos(float64(yaw))
	direction := mgl64.Vec3{float64(yawSin), velocity[1], -float64(yawCos)}
	if direction.Len() > 0 {
		direction = direction.Normalize()
	}
	dot := max((direction.Dot(aim)+.5)/1.5, 0)
	span := 2 / (distanceSqr + 1)
	acceleration := .06 * (dot*span + 1 - span)
	velocity = velocity.Add(mgl64.Vec3{float64(yawSin) * acceleration, 0, -float64(yawCos) * acceleration})
	yawDelta := max(-50.0, min(50.0, wrapDragonDegrees(180-math.Atan2(delta[0], delta[2])*180/math.Pi-rotation.Yaw())))
	rotationBase := math.Hypot(velocity[0], velocity[2]) + 1
	rotationSpeed := .7 / (min(rotationBase, 40) * rotationBase)
	if b.phase == DragonLanding {
		rotationSpeed = min(rotationBase, 40) / rotationBase
	}
	b.yRotA = b.yRotA*.8 + yawDelta*rotationSpeed
	rotation[0] = wrapDragonDegrees(rotation[0] + b.yRotA*.1)
	directionYaw := float32(rotation[0]) * float32(math.Pi/180)
	direction = mgl64.Vec3{float64(java26.Sin(float64(directionYaw))), velocity[1], -float64(java26.Cos(float64(directionYaw)))}
	if direction.Len() > 0 {
		direction = direction.Normalize()
	}
	actual := velocity
	if actual.Len() > 0 {
		actual = actual.Normalize()
	}
	slide := .8 + .15*(actual.Dot(direction)+1)/2
	velocity = mgl64.Vec3{velocity[0] * slide, velocity[1] * .91, velocity[2] * slide}
	e.data.Vel, e.data.Rot = velocity, rotation
}

func (b *EnderDragonBehaviour) attackNearby(e *Ent, tx *world.Tx) {
	if b.phase == DragonDying || b.hurtTime > 0 {
		return
	}
	dragon := &EnderDragon{Ent: e}
	parts := dragon.PartBoxes()
	for _, part := range parts {
		switch part.Name {
		case "wing1", "wing2":
			box := part.Box.GrowVec3(mgl64.Vec3{4, 2, 4}).Translate(mgl64.Vec3{0, -2, 0})
			body := parts[2].Box.Min().Add(parts[2].Box.Max()).Mul(.5)
			for target := range tx.Entities() {
				living, ok := target.(Living)
				if !ok || target == dragon || !dragonCanAttackTarget(target) {
					continue
				}
				if !box.IntersectsWith(target.H().Type().BBox(target).Translate(target.Position())) {
					continue
				}
				delta := target.Position().Sub(body)
				distanceSquared := math.Max(delta[0]*delta[0]+delta[2]*delta[2], .1)
				push := mgl64.Vec3{delta[0] / distanceSquared * 4, .2, delta[2] / distanceSquared * 4}
				living.SetVelocity(living.Velocity().Add(push))
				if dragonSittingPhase(b.phase) {
					continue
				}
				if b.wingHitTicks == nil {
					b.wingHitTicks = make(map[*world.EntityHandle]int64)
				}
				lastHit, hitBefore := b.wingHitTicks[target.H()]
				if !hitBefore || b.tickCount-lastHit > 2 {
					b.wingHitTicks[target.H()] = b.tickCount
					living.Hurt(5, AttackDamageSource{Attacker: dragon})
				}
			}
		case "head", "neck":
			box := part.Box.Grow(1)
			for target := range tx.Entities() {
				living, ok := target.(Living)
				if !ok || target == dragon || !dragonCanAttackTarget(target) {
					continue
				}
				if !box.IntersectsWith(target.H().Type().BBox(target).Translate(target.Position())) {
					continue
				}
				living.Hurt(10, AttackDamageSource{Attacker: dragon})
			}
		}
	}
}

func dragonCanAttackTarget(target world.Entity) bool {
	gameMode, isPlayer := target.(interface{ GameMode() world.GameMode })
	return !isPlayer || gameMode.GameMode().AllowsTakingDamage()
}

func (b *EnderDragonBehaviour) flameAttack(e *Ent, tx *world.Tx) {
	parts := (&EnderDragon{Ent: e}).PartBoxes()
	head := parts[0].Box.Min().Add(parts[0].Box.Max()).Mul(.5)
	delta := head.Sub(e.Position())
	delta[1] = 0
	if delta.Len() > 0 {
		delta = delta.Normalize()
	}
	pos := head.Add(delta.Mul(2.5))
	for y := math.Floor(pos[1]); y >= 0; y-- {
		blockPos := cube.Pos{int(math.Floor(pos[0])), int(y), int(math.Floor(pos[2]))}
		if len(tx.Block(blockPos).Model().BBox(blockPos, tx)) != 0 {
			pos[1] = y + 1
			break
		}
	}
	cloud := NewAreaEffectCloudWith(world.EntitySpawnOpts{Position: pos}, potion.Harming(), 10*time.Second, time.Second/2, 0, 5, -.5, -.005)
	tx.AddEntity(cloud)
}

func (b *EnderDragonBehaviour) setPhase(p DragonPhase) {
	if b.phase == p {
		return
	}
	b.phase = p
	b.phaseTicks = 0
	b.clearDragonPath()
	b.phaseTarget = nil
	b.fireballCharge = 0
	if p == DragonTakeoff {
		b.firstTakeoffTick = true
	}
	if p == DragonSittingFlaming {
		b.flameCount++
	}
	if p == DragonSittingScanning {
		b.scanTicks = 0
	}
}
func (d *EnderDragon) Health() float64        { return d.behaviour().health.Health() }
func (d *EnderDragon) MaxHealth() float64     { return d.behaviour().health.MaxHealth() }
func (d *EnderDragon) SetMaxHealth(h float64) { d.behaviour().health.SetMaxHealth(h) }
func (d *EnderDragon) Dead() bool             { b := d.behaviour(); return b.dead || b.health.Health() <= 0 }
func (d *EnderDragon) Hurt(amount float64, src world.DamageSource) (float64, bool) {
	if attacker := dragonPlayerAttacker(src); attacker != nil {
		if part, _, ok := d.PartHitFrom(EyePosition(attacker), attacker.Rotation().Vec3(), 14); ok {
			return d.HurtPart(part, amount, src)
		}
	}
	if projectile, ok := src.(ProjectileDamageSource); ok && projectile.Projectile != nil {
		if part, hit := d.PartAt(projectile.Projectile.Position()); hit {
			return d.HurtPart(part, amount, src)
		}
	}
	return d.HurtPart("body", amount, src)
}

// HurtPart applies the vanilla part multiplier and player/dragon damage gate.
func (d *EnderDragon) HurtPart(part string, amount float64, src world.DamageSource) (float64, bool) {
	b := d.behaviour()
	if d.Dead() || amount < .01 || b.phase == DragonDying || !dragonDamageAllowed(src) {
		return 0, false
	}
	if part != "head" {
		amount = amount/4 + min(amount, 1)
	}
	old := d.Health()
	b.health.AddHealth(-amount)
	dealt := old - d.Health()
	if dragonSittingPhase(b.phase) {
		b.sittingDamage += dealt
		if b.sittingDamage > .25*d.MaxHealth() {
			b.sittingDamage = 0
			b.setPhase(DragonTakeoff)
		}
	} else if b.health.Health() <= 0 {
		b.health.AddHealth(1 - b.health.Health())
		b.setPhase(DragonDying)
	}
	b.hurtTime = 10
	d.viewAction(HurtAction{})
	return dealt, true
}

// CrystalDestroyed applies the dragon's crystal-dependent healing and current phase reaction.
func (d *EnderDragon) CrystalDestroyed(tx *world.Tx, crystal world.Entity, pos cube.Pos, source world.DamageSource) {
	b := d.behaviour()
	if b.nearestCrystal != nil && b.nearestCrystal == crystal.H() {
		d.HurtPart("head", 10, ExplosionDamageSource{Source: world.NewEntityExplosionSource(crystal, 6)})
	}
	player := dragonPlayerAttacker(source)
	if player == nil {
		player = dragonNearestPlayer(tx, pos.Vec3(), 64)
	}
	if b.phase == DragonHoldingPattern && player != nil {
		b.startDragonStrafe(d.Ent, tx, player)
	}
}

func dragonPlayerAttacker(src world.DamageSource) world.Entity {
	var attacker world.Entity
	switch source := src.(type) {
	case AttackDamageSource:
		attacker = source.Attacker
	case SpearDamageSource:
		attacker = source.Attacker
	case MaceSmashDamageSource:
		attacker = source.Attacker
	case ProjectileDamageSource:
		attacker = source.Owner
	}
	if attacker != nil && attacker.H().Type().EncodeEntity() == "minecraft:player" {
		return attacker
	}
	return nil
}

func dragonDamageAllowed(src world.DamageSource) bool {
	if dragonPlayerAttacker(src) != nil {
		return true
	}
	// Java's ALWAYS_HURTS_ENDER_DRAGONS tag expands to is_explosion.
	_, explosion := src.(ExplosionDamageSource)
	return explosion
}

func dragonSittingPhase(phase DragonPhase) bool {
	switch phase {
	case DragonSittingAttacking, DragonSittingFlaming, DragonSittingScanning, DragonHovering:
		return true
	default:
		return false
	}
}
func dragonNearestPlayer(tx *world.Tx, pos mgl64.Vec3, radius float64) world.Entity {
	var nearest world.Entity
	bestDistance := radius * radius
	for player := range tx.Players() {
		distance := player.Position().Sub(pos).LenSqr()
		if distance < bestDistance {
			nearest, bestDistance = player, distance
		}
	}
	return nearest
}
func (d *EnderDragon) Heal(amount float64, _ world.HealingSource) float64 {
	if d.Dead() || amount <= 0 {
		return 0
	}
	old := d.Health()
	d.behaviour().health.AddHealth(amount)
	return d.Health() - old
}
func (d *EnderDragon) KnockBack(_ mgl64.Vec3, _, _ float64)   {}
func (d *EnderDragon) AddEffect(e effect.Effect)              {}
func (d *EnderDragon) RemoveEffect(effect.Type)               {}
func (d *EnderDragon) Effects() []effect.Effect               { return nil }
func (d *EnderDragon) Speed() float64                         { return 0.6 }
func (d *EnderDragon) SetSpeed(float64)                       {}
func (d *EnderDragon) Phase() DragonPhase                     { return d.behaviour().phase }
func (d *EnderDragon) SetPhase(p DragonPhase)                 { d.behaviour().setPhase(p) }
func (d *EnderDragon) SetDragonFight(fight world.DragonFight) { d.behaviour().fight = fight }
func (d *EnderDragon) SetFightOrigin(origin cube.Pos)         { d.behaviour().origin = origin }
func (d *EnderDragon) FightOrigin() cube.Pos                  { return d.behaviour().origin }
func (d *EnderDragon) viewAction(a world.EntityAction) {
	vs := d.tx.Viewers(d.Position())
	for _, v := range vs {
		v.ViewEntityAction(d, a)
	}
	d.tx.ReleaseViewers(vs)
}

var _ world.EntityType = enderDragonType{}
var _ Living = (*EnderDragon)(nil)
