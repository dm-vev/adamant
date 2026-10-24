package entity

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
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

// dragonEyeHeight is the eye height of the 16 by 8 ender dragon.
const dragonEyeHeight = 8 * .85

// The indices of the dragon's parts, in the order of EnderDragon.subEntities.
const (
	dragonPartHead = iota
	dragonPartNeck
	dragonPartBody
	dragonPartTail1
	dragonPartTail2
	dragonPartTail3
	dragonPartWing1
	dragonPartWing2
)

// dragonParts are the names, widths and heights of the EnderDragonParts.
var dragonParts = [8]struct {
	name          string
	width, height float64
}{
	{"head", 1, 1}, {"neck", 3, 3}, {"body", 5, 3},
	{"tail1", 2, 2}, {"tail2", 2, 2}, {"tail3", 2, 2},
	{"wing1", 4, 2}, {"wing2", 4, 2},
}

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
		"DragonDeathTime": int32(b.deathTime), "sitting_damage_received": b.sittingDamage,
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
	b := &EnderDragonBehaviour{health: NewHealthManager(health, 200), movement: &MovementComputer{NoBlockCollisions: true}, origin: c.FightOrigin, deathTime: c.DeathTime, sittingDamage: float32(c.SittingDamage), fight: c.Fight, flightHead: -1}
	// readAdditionalSaveData sets the phase through the manager, so that it
	// begins.
	b.setPhase(nil, phase)
	return b
}

// dragonFlightSample ports DragonFlightHistory.Sample.
type dragonFlightSample struct {
	y   float64
	yaw float32
}

type EnderDragonBehaviour struct {
	health        *HealthManager
	movement      *MovementComputer
	phase         DragonPhase
	phaseSet      bool
	ph            dragonPhases
	tickCount     int64
	origin        cube.Pos
	deathTime     int
	sittingDamage float32
	fight         world.DragonFight

	nearestCrystal *world.EntityHandle
	flight         [64]dragonFlightSample
	flightHead     int
	yRotA          float32
	inWall         bool

	// parts are the positions of the parts, the bottom centres of their
	// boxes, as EnderDragonPart positions.
	parts    [8]mgl64.Vec3
	partsSet bool

	hurtTime         int
	invulnerableTime int
	lastHurt         float32
	wingHitTicks     map[*world.EntityHandle]int64
	dead             bool

	nodes            [24]cube.Pos
	nodesInitialized bool
}

// EnderDragon runs the server-side dragon phase machine and flight behavior.
type EnderDragon struct{ *Ent }

func (d *EnderDragon) behaviour() *EnderDragonBehaviour { return d.Behaviour().(*EnderDragonBehaviour) }

// DragonPartBox describes one independently hittable Java dragon body part.
type DragonPartBox struct {
	Name string
	Box  cube.BBox
}

// PartBoxes returns the boxes of the dragon's head, neck, body, three tail
// segments and two wings, where its last tick put them.
func (d *EnderDragon) PartBoxes() [8]DragonPartBox {
	b := d.behaviour()
	if !b.partsSet {
		b.placeParts(d.Ent, false)
	}
	var boxes [8]DragonPartBox
	for i := range boxes {
		boxes[i] = DragonPartBox{Name: dragonParts[i].name, Box: b.partBox(i)}
	}
	return boxes
}

// partBox returns the box of a part, as Entity.getBoundingBox of the part.
func (b *EnderDragonBehaviour) partBox(i int) cube.BBox {
	p, w, h := b.parts[i], dragonParts[i].width/2, dragonParts[i].height
	return cube.Box(p[0]-w, p[1], p[2]-w, p[0]+w, p[1]+h, p[2]+w)
}

