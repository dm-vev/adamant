package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math"
	"sync"
)

// MovementComputer is used to compute movement of an entity. When constructed, the Gravity of the entity
// the movement is computed for must be passed.
type MovementComputer struct {
	Gravity, Drag     float64
	DragBeforeGravity bool

	onGround             bool
	collidedHorizontally bool
	collidedVertically   bool
	CollideEntities      bool
}

// blockBBoxPool caches scratch slices used while expanding collision boxes around an entity during movement
// resolution. The collider path runs every tick for every moving entity, so eliminating these temporary allocations
// materially reduces GC churn in crowded areas.
var blockBBoxPool = sync.Pool{
	New: func() any {
		return make([]cube.BBox, 0, 16)
	},
}

// Movement represents the movement of a world.Entity as a result of a call to MovementComputer.TickMovement. The
// resulting position and velocity can be obtained by calling Position and Velocity. These can be sent to viewers by
// calling Send.
type Movement struct {
	v                    []world.Viewer
	release              func()
	e                    world.Entity
	pos, vel, dpos, dvel mgl64.Vec3
	rot                  cube.Rotation
	onGround             bool
	rotChanged           bool
}

// Send sends the Movement to any viewers watching the entity at the time of the movement. If the position/velocity
// changes were negligible, nothing is sent.
func (m *Movement) Send() {
	posChanged := !m.dpos.ApproxEqualThreshold(zeroVec3, epsilon)
	velChanged := !m.dvel.ApproxEqualThreshold(zeroVec3, epsilon)

	for _, v := range m.v {
		if posChanged || m.rotChanged {
			v.ViewEntityMovement(m.e, m.pos, m.rot, m.onGround)
		}
		if velChanged {
			v.ViewEntityVelocity(m.e, m.vel)
		}
	}
	if m.release != nil {
		m.release()
		m.release = nil
	}
}

// Position returns the position as a result of the Movement as an mgl64.Vec3.
func (m *Movement) Position() mgl64.Vec3 {
	return m.pos
}

// Velocity returns the velocity after the Movement as an mgl64.Vec3.
func (m *Movement) Velocity() mgl64.Vec3 {
	return m.vel
}

// Rotation returns the rotation, yaw and pitch, of the entity after the Movement.
func (m *Movement) Rotation() cube.Rotation {
	return m.rot
}

// TickMovement performs a movement tick on an entity. Velocity is applied and changed according to the values
// of its Drag and Gravity.
// The new position of the entity after movement is returned.
// The resulting Movement can be sent to viewers by calling Movement.Send.
func (c *MovementComputer) TickMovement(e world.Entity, pos, vel mgl64.Vec3, rot, prevRot cube.Rotation, tx *world.Tx) *Movement {
	viewers := tx.Viewers(pos)

	velBefore := vel
	vel = c.applyHorizontalForces(tx, pos, c.applyVerticalForces(vel))
	dPos, vel := c.CheckCollision(tx, e, pos, vel)

	return &Movement{v: viewers, release: func() { tx.ReleaseViewers(viewers) }, e: e,
		pos: pos.Add(dPos), vel: vel, dpos: dPos, dvel: vel.Sub(velBefore),
		rot: rot, onGround: c.onGround, rotChanged: rotationChanged(rot, prevRot),
	}
}

// OnGround checks if the entity that this computer calculates is currently on the ground.
func (c *MovementComputer) OnGround() bool {
	return c.onGround
}

// CollidedHorizontally reports if the last tick movement hit a horizontal surface.
func (c *MovementComputer) CollidedHorizontally() bool {
	return c.collidedHorizontally
}

// CollidedVertically reports if the last tick movement hit a vertical surface.
func (c *MovementComputer) CollidedVertically() bool {
	return c.collidedVertically
}

// zeroVec3 is a mgl64.Vec3 with zero values.
var zeroVec3 mgl64.Vec3

// epsilon is the epsilon used for thresholds for change used for change in position and velocity.
const epsilon = 0.001

