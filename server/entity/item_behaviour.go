package entity

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// ItemBehaviourConfig holds optional parameters for an ItemBehaviour.
type ItemBehaviourConfig struct {
	Item item.Stack
	// Gravity is the amount of Y velocity subtracted every tick.
	Gravity float64
	// Drag is used to reduce all axes of the velocity every tick. Velocity is
	// multiplied with (1-Drag) every tick.
	Drag float64
	// ExistenceDuration specifies how long the item stack should last. The
	// default is time.Minute * 5.
	ExistenceDuration time.Duration
	// PickupDelay specifies how much time must expire before the item can be
	// picked up by collectors. The default is time.Second / 2.
	PickupDelay time.Duration
}

func (conf ItemBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates an ItemBehaviour using i and the optional parameters in conf. An
// optional block registry is used when normalising block items.
func (conf ItemBehaviourConfig) New(registries ...world.BlockRegistry) *ItemBehaviour {
	i := conf.Item
	if i.Count() > i.MaxCount() {
		i = i.Grow(i.MaxCount() - i.Count())
	}
	i = item.ReadNBT(item.WriteNBT(i, true), nil, registries...)

	if conf.PickupDelay == 0 {
		conf.PickupDelay = time.Second / 2
	}
	if conf.ExistenceDuration == 0 {
		conf.ExistenceDuration = time.Minute * 5
	}

	b := &ItemBehaviour{conf: conf, i: i, pickupDelay: conf.PickupDelay}
	b.passive = PassiveBehaviourConfig{
		Gravity:           conf.Gravity,
		Drag:              conf.Drag,
		ExistenceDuration: conf.ExistenceDuration,
		Tick:              b.tick,
		Move:              b.move,
	}.New()
	b.passive.mc.DragAfterMovement = true
	return b
}

// ItemBehaviour implements the behaviour of item entities.
type ItemBehaviour struct {
	conf    ItemBehaviourConfig
	passive *PassiveBehaviour
	i       item.Stack

	pickupDelay time.Duration
}

// PortalTravelComputer returns the interdimensional travel state for the behaviour.
func (i *ItemBehaviour) PortalTravelComputer() *PortalTravelComputer {
	return i.passive.PortalTravelComputer()
}

// Item returns the item.Stack held by the entity.
func (i *ItemBehaviour) Item() item.Stack {
	return i.i
}

// Tick moves the entity, checks if it should be picked up by a nearby collector
// or if it should merge with nearby item entities.
func (i *ItemBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if e.OnFireDuration() > 0 {
		if fireproof, ok := i.Item().Item().(item.FireProof); !(ok && fireproof.FireProof()) {
			tx.PlaySound(e.Position(), sound.FireExtinguish{})
			_ = e.Close()
			return nil
		}
	}

	pos := cube.PosFromVec3(e.Position())
	blockPos := pos.Side(cube.FaceDown)

	if i.burnsInHazard(e, tx, pos, blockPos) {
		return nil
	}

	bl, ok := tx.Block(blockPos).(block.Hopper)
	if ok && !bl.Powered && bl.CollectCooldown <= 0 {
		addedCount, _ := bl.Inventory(tx, blockPos).AddItem(i.i)
		if addedCount == 0 {
			return i.passive.Tick(e, tx)
		}
		bl.CollectCooldown = 8
		tx.SetBlock(blockPos, bl, nil)
		if addedCount == i.i.Count() {
			_ = e.CloseIn(tx)
			return nil
		}
		i.setCount(e, i.i.Count()-addedCount, tx)
		return i.passive.Tick(e, tx)
	}
	return i.passive.Tick(e, tx)
}

// move ports the movement of ItemEntity.tick: an item floats up in water
// and lava, is carried by their currents and bounces when it lands.
func (i *ItemBehaviour) move(e *Ent, tx *world.Tx, mc *MovementComputer) *Movement {
	pos, vel := e.data.Pos, e.data.Vel
	velBefore := vel
	bbox := e.H().Type().BBox(e)
	// Entity.baseTick updates the fluids the item is in before it moves.
	fluids := measureFluids(tx, bbox.Translate(pos), true)
	vel = fluids.push(tx, vel)
	switch {
	case fluids.waterHeight > float64(float32(.1)):
		vel = itemFluidMovement(vel, float64(float32(.99)))
	case fluids.lavaHeight > float64(float32(.1)):
		vel = itemFluidMovement(vel, float64(float32(.95)))
	default:
		vel[1] -= i.conf.Gravity
	}
	var dPos mgl64.Vec3
	ticks := int64(e.Age() / (time.Second / 20))
	if !mc.OnGround() || vel[0]*vel[0]+vel[2]*vel[2] > float64(float32(1e-5)) || (ticks+int64(e.H().UUID()[15]))%4 == 0 {
		dPos, vel = mc.CheckCollision(tx, e, pos, vel)
		airDrag := float32(1 - i.conf.Drag)
		groundFriction := airDrag
		if mc.OnGround() {
			below := cube.PosFromVec3(pos.Add(dPos).Sub(mgl64.Vec3{0, float64(float32(.999999))}))
			friction := float32(.6)
			if f, ok := tx.Block(below).(interface{ Friction() float64 }); ok {
				friction = float32(f.Friction())
			}
			groundFriction *= friction
		}
		vel = mgl64.Vec3{vel[0] * float64(groundFriction), vel[1] * float64(airDrag), vel[2] * float64(groundFriction)}
		if mc.OnGround() && vel[1] < 0 {
			vel[1] *= -.5
		}
	}
	pos = pos.Add(dPos)
	// ItemEntity.tick updates the fluids once more after the item moved.
	vel = measureFluids(tx, bbox.Translate(pos), true).push(tx, vel)

	viewers := tx.Viewers(e.data.Pos)
	return &Movement{v: viewers, release: func() { tx.ReleaseViewers(viewers) }, e: e,
		pos: pos, vel: vel, dpos: dPos, dvel: vel.Sub(velBefore), rot: e.data.Rot, onGround: mc.OnGround()}
}

// itemFluidMovement ports ItemEntity.setFluidMovement: an item rises slowly
// in a liquid and loses speed.
func itemFluidMovement(vel mgl64.Vec3, multiplier float64) mgl64.Vec3 {
	if vel[1] < float64(float32(.06)) {
		vel[1] += float64(float32(5e-4))
	}
	return mgl64.Vec3{vel[0] * multiplier, vel[1], vel[2] * multiplier}
}

// Explode reacts to explosions. The item entity is destroyed, unless the item
// type is blast proof.
func (i *ItemBehaviour) Explode(e *Ent, _ world.ExplosionSource, impact float64) {
	if impact > 0 {
		if expl, ok := i.Item().Item().(interface{ BlastProof() bool }); ok && expl.BlastProof() {
			return
		}
		_ = e.Close()
	}
}

// Prick reacts to contact with cactus. The item entity is destroyed unless the item type is prick proof.
func (i *ItemBehaviour) Prick(e *Ent, blockPos cube.Pos) {
	if _, cactus := e.tx.Block(blockPos).(block.Cactus); !cactus {
		return
	}
	if p, ok := i.Item().Item().(interface{ PrickProof() bool }); ok && p.PrickProof() {
		return
	}
	_ = e.Close()
}

// tick checks if the item can be picked up or merged with nearby item stacks.
func (i *ItemBehaviour) tick(e *Ent, tx *world.Tx) {
	if i.pickupDelay == 0 {
		i.checkNearby(e, tx)
	} else if i.pickupDelay < math.MaxInt16*(time.Second/20) {
		i.pickupDelay -= time.Second / 20
	}
}

// checkNearby checks the nearby entities for item collectors and other item
// stacks. If a collector is found in range, the item will be picked up. If
// another item stack with the same item type is found in range, the item
// stacks will merge.
func (i *ItemBehaviour) checkNearby(e *Ent, tx *world.Tx) {
	pos := e.Position()
	bbox := e.H().Type().BBox(e)
	grown := bbox.GrowVec3(mgl64.Vec3{1, 0.5, 1}).Translate(pos)

	for other := range tx.EntitiesWithin(bbox.Translate(pos).Grow(2)) {
		if e.H() == other.H() || !other.H().Type().BBox(other).Translate(other.Position()).IntersectsWith(grown) {
			continue
		}
		if collector, ok := other.(Collector); ok {
			// A collector was within range to pick up the entity.
			i.collect(e, collector, tx)
			return
		} else if other.H().Type() == ItemType {
			// Another item entity was in range to merge with.
			if i.merge(e, other.(*Ent), tx) {
				return
			}
		}
	}
}

// merge merges the item entity with another item entity.
func (i *ItemBehaviour) merge(e *Ent, other *Ent, tx *world.Tx) bool {
	otherBehaviour := other.Behaviour().(*ItemBehaviour)
	if otherBehaviour.i.Count() == otherBehaviour.i.MaxCount() || i.i.Count() == i.i.MaxCount() || !i.i.Comparable(otherBehaviour.i) {
		// Either stack is already filled up to the maximum, meaning we can't
		// change anything any way, other the stack types weren't comparable.
		return false
	}
	// Keep the larger stack's actor, position, motion and client animation.
	// Replacing both actors restarts bobbing and makes airborne loot snap.
	source, target := e, other
	sourceBehaviour, targetBehaviour := i, otherBehaviour
	if i.i.Count() > otherBehaviour.i.Count() {
		source, target = other, e
		sourceBehaviour, targetBehaviour = otherBehaviour, i
	}
	count := min(sourceBehaviour.i.Count(), targetBehaviour.i.MaxCount()-targetBehaviour.i.Count())
	targetBehaviour.pickupDelay = max(targetBehaviour.pickupDelay, sourceBehaviour.pickupDelay)
	target.data.Age = min(target.data.Age, source.data.Age)
	targetBehaviour.setCount(target, targetBehaviour.i.Count()+count, tx)
	if count == sourceBehaviour.i.Count() {
		_ = source.CloseIn(tx)
	} else {
		sourceBehaviour.setCount(source, sourceBehaviour.i.Count()-count, tx)
	}
	return true
}

// setCount updates the existing item actor rather than despawning it. Bedrock's
// UpdateStackSize event changes the client stack without resetting its motion.
func (i *ItemBehaviour) setCount(e *Ent, count int, tx *world.Tx) {
	i.i = i.i.Grow(count - i.i.Count())
	viewers := tx.Viewers(e.Position())
	for _, v := range viewers {
		v.ViewEntityAction(e, ItemStackSizeAction{Count: count})
	}
	tx.ReleaseViewers(viewers)
}

// collect makes a collector collect the item (or at least part of it).
func (i *ItemBehaviour) collect(e *Ent, collector Collector, tx *world.Tx) {
	n, _ := collector.Collect(i.i)
	if n == 0 {
		return
	}
	if n == i.i.Count() {
		// TakeItemActor removes the client actor, so only send it when the
		// whole stack was collected. A partial pickup updates its count.
		viewers := tx.Viewers(e.Position())
		for _, viewer := range viewers {
			viewer.ViewEntityAction(e, PickedUpAction{Collector: collector})
		}
		tx.ReleaseViewers(viewers)
		_ = e.CloseIn(tx)
		return
	}
	i.setCount(e, i.i.Count()-n, tx)
}

func (i *ItemBehaviour) burnsInHazard(e *Ent, tx *world.Tx, positions ...cube.Pos) bool {
	for _, p := range positions {
		if hazardConsumesItems(tx.Block(p), nil) {
			_ = e.CloseIn(tx)
			return true
		}
		if liquid, ok := tx.Liquid(p); ok && hazardConsumesItems(nil, liquid) {
			_ = e.CloseIn(tx)
			return true
		}
	}
	return false
}

func hazardConsumesItems(bl world.Block, liquid world.Liquid) bool {
	switch bl.(type) {
	case block.Fire:
		return true
	case block.Lava:
		return true
	}
	if liquid != nil {
		if _, ok := liquid.(block.Lava); ok {
			return true
		}
		if liquid.LiquidType() == "lava" {
			return true
		}
	}
	return false
}

// Collector represents an entity in the world that is able to collect an item, typically an entity such as
// a player or a zombie.
type Collector interface {
	world.Entity
	// Collect collects the stack passed. It is called if the Collector is standing near an item entity that
	// may be picked up.
	// The count of items collected from the stack n is returned, along with a
	// bool that indicates if the Collector was in a state where it could
	// collect any items in the first place.
	Collect(stack item.Stack) (n int, ok bool)
}