// PartAt resolves a point against the dragon's body parts, preferring head/neck.
func (d *EnderDragon) PartAt(pos mgl64.Vec3) (string, bool) {
	for _, part := range d.PartBoxes() {
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

// Tick ports the ticks of the EnderDragon: tickDeath while it dies, and
// otherwise its aiStep, which runs its phase, flies it at the target of the
// phase and moves its parts.
func (b *EnderDragonBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.dead {
		return nil
	}
	b.tickCount++
	if b.hurtTime > 0 {
		b.hurtTime--
	}
	if b.invulnerableTime > 0 {
		b.invulnerableTime--
	}
	if b.health.Health() <= 0 {
		return b.tickDeath(e, tx)
	}
	startRot := e.data.Rot
	b.checkCrystals(e, tx)
	e.data.Rot[0] = float64(wrapDegrees32(float32(e.data.Rot[0])))
	b.recordFlight(e)
	phase := b.phase
	b.tickPhase(e, tx)
	if b.phase != phase {
		b.tickPhase(e, tx)
	}
	m := b.fly(e, tx, startRot)
	b.placeParts(e, true)
	if b.fight != nil {
		b.fight.UpdateDragon(tx, &EnderDragon{Ent: e})
	}
	return m
}

// tickDeath ports EnderDragon.tickDeath: the dragon rises for ten seconds,
// dropping experience in its last fifty ticks, and then ends the fight.
func (b *EnderDragonBehaviour) tickDeath(e *Ent, tx *world.Tx) *Movement {
	dragon := &EnderDragon{Ent: e}
	if b.fight != nil {
		b.fight.UpdateDragon(tx, dragon)
	}
	b.deathTime++
	if b.deathTime >= 180 && b.deathTime <= 200 {
		offset := mgl64.Vec3{float64((rand.Float32() - .5) * 8), 2 + float64((rand.Float32()-.5)*4), float64((rand.Float32() - .5) * 8)}
		tx.AddParticle(e.Position().Add(offset), particle.HugeExplosion{})
	}
	xp := 500
	if b.fight != nil && !b.fight.HasPreviouslyKilledDragon() {
		xp = 12000
	}
	drops := tx.World().GameRuleBool("mob_drops")
	if b.deathTime > 150 && b.deathTime%5 == 0 && drops {
		for _, orb := range NewExperienceOrbs(e.Position(), int(math.Floor(float64(float32(xp)*.08)))) {
			tx.AddEntity(orb)
		}
	}
	if b.deathTime == 1 {
		dragon.viewAction(DragonDeathAction{})
		tx.PlayGlobalSound(e.Position(), sound.Custom{Name: "mob.enderdragon.death", Volume: 10, Pitch: 1})
	}
	m := b.movement.TickMovement(e, e.Position(), mgl64.Vec3{0, float64(float32(.1)), 0}, e.Rotation(), e.Rotation(), tx)
	e.data.Pos, e.data.Rot = m.Position(), m.Rotation()
	m.vel, m.dvel = e.data.Vel, mgl64.Vec3{}
	for i := range b.parts {
		b.parts[i][1] += float64(float32(.1))
	}
	if b.deathTime >= 200 {
		if drops {
			for _, orb := range NewExperienceOrbs(e.Position(), int(math.Floor(float64(float32(xp)*.2)))) {
				tx.AddEntity(orb)
			}
		}
		if b.fight != nil {
			b.fight.SetDragonKilled(tx, dragon)
		}
		b.dead = true
		_ = e.CloseIn(tx)
		m.releaseViewers()
		return nil
	}
	return m
}

func (b *EnderDragonBehaviour) recordFlight(e *Ent) {
	sample := dragonFlightSample{e.Position()[1], float32(e.Rotation().Yaw())}
	if b.flightHead < 0 {
		for i := range b.flight {
			b.flight[i] = sample
		}
	}
	b.flightHead = (b.flightHead + 1) & 63
	b.flight[b.flightHead] = sample
}

func (b *EnderDragonBehaviour) flightSample(delay int, e *Ent) dragonFlightSample {
	if b.flightHead < 0 {
		return dragonFlightSample{e.Position()[1], float32(e.Rotation().Yaw())}
	}
	return b.flight[(b.flightHead-delay)&63]
}

// checkCrystals ports EnderDragon.checkCrystals: a crystal within 32 blocks
// heals the dragon one point every half second.
func (b *EnderDragonBehaviour) checkCrystals(e *Ent, tx *world.Tx) {
	if b.nearestCrystal != nil {
		if _, ok := b.nearestCrystal.Entity(tx); !ok {
			b.nearestCrystal = nil
		} else if b.tickCount%10 == 0 && b.health.Health() < b.health.MaxHealth() {
			b.health.AddHealth(1)
		}
	}
	if rand.IntN(10) != 0 {
		return
	}
	box := e.H().Type().BBox(e).Translate(e.Position()).Grow(32)
	var nearest *world.EntityHandle
	best := math.MaxFloat64
	for candidate := range tx.EntitiesWithin(box) {
		if c, ok := candidate.(*EndCrystal); !ok || c.behaviour().exploded {
			continue
		}
		if d := candidate.Position().Sub(e.Position()).LenSqr(); d < best {
			nearest, best = candidate.H(), d
		}
	}
	b.nearestCrystal = nearest
}

// fly ports the flight of EnderDragon.aiStep: the dragon turns and speeds
// towards the target of its phase, and hangs in the air without one.
func (b *EnderDragonBehaviour) fly(e *Ent, tx *world.Tx, prevRot cube.Rotation) *Movement {
	b.movement.Drag, b.movement.Gravity = 0, 0
	target := b.flyTarget()
	if !target.set {
		m := b.movement.TickMovement(e, e.Position(), mgl64.Vec3{}, e.data.Rot, prevRot, tx)
		e.data.Rot = m.Rotation()
		m.vel, m.dvel = e.data.Vel, mgl64.Vec3{}
		return m
	}
	pos, vel := e.Position(), e.data.Vel
	oldVel := vel
	xdd, ydd, zdd := target.pos[0]-pos[0], target.pos[1]-pos[1], target.pos[2]-pos[2]
	distToTarget := xdd*xdd + ydd*ydd + zdd*zdd
	maxSpeed := float64(b.flySpeed())
	if horizontal := math.Sqrt(xdd*xdd + zdd*zdd); horizontal > 0 {
		ydd = mgl64.Clamp(ydd/horizontal, -maxSpeed, maxSpeed)
	}
	vel[1] += ydd * .01
	yaw := wrapDegrees32(float32(e.data.Rot[0]))
	aim := javaNormalize(target.pos.Sub(pos))
	yawRad := yaw * float32(math.Pi/180)
	dir := javaNormalize(mgl64.Vec3{float64(vanilla.Sin(float64(yawRad))), vel[1], -float64(vanilla.Cos(float64(yawRad)))})
	dot := max((float32(dir.Dot(aim))+.5)/1.5, 0)
	if math.Abs(xdd) > 1e-5 || math.Abs(zdd) > 1e-5 {
		yRotD := mgl64.Clamp(float64(wrapDegrees32(180-float32(vanilla.Atan2(xdd, zdd))*(180/float32(math.Pi))-yaw)), -50, 50)
		b.yRotA *= .8
		b.yRotA += float32(yRotD) * b.turnSpeed(vel)
		yaw += b.yRotA * .1
	}
	span := float32(2 / (distToTarget + 1))
	speed := float64(.06 * (dot*span + (1 - span)))
	yawRad = yaw * float32(math.Pi/180)
	vel = vel.Add(mgl64.Vec3{speed * float64(vanilla.Sin(float64(yawRad))), 0, -speed * float64(vanilla.Cos(float64(yawRad)))})
	move := vel
	if b.inWall {
		move = vel.Mul(float64(float32(.8)))
	}
	rot := cube.Rotation{float64(yaw), e.data.Rot[1]}
	m := b.movement.TickMovement(e, pos, move, rot, prevRot, tx)
	actual := javaNormalize(vel)
	slide := .8 + .15*(actual.Dot(dir)+1)/2
	vel = mgl64.Vec3{vel[0] * slide, vel[1] * float64(float32(.91)), vel[2] * slide}
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), vel, m.Rotation()
	m.vel, m.dvel = vel, vel.Sub(oldVel)
	return m
}

// placeParts ports the part placement of EnderDragon.aiStep. Between the
// body and wings and the head, the dragon pushes away what its wings touch
// and hurts what its head and neck touched, as Java does with the head and
// neck where the last tick put them.
func (b *EnderDragonBehaviour) placeParts(e *Ent, live bool) {
	pos := e.Position()
	yaw := float32(e.Rotation().Yaw())
	p5, p10 := b.flightSample(5, e), b.flightSample(10, e)
	tilt := float32(p5.y-p10.y) * 10 * float32(math.Pi/180)
	ccTilt, ssTilt := vanilla.Cos(float64(tilt)), vanilla.Sin(float64(tilt))
	rot1 := yaw * float32(math.Pi/180)
	ss1, cc1 := vanilla.Sin(float64(rot1)), vanilla.Cos(float64(rot1))
	b.parts[dragonPartBody] = pos.Add(mgl64.Vec3{float64(ss1 * .5), 0, float64(-cc1 * .5)})
	b.parts[dragonPartWing1] = pos.Add(mgl64.Vec3{float64(cc1 * 4.5), 2, float64(ss1 * 4.5)})
	b.parts[dragonPartWing2] = pos.Add(mgl64.Vec3{float64(cc1 * -4.5), 2, float64(ss1 * -4.5)})
	if live && b.hurtTime <= 0 {
		dragon := &EnderDragon{Ent: e}
		for _, wing := range [2]int{dragonPartWing1, dragonPartWing2} {
			b.knockBack(dragon, b.partBox(wing).GrowVec3(mgl64.Vec3{4, 2, 4}).Translate(mgl64.Vec3{0, -2, 0}))
		}
		if b.partsSet {
			b.hurtTouching(dragon, b.partBox(dragonPartHead).Grow(1))
			b.hurtTouching(dragon, b.partBox(dragonPartNeck).Grow(1))
		}
	}
	headRot := yaw*float32(math.Pi/180) - b.yRotA*.01
	ss2, cc2 := vanilla.Sin(float64(headRot)), vanilla.Cos(float64(headRot))
	yOffset := float32(-1)
	if !dragonSitting(b.phase) {
		yOffset = float32(p5.y - b.flightSample(0, e).y)
	}
	b.parts[dragonPartHead] = pos.Add(mgl64.Vec3{float64(ss2 * 6.5 * ccTilt), float64(yOffset + ssTilt*6.5), float64(-cc2 * 6.5 * ccTilt)})
	b.parts[dragonPartNeck] = pos.Add(mgl64.Vec3{float64(ss2 * 5.5 * ccTilt), float64(yOffset + ssTilt*5.5), float64(-cc2 * 5.5 * ccTilt)})
	for i := range 3 {
		p0 := b.flightSample(12+i*2, e)
		r := yaw*float32(math.Pi/180) + wrapDegrees32(p0.yaw-p5.yaw)*float32(math.Pi/180)
		ss, cc := vanilla.Sin(float64(r)), vanilla.Cos(float64(r))
		dd := float32(i+1) * 2
		b.parts[dragonPartTail1+i] = pos.Add(mgl64.Vec3{
			float64(-(ss1*1.5 + ss*dd) * ccTilt),
			p0.y - p5.y - float64((dd+1.5)*ssTilt) + 1.5,
			float64((cc1*1.5 + cc*dd) * ccTilt),
		})
	}
	b.partsSet = true
	if live {
		tx := e.tx
		head := b.checkWalls(tx, b.partBox(dragonPartHead))
		neck := b.checkWalls(tx, b.partBox(dragonPartNeck))
		body := b.checkWalls(tx, b.partBox(dragonPartBody))
		b.inWall = head || neck || body
	}
}

// dragonAffected ports EntitySelector.NO_CREATIVE_OR_SPECTATOR for living
// entities.
func dragonAffected(dragon *EnderDragon, e world.Entity) bool {
	if _, ok := e.(Living); !ok || e.H() == dragon.H() {
		return false
	}
	if g, ok := e.(interface{ GameMode() world.GameMode }); ok {
		mode := g.GameMode()
		return mode.AllowsTakingDamage() && mode.HasCollision()
	}
	return true
}

// knockBack ports EnderDragon.knockBack: the wings push living entities
// away from the body and, unless the dragon sits, hurt them.
func (b *EnderDragonBehaviour) knockBack(dragon *EnderDragon, box cube.BBox) {
	tx := dragon.tx
	body := b.partBox(dragonPartBody)
	xm, zm := (body.Min()[0]+body.Max()[0])/2, (body.Min()[2]+body.Max()[2])/2
	for target := range tx.EntitiesWithin(box) {
		if !dragonAffected(dragon, target) || !box.IntersectsWith(target.H().Type().BBox(target).Translate(target.Position())) {
			continue
		}
		living := target.(Living)
		xd, zd := target.Position()[0]-xm, target.Position()[2]-zm
		dd := math.Max(xd*xd+zd*zd, .1)
		living.SetVelocity(living.Velocity().Add(mgl64.Vec3{xd / dd * 4, float64(float32(.2)), zd / dd * 4}))
		if dragonSitting(b.phase) {
			continue
		}
		if b.wingHitTicks == nil {
			b.wingHitTicks = make(map[*world.EntityHandle]int64)
		}
		if last, ok := b.wingHitTicks[target.H()]; !ok || b.tickCount-last > 2 {
			b.wingHitTicks[target.H()] = b.tickCount
			living.Hurt(5, AttackDamageSource{Attacker: dragon})
		}
	}
}

// hurtTouching ports EnderDragon.hurt of a list of entities: the head and
// neck bite what they touch for 10 damage.
func (b *EnderDragonBehaviour) hurtTouching(dragon *EnderDragon, box cube.BBox) {
	for target := range dragon.tx.EntitiesWithin(box) {
		if !dragonAffected(dragon, target) || !box.IntersectsWith(target.H().Type().BBox(target).Translate(target.Position())) {
			continue
		}
		target.(Living).Hurt(10, AttackDamageSource{Attacker: dragon})
	}
}

// checkWalls ports EnderDragon.checkWalls: the dragon flies through what is
// not dragon immune while mob griefing is on, and is slowed by it otherwise.
func (b *EnderDragonBehaviour) checkWalls(tx *world.Tx, box cube.BBox) bool {
	x0, y0, z0 := int(math.Floor(box.Min()[0])), int(math.Floor(box.Min()[1])), int(math.Floor(box.Min()[2]))
	x1, y1, z1 := int(math.Floor(box.Max()[0])), int(math.Floor(box.Max()[1])), int(math.Floor(box.Max()[2]))
	griefing := !mobGriefingDisabled(tx)
	hitWall, destroyed := false, false
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				pos := cube.Pos{x, y, z}
				if pos.OutOfBounds(tx.Range()) {
					continue
				}
				bl := tx.Block(pos)
				if _, air := bl.(block.Air); air || blockInTag(bl, "minecraft:dragon_transparent") {
					continue
				}
				if !griefing || blockInTag(bl, "minecraft:dragon_immune") {
					hitWall = true
					continue
				}
				// Level.removeBlock leaves the fluid of the block, so that a
				// fluid itself is left as it is.
				if _, liquid := bl.(world.Liquid); liquid {
					continue
				}
				tx.SetBlock(pos, nil, nil)
				destroyed = true
			}
		}
	}
	if destroyed {
		at := cube.Pos{x0 + rand.IntN(x1-x0+1), y0 + rand.IntN(y1-y0+1), z0 + rand.IntN(z1-z0+1)}
		tx.AddParticle(at.Vec3(), particle.DragonDestroyBlock{})
	}
	return hitWall
}