func rotationChanged(rot, prev cube.Rotation) bool {
	const rotationEpsilon = 0.01
	if math.Abs(rot.Yaw()-prev.Yaw()) > rotationEpsilon {
		return true
	}
	return math.Abs(rot.Pitch()-prev.Pitch()) > rotationEpsilon
}

// applyVerticalForces applies gravity and drag on the Y axis, based on the Gravity and Drag values set.
func (c *MovementComputer) applyVerticalForces(vel mgl64.Vec3) mgl64.Vec3 {
	if c.DragBeforeGravity {
		vel[1] *= 1 - c.Drag
	}
	vel[1] -= c.Gravity
	if !c.DragBeforeGravity {
		vel[1] *= 1 - c.Drag
	}
	return vel
}

// applyHorizontalForces applies friction to the velocity based on the Drag value, reducing it on the X and Z axes.
func (c *MovementComputer) applyHorizontalForces(tx *world.Tx, pos, vel mgl64.Vec3) mgl64.Vec3 {
	friction := 1 - c.Drag
	if c.onGround {
		if f, ok := tx.Block(cube.PosFromVec3(pos).Side(cube.FaceDown)).(interface {
			Friction() float64
		}); ok {
			friction *= f.Friction()
		} else {
			friction *= 0.6
		}
	}
	vel[0] *= friction
	vel[2] *= friction
	return vel
}

// CheckCollision handles the collision of the entity with blocks, adapting the velocity of the entity if it
// happens to collide with a block.
// The final velocity and the Vec3 that the entity should move is returned.
func (c *MovementComputer) CheckCollision(tx *world.Tx, e world.Entity, pos, vel mgl64.Vec3) (mgl64.Vec3, mgl64.Vec3) {
	return c.checkCollision(tx, e, pos, vel)
}

func (c *MovementComputer) checkCollision(tx *world.Tx, e world.Entity, pos, vel mgl64.Vec3) (mgl64.Vec3, mgl64.Vec3) {
	deltaX, deltaY, deltaZ := vel[0], vel[1], vel[2]

	// Entities only ever have a single bounding box.
	entityBBox := e.H().Type().BBox(e).Translate(pos)
	blocks := blockBBoxsAround(tx, entityBBox.Extend(vel))
	var entities []cube.BBox
	if c.CollideEntities {
		entities = entityBBoxsAround(tx, e, entityBBox.Extend(vel))
	}

	if !mgl64.FloatEqualThreshold(deltaY, 0, epsilon) {
		// First we move the entity BBox on the Y axis.
		for _, blockBBox := range blocks {
			deltaY = entityBBox.YOffset(blockBBox, deltaY)
		}
		for _, colliderBBox := range entities {
			deltaY = entityBBox.YOffset(colliderBBox, deltaY)
		}
		entityBBox = entityBBox.Translate(mgl64.Vec3{0, deltaY})
	}
	if !mgl64.FloatEqualThreshold(deltaX, 0, epsilon) {
		// Then on the X axis.
		for _, blockBBox := range blocks {
			deltaX = entityBBox.XOffset(blockBBox, deltaX)
		}
		for _, colliderBBox := range entities {
			deltaX = entityBBox.XOffset(colliderBBox, deltaX)
		}
		entityBBox = entityBBox.Translate(mgl64.Vec3{deltaX})
	}
	if !mgl64.FloatEqualThreshold(deltaZ, 0, epsilon) {
		// And finally on the Z axis.
		for _, blockBBox := range blocks {
			deltaZ = entityBBox.ZOffset(blockBBox, deltaZ)
		}
		for _, colliderBBox := range entities {
			deltaZ = entityBBox.ZOffset(colliderBBox, deltaZ)
		}
	}
	if !mgl64.FloatEqual(vel[1], 0) {
		// The Y velocity of the entity is currently not 0, meaning it is moving either up or down. We can
		// then assume the entity is not currently on the ground.
		c.onGround = false
	}
	c.collidedHorizontally = !mgl64.FloatEqual(deltaX, vel[0]) || !mgl64.FloatEqual(deltaZ, vel[2])
	c.collidedVertically = !mgl64.FloatEqual(deltaY, vel[1])
	if !mgl64.FloatEqual(deltaX, vel[0]) {
		vel[0] = 0
	}
	if !mgl64.FloatEqual(deltaY, vel[1]) {
		// The entity either hit the ground or hit the ceiling.
		if vel[1] < 0 {
			// The entity was going down, so we can assume it is now on the ground.
			c.onGround = true
		}
		vel[1] = 0
	}
	if !mgl64.FloatEqual(deltaZ, vel[2]) {
		vel[2] = 0
	}
	blockBBoxPool.Put(blocks[:0])
	if c.CollideEntities {
		entityBBoxPool.Put(entities[:0])
	}
	return mgl64.Vec3{deltaX, deltaY, deltaZ}, vel
}

