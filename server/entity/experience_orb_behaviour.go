package entity

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// experienceOrbSearchInterval defines how many world ticks we wait between target acquisition scans. Using the world
// tick counter keeps the behaviour deterministic even if the server wall clock is jittery under load.
const experienceOrbSearchInterval int64 = 20

// ExperienceOrbBehaviourConfig holds optional parameters for the creation of
// an ExperienceOrbBehaviour.
type ExperienceOrbBehaviourConfig struct {
	// Gravity is the amount of Y velocity subtracted every tick.
	Gravity float64
	// Drag is used to reduce all axes of the velocity every tick. Velocity is
	// multiplied with (1-Drag) every tick.
	Drag float64
	// ExistenceDuration specifies how long the experience orb should last. The
	// default is time.Minute * 5.
	ExistenceDuration time.Duration
	// Experience is the amount of experience held by the orb. Default is 1.
	Experience int
}

func (conf ExperienceOrbBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates an ExperienceOrbBehaviour using the parameters in conf.
func (conf ExperienceOrbBehaviourConfig) New() *ExperienceOrbBehaviour {
	if conf.Experience == 0 {
		conf.Experience = 1
	}
	if conf.ExistenceDuration == 0 {
		conf.ExistenceDuration = time.Minute * 5
	}
	b := &ExperienceOrbBehaviour{conf: conf, lastSearchTick: -experienceOrbSearchInterval}
	b.passive = PassiveBehaviourConfig{
		Gravity:           conf.Gravity,
		Drag:              conf.Drag,
		ExistenceDuration: conf.ExistenceDuration,
		Tick:              b.tick,
		Move:              b.move,
	}.New()
	return b
}

// ExperienceOrbBehaviour implements Behaviour for an experience orb entity.
type ExperienceOrbBehaviour struct {
	conf ExperienceOrbBehaviourConfig

	passive *PassiveBehaviour

	// lastSearchTick tracks when we last performed a collector search using the world's logical tick counter.
	// Relying on ticks instead of time.Now() avoids jitter when the server slows down and ensures orbs wake up
	// immediately once the simulation resumes at full speed.
	lastSearchTick int64
	target         *world.EntityHandle
}

// PortalTravelComputer returns the interdimensional travel state for the behaviour.
func (exp *ExperienceOrbBehaviour) PortalTravelComputer() *PortalTravelComputer {
	return exp.passive.PortalTravelComputer()
}

// Experience returns the amount of experience the orb carries.
func (exp *ExperienceOrbBehaviour) Experience() int {
	return exp.conf.Experience
}

// Tick finds a target for the experience orb and moves the orb towards it.
func (exp *ExperienceOrbBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	return exp.passive.Tick(e, tx)
}

// move ports the movement of ExperienceOrb.tick: an orb rises in water, is
// tossed about in lava, follows its collector and bounces when it lands.
func (exp *ExperienceOrbBehaviour) move(e *Ent, tx *world.Tx, mc *MovementComputer) *Movement {
	pos, vel := e.data.Pos, e.data.Vel
	velBefore := vel
	bbox := e.H().Type().BBox(e)
	box := bbox.Translate(pos)
	vel = measureFluids(tx, box, true).push(tx, vel)
	colliding := boxCollides(tx, box)
	if orbEyeInWater(tx, pos, bbox) {
		vel = mgl64.Vec3{vel[0] * float64(float32(.99)), math.Min(vel[1]+float64(float32(5e-4)), float64(float32(.06))), vel[2] * float64(float32(.99))}
	} else if !colliding {
		vel[1] -= exp.conf.Gravity
	}
	if l, ok := tx.Liquid(cube.PosFromVec3(pos)); ok && l.LiquidType() == "lava" {
		vel = mgl64.Vec3{float64((rand.Float32() - rand.Float32()) * .2), float64(float32(.2)), float64((rand.Float32() - rand.Float32()) * .2)}
	}
	vel = exp.follow(e, tx, vel)

	fallSpeed := vel[1]
	dPos, vel := mc.CheckCollision(tx, e, pos, vel)
	friction := float32(1 - exp.conf.Drag)
	if mc.OnGround() {
		below := cube.PosFromVec3(pos.Add(dPos).Sub(mgl64.Vec3{0, float64(float32(.999999))}))
		blockFriction := float32(.6)
		if f, ok := tx.Block(below).(interface{ Friction() float64 }); ok {
			blockFriction = float32(f.Friction())
		}
		friction *= blockFriction
	}
	vel = vel.Mul(float64(friction))
	if mc.CollidedVertically() && fallSpeed < 0 && fallSpeed < -exp.conf.Gravity {
		vel[1] = -fallSpeed * .4
	}
	pos = pos.Add(dPos)

	viewers := tx.Viewers(e.data.Pos)
	return &Movement{v: viewers, release: func() { tx.ReleaseViewers(viewers) }, e: e,
		pos: pos, vel: vel, dpos: dPos, dvel: vel.Sub(velBefore), rot: e.data.Rot, onGround: mc.OnGround()}
}

// orbEyeInWater reports if the eyes of an orb at pos are in water, as
// Entity.isEyeInFluid.
func orbEyeInWater(tx *world.Tx, pos mgl64.Vec3, bbox cube.BBox) bool {
	eyeY := pos[1] + bbox.Height()*.85
	eye := cube.PosFromVec3(mgl64.Vec3{pos[0], eyeY, pos[2]})
	l, ok := tx.Liquid(eye)
	return ok && l.LiquidType() == "water" && eyeY <= float64(eye[1])+liquidHeight(tx, eye, l)
}

// boxCollides reports if box overlaps a collision box of a block, as
// Level.noCollision reports the opposite.
func boxCollides(tx *world.Tx, box cube.BBox) bool {
	for _, b := range blockBBoxsAround(tx, box) {
		if b.IntersectsWith(box) {
			return true
		}
	}
	return false
}

// follow ports ExperienceOrb.followNearbyPlayer: an orb is pulled towards
// the collector it follows, harder the closer it is.
func (exp *ExperienceOrbBehaviour) follow(e *Ent, tx *world.Tx, vel mgl64.Vec3) mgl64.Vec3 {
	targetEnt, ok := exp.target.Entity(tx)
	target, _ := targetEnt.(experienceCollector)
	if !ok || target == nil || target.Dead() {
		return vel
	}
	dst := target.Position()
	if o, ok := target.(Eyed); ok {
		dst[1] += o.EyeHeight() / 2
	}
	delta := dst.Sub(e.Position())
	if delta.LenSqr() > 64 {
		return vel
	}
	power := 1 - delta.Len()/8
	return vel.Add(javaNormalize(delta).Mul(power * power * .1))
}

// followBox is the bounding box used to search for collectors to follow for experience orbs.
var followBox = cube.Box(-8, -8, -8, 8, 8, 8)

// tick finds a target for the experience orb and moves the orb towards it.
func (exp *ExperienceOrbBehaviour) tick(e *Ent, tx *world.Tx) {
	targetEnt, ok := exp.target.Entity(tx)
	target, _ := targetEnt.(experienceCollector)

	pos := e.Position()
	hasTarget := ok && !target.Dead() && pos.Sub(target.Position()).Len() <= 8
	currentTick := tx.World().CurrentTick()
	// The world tick counter advances even if the main loop momentarily stalls. Anchoring the search cadence to
	// it keeps orb pursuit smooth under load and matches how other behaviours pace themselves.
	if !hasTarget {
		if currentTick-exp.lastSearchTick >= experienceOrbSearchInterval {
			exp.findTarget(tx, pos, currentTick)
		}
	} else {
		exp.moveToTarget(e, target, tx)
	}
}

// findTarget attempts to find a target for an experience orb in w around pos.
func (exp *ExperienceOrbBehaviour) findTarget(tx *world.Tx, pos mgl64.Vec3, currentTick int64) {
	exp.target = nil
	for o := range tx.EntitiesWithin(followBox.Translate(pos)) {
		if ec, ok := o.(experienceCollector); ok && ec.CanCollectExperience() {
			exp.target = o.H()
			break
		}
	}
	exp.lastSearchTick = currentTick
}

// moveToTarget collects the experience orb once it touches its target.
func (exp *ExperienceOrbBehaviour) moveToTarget(e *Ent, target experienceCollector, tx *world.Tx) {
	pos := e.Position()
	if e.H().Type().BBox(e).Translate(pos).IntersectsWith(target.H().Type().BBox(target).Translate(target.Position())) && target.CollectExperience(exp.conf.Experience) {
		_ = e.CloseIn(tx)
	}
}

// experienceCollector represents an entity that can collect experience orbs.
type experienceCollector interface {
	Living
	// CollectExperience makes the player collect the experience points passed,
	// adding it to the experience manager. A bool is returned indicating
	// whether the player was able to collect the experience, or not, due to the
	// 100ms delay between experience collection.
	CollectExperience(value int) bool
	// CanCollectExperience returns whether the player can collect experience or not.
	CanCollectExperience() bool
}