func (d *EnderDragon) Health() float64        { return d.behaviour().health.Health() }
func (d *EnderDragon) MaxHealth() float64     { return d.behaviour().health.MaxHealth() }
func (d *EnderDragon) SetMaxHealth(h float64) { d.behaviour().health.SetMaxHealth(h) }
func (d *EnderDragon) Dead() bool             { b := d.behaviour(); return b.dead || b.health.Health() <= 0 }

// Hurt hurts the part of the dragon that was hit: the part a player looks
// at or a projectile is in, and the body otherwise.
func (d *EnderDragon) Hurt(amount float64, src world.DamageSource) (float64, bool) {
	if attacker := dragonPlayerAttacker(src); attacker != nil {
		if _, melee := src.(AttackDamageSource); melee {
			if part, _, ok := d.PartHitFrom(EyePosition(attacker), attacker.Rotation().Vec3(), 14); ok {
				return d.HurtPart(part, amount, src)
			}
		}
	}
	if projectile, ok := src.(ProjectileDamageSource); ok && projectile.Projectile != nil {
		if part, hit := d.PartAt(projectile.Projectile.Position()); hit {
			return d.HurtPart(part, amount, src)
		}
	}
	return d.HurtPart("body", amount, src)
}

// HurtPart ports EnderDragon.hurt of a part: arrows and wind charges bounce
// off a sitting dragon, parts other than the head and neck take a quarter of
// the damage, and only players and explosions hurt it at all.
func (d *EnderDragon) HurtPart(part string, amount float64, src world.DamageSource) (float64, bool) {
	b := d.behaviour()
	if d.Dead() || b.phase == DragonDying {
		return 0, false
	}
	damage := float32(amount)
	if projectile, ok := src.(ProjectileDamageSource); ok && projectile.Projectile != nil && b.phase != DragonHovering && dragonSitting(b.phase) {
		switch projectile.Projectile.H().Type().EncodeEntity() {
		case "minecraft:arrow", "minecraft:spectral_arrow", "minecraft:thrown_trident", "minecraft:wind_charge_projectile", "minecraft:breeze_wind_charge_projectile":
			if f, ok := projectile.Projectile.(interface{ SetOnFire(time.Duration) }); ok {
				f.SetOnFire(time.Second)
			}
			damage = 0
		}
	}
	if part != "head" && part != "neck" {
		damage = damage/4 + min(damage, 1)
	}
	if damage < .01 {
		return 0, false
	}
	if !dragonDamageAllowed(src) {
		return 0, true
	}
	before := d.Health()
	if !b.reallyHurt(d, damage) {
		return 0, true
	}
	dealt := before - d.Health()
	if dragonSitting(b.phase) {
		b.sittingDamage += float32(dealt)
		if b.sittingDamage > .25*float32(d.MaxHealth()) {
			b.sittingDamage = 0
			b.setPhase(d.tx, DragonTakeoff)
		}
	}
	return dealt, true
}