// blockBBoxsAround returns all blocks around the entity passed, using the BBox passed to make a prediction of
// what blocks need to have their BBox returned.
func blockBBoxsAround(tx *world.Tx, box cube.BBox) []cube.BBox {
	grown := box.Grow(0.25)
	min, max := grown.Min(), grown.Max()
	minX, minY, minZ := int(math.Floor(min[0])), int(math.Floor(min[1])), int(math.Floor(min[2]))
	maxX, maxY, maxZ := int(math.Ceil(max[0])), int(math.Ceil(max[1])), int(math.Ceil(max[2]))

	// A prediction of one BBox per block, plus an additional 2, in case
	blockBBoxs := blockBBoxPool.Get().([]cube.BBox)
	required := (maxX - minX) * (maxY - minY) * (maxZ - minZ)
	if cap(blockBBoxs) < required+2 {
		blockBBoxPool.Put(blockBBoxs[:0])
		blockBBoxs = make([]cube.BBox, 0, required+2)
	} else {
		blockBBoxs = blockBBoxs[:0]
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			for z := minZ; z <= maxZ; z++ {
				pos := cube.Pos{x, y, z}
				block := tx.Block(pos)
				// We deliberately avoid caching bounding boxes across different positions. Many
				// block models (for example fences, walls, or trapdoors) inspect neighbouring
				// blocks through the BlockSource passed to BBox in order to decide which collision
				// shape to expose. Two blocks with the same runtime hash can therefore still
				// produce different shapes when their surroundings diverge, so memoising purely by
				// BlockHash would leak the first configuration into subsequent checks within the
				// same sweep. The extra allocations here are negligible compared to the correctness
				// issues introduced by reusing the wrong shape.
				boxes := block.Model().BBox(pos, tx)
				if len(boxes) == 0 {
					continue
				}
				offset := mgl64.Vec3{float64(x), float64(y), float64(z)}
				for _, box := range boxes {
					blockBBoxs = append(blockBBoxs, box.Translate(offset))
				}
			}
		}
	}
	return blockBBoxs
}

// entityBBoxPool caches scratch slices used while expanding collision boxes around an entity during movement
// resolution when checking collisions with other living entities.
var entityBBoxPool = sync.Pool{
	New: func() any {
		return make([]cube.BBox, 0, 8)
	},
}

// entityBBoxsAround returns all entity bounding boxes around the entity passed, using the BBox passed to make a
// prediction of what entities need to have their BBox returned.
func entityBBoxsAround(tx *world.Tx, self world.Entity, box cube.BBox) []cube.BBox {
	if _, ok := self.(Living); !ok {
		return entityBBoxPool.Get().([]cube.BBox)[:0]
	}
	expanded := box.Grow(0.5)
	entityBBoxs := entityBBoxPool.Get().([]cube.BBox)
	entityBBoxs = entityBBoxs[:0]
	for e := range tx.EntitiesWithin(expanded) {
		if e == self {
			continue
		}
		l, ok := e.(Living)
		if !ok || l.Dead() {
			continue
		}
		entityBBoxs = append(entityBBoxs, e.H().Type().BBox(e).Translate(e.Position()))
	}
	return entityBBoxs
}