// reallyHurt ports LivingEntity.hurtServer for the dragon: damage within
// ten ticks of the last hurts only as far as it exceeds it, and a killing
// blow to a flying dragon leaves it one point to fly to its death with.
func (b *EnderDragonBehaviour) reallyHurt(d *EnderDragon, damage float32) bool {
	if b.invulnerableTime > 10 {
		if damage <= b.lastHurt {
			return false
		}
		damage, b.lastHurt = damage-b.lastHurt, damage
	} else {
		b.lastHurt, b.invulnerableTime = damage, 20
		b.hurtTime = 10
		d.viewAction(HurtAction{})
	}
	b.health.AddHealth(-float64(damage))
	if b.health.Health() <= 0 && !dragonSitting(b.phase) {
		// EnderDragon.handleKillingBlow.
		b.health.AddHealth(1 - b.health.Health())
		b.setPhase(d.tx, DragonDying)
	}
	return true
}

// Explode hurts every part an explosion reaches, as Java's explosions hurt
// the dragon's parts each as entities, and pushes the dragon away.
func (d *EnderDragon) Explode(src world.ExplosionSource, impact float64) {
	b := d.behaviour()
	center := src.Position()
	diameter := src.Size() * 2
	explosion := ExplosionDamageSource{Source: src}
	if impact > 0 {
		d.HurtPart("body", (impact*impact+impact)/2*7*diameter+1, explosion)
		push := javaNormalize(mgl64.Vec3{d.Position()[0] - center[0], d.Position()[1] + dragonEyeHeight - center[1], d.Position()[2] - center[2]})
		d.data.Vel = d.data.Vel.Add(push.Mul(impact))
	}
	for i := range b.parts {
		dist := b.parts[i].Sub(center).Len() / diameter
		if dist > 1 {
			continue
		}
		pow := (1 - dist) * block.ExplosionSeenPercent(d.tx, center, b.partBox(i))
		d.HurtPart(dragonParts[i].name, (pow*pow+pow)/2*7*diameter+1, explosion)
	}
}

// CrystalDestroyed ports EnderDragon.onCrystalDestroyed: the crystal the
// dragon heals from hurts its head as it explodes, and a circling dragon
// turns on the player who destroyed it.
func (d *EnderDragon) CrystalDestroyed(tx *world.Tx, crystal world.Entity, pos cube.Pos, source world.DamageSource) {
	b := d.behaviour()
	player := dragonPlayerAttacker(source)
	if player == nil && tx.World().Difficulty() != world.DifficultyPeaceful {
		player = dragonNearestPlayer(tx, d.Ent, pos.Vec3(), 0, false, nil)
	}
	if b.nearestCrystal != nil && b.nearestCrystal == crystal.H() {
		d.HurtPart("head", 10, ExplosionDamageSource{Source: world.NewEntityExplosionSource(crystal, 6)})
	}
	b.crystalDestroyed(d.Ent, tx, player)
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

func (d *EnderDragon) Heal(amount float64, _ world.HealingSource) float64 {
	if d.Dead() || amount <= 0 {
		return 0
	}
	old := d.Health()
	d.behaviour().health.AddHealth(amount)
	return d.Health() - old
}

// KnockBack ports EnderDragon.knockback: a flying dragon is knocked back as
// any living entity in the air, and a sitting one not at all.
func (d *EnderDragon) KnockBack(src mgl64.Vec3, force, _ float64) {
	if dragonSitting(d.behaviour().phase) || force <= 0 {
		return
	}
	away := javaNormalize(mgl64.Vec3{d.Position()[0] - src[0], 0, d.Position()[2] - src[2]}).Mul(force)
	v := d.data.Vel
	d.data.Vel = mgl64.Vec3{v[0]/2 + away[0], v[1], v[2]/2 + away[2]}
}

func (d *EnderDragon) AddEffect(e effect.Effect)              {}
func (d *EnderDragon) RemoveEffect(effect.Type)               {}
func (d *EnderDragon) Effects() []effect.Effect               { return nil }
func (d *EnderDragon) Speed() float64                         { return 0.6 }
func (d *EnderDragon) SetSpeed(float64)                       {}
func (d *EnderDragon) EyeHeight() float64                     { return dragonEyeHeight }
func (d *EnderDragon) Phase() DragonPhase                     { return d.behaviour().phase }
func (d *EnderDragon) SetPhase(p DragonPhase)                 { d.behaviour().setPhase(d.tx, p) }
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

// dragonLineOfSight reports if no block intersects the line between two
// points.
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
