package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"sync"
	"time"
)

// Behaviour implements the behaviour of an Ent.
type Behaviour interface {
	// Tick ticks the Ent using the Behaviour. A Movement is returned that
	// specifies the movement of the entity over the tick. Nil may be returned
	// if the entity did not move.
	Tick(e *Ent, tx *world.Tx) *Movement
}

// ItemAcceptor is an interface that Behaviours or world.Entity may implement to accept items from players.
type ItemAcceptor interface {
	// AcceptItem returns whether the entity accepts the item stack passed. This may be called when a player tries to
	// interact with the entity.
	AcceptItem(from world.Entity, tx *world.Tx, ctx *item.UseContext) bool
}

// Attackable is an interface that Behaviours or world.Entity may implement to allow entities to be attacked by other
// entities.
type Attackable interface {
	// Attack is called when the entity is attacked by an attacker. Unlike world.Living entities, no damage value is
	// passed. Used for entities that do not have health but can still be interacted with through attacks like armour
	// stands.
	Attack(attacker world.Entity, tx *world.Tx)
}

type dismounter interface {
	DismountAll(e *Ent, tx *world.Tx)
}

// Ent is a world.Entity implementation that allows entity implementations to
// share a lot of code. It is currently under development and is prone to
// (breaking) changes.
type Ent struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	once   sync.Once
}

// Open converts a world.EntityHandle to an Ent in a world.Tx.
func Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) *Ent {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (e *Ent) H() *world.EntityHandle {
	return e.handle
}

func (e *Ent) Behaviour() Behaviour {
	return e.data.Data.(Behaviour)
}

// ProjectileOwner exposes the projectile behaviour's owner without retaining
// a transaction-bound entity in delayed game events.
func (e *Ent) ProjectileOwner() *world.EntityHandle {
	if owned, ok := e.Behaviour().(interface{ Owner() *world.EntityHandle }); ok {
		return owned.Owner()
	}
	return nil
}

// Explode propagates the explosion behaviour of the underlying Behaviour. A
// living entity without one is hurt and pushed away as
// ServerExplosion.hurtEntities hurts and pushes it.
func (e *Ent) Explode(src world.ExplosionSource, impact float64) {
	if expl, ok := e.Behaviour().(interface {
		Explode(e *Ent, src world.ExplosionSource, impact float64)
	}); ok {
		expl.Explode(e, src, impact)
		return
	}
	living, ok := e.living().(Living)
	if !ok {
		return
	}
	living.Hurt((impact*impact+impact)/2*7*src.Size()*2+1, ExplosionDamageSource{Source: src})
	if !e.handle.Closed() {
		living.SetVelocity(living.Velocity().Add(ExplosionImpulse(living, src, impact)))
	}
}

// Prick propagates the prick behaviour of the underlying Behaviour.
func (e *Ent) Prick(blockPos cube.Pos) {
	if pricker, ok := e.Behaviour().(interface {
		Prick(e *Ent, blockPos cube.Pos)
	}); ok {
		pricker.Prick(e, blockPos)
	}
}

// HurtEntities makes a falling block entity hurt entities it lands on by
// damagePerBlock per block fallen, up to maxDamage.
func (e *Ent) HurtEntities(damagePerBlock, maxDamage float64) {
	if f, ok := e.Behaviour().(*FallingBlockBehaviour); ok {
		f.HurtEntities(damagePerBlock, maxDamage)
	}
}

// Position returns the current position of the entity.
func (e *Ent) Position() mgl64.Vec3 {
	return e.data.Pos
}

// currentTx returns the transaction the entity was opened in.
func (e *Ent) currentTx() *world.Tx {
	return e.tx
}

// Velocity returns the current velocity of the entity. The values in the Vec3 returned represent the speed on
// that axis in blocks/tick.
func (e *Ent) Velocity() mgl64.Vec3 {
	return e.data.Vel
}

// SetVelocity sets the velocity of the entity. The values in the Vec3 passed represent the speed on
// that axis in blocks/tick.
func (e *Ent) SetVelocity(v mgl64.Vec3) {
	e.data.Vel = v
}

// Teleport teleports the entity to the position given.
func (e *Ent) Teleport(pos mgl64.Vec3) {
	viewers := e.tx.Viewers(e.data.Pos)
	e.data.Pos = pos
	for _, v := range viewers {
		v.ViewEntityTeleport(e, pos)
	}
	e.tx.ReleaseViewers(viewers)
	e.movesShown(pos, e.data.Rot, e.data.Rot.Yaw())
}

// TeleportThroughGateway preserves ordinary entities' motion and rotation.
// Thrown ender pearls use the source transition with absolute zero motion.
func (e *Ent) TeleportThroughGateway(pos mgl64.Vec3) {
	if e.H().Type() == EnderPearlType {
		e.data.Vel = mgl64.Vec3{}
		e.data.Rot = cube.Rotation{}
	}
	e.Teleport(pos)
}

// Displace moves the entity by a relative delta.
func (e *Ent) Displace(deltaPos mgl64.Vec3) {
	e.Teleport(e.Position().Add(deltaPos))
}

// Rotation returns the rotation of the entity.
func (e *Ent) Rotation() cube.Rotation {
	return e.data.Rot
}
func (e *Ent) setRotation(rotation cube.Rotation) {
	e.data.Rot = rotation
}

// Move moves the entity by a delta and turns it by a delta yaw and pitch, as
// commands like /tp and /rotate do.
func (e *Ent) Move(deltaPos mgl64.Vec3, deltaYaw, deltaPitch float64) {
	pos := e.data.Pos.Add(deltaPos)
	rot := cube.Rotation{e.data.Rot.Yaw() + deltaYaw, e.data.Rot.Pitch() + deltaPitch}
	viewers := e.tx.Viewers(e.data.Pos)
	e.data.Pos, e.data.Rot = pos, rot
	for _, v := range viewers {
		v.ViewEntityMovement(e, pos, rot, e.data.OnGround)
	}
	e.tx.ReleaseViewers(viewers)
	e.movesShown(pos, rot, e.HeadYaw())
}

// Age returns the total time lived of this entity. It increases by
// time.Second/20 for every time Tick is called.
func (e *Ent) Age() time.Duration {
	return e.data.Age
}

// OnFireDuration ...
func (e *Ent) OnFireDuration() time.Duration {
	return e.data.FireDuration
}

// fireImmuneTypes are the entity types Java builds fireImmune.
var fireImmuneTypes = map[string]bool{
	"minecraft:area_effect_cloud": true, "minecraft:blaze": true, "minecraft:end_crystal": true,
	"minecraft:ender_dragon": true, "minecraft:ghast": true, "minecraft:magma_cube": true,
	"minecraft:shulker": true, "minecraft:strider": true, "minecraft:tnt": true, "minecraft:vex": true,
	"minecraft:warden": true, "minecraft:wither": true, "minecraft:wither_skeleton": true,
	"minecraft:zoglin": true, "minecraft:zombified_piglin": true,
}

// FireImmune reports if fire and lava neither burn nor hurt the entity, as
// Entity.fireImmune.
func (e *Ent) FireImmune() bool { return fireImmuneTypes[e.handle.Type().EncodeEntity()] }

// SetOnFire sets the entity on fire for the duration passed, unless it is
// immune to fire, which Entity.baseTick keeps from burning.
func (e *Ent) SetOnFire(duration time.Duration) {
	duration = max(duration, 0)
	if e.FireImmune() {
		duration = 0
	}
	stateChanged := (e.data.FireDuration > 0) != (duration > 0)

	e.data.FireDuration = duration
	if stateChanged {
		e.updateState()
	}
}

// tickFire ports the fire of Entity.baseTick: a mob on fire takes a point of
// damage every second it is out of lava, until the fire burns out.
func (e *Ent) tickFire(tx *world.Tx) {
	if e.data.FireDuration <= 0 {
		return
	}
	if ticks := int64(e.data.FireDuration / (time.Second / 20)); ticks%20 == 0 {
		l, inLiquid := tx.Liquid(cube.PosFromVec3(e.data.Pos))
		if living, ok := e.living().(Living); ok && !(inLiquid && l.LiquidType() == "lava") {
			living.Hurt(1, block.FireDamageSource{})
		}
	}
	e.SetOnFire(e.data.FireDuration - time.Second/20)
}

// inRain ports Entity.isInRain: rain falls on the feet or the top of the
// entity.
func (e *Ent) inRain(tx *world.Tx) bool {
	top := e.data.Pos.Add(mgl64.Vec3{0, e.H().Type().BBox(e.living()).Height()})
	return tx.RainingAt(cube.PosFromVec3(e.data.Pos)) || tx.RainingAt(cube.PosFromVec3(top))
}

// Riding returns the entity that this entity is riding, if any.
func (e *Ent) Riding() *world.EntityHandle {
	return e.data.Riding
}

// SetRiding updates the entity that this entity is riding.
func (e *Ent) SetRiding(r *world.EntityHandle) {
	e.data.Riding = r
}

// RiderSeat returns the seat of the vehicle the entity rides, or last rode.
func (e *Ent) RiderSeat() world.RiderSeat {
	return e.data.Seat
}

// SetRiderSeat changes the seat the entity rides in and shows it to viewers.
func (e *Ent) SetRiderSeat(s world.RiderSeat) {
	if e.data.Seat == s {
		return
	}
	e.data.Seat = s
	e.updateState()
}

// Extinguish ...
func (e *Ent) Extinguish() {
	e.SetOnFire(0)
}

// NameTag returns the name tag of the entity. An empty string is returned if
// no name tag was set.
func (e *Ent) NameTag() string {
	return e.data.Name
}

// SetNameTag changes the name tag of an entity. The name tag is removed if an
// empty string is passed.
func (e *Ent) SetNameTag(s string) {
	e.data.Name = s
	e.updateState()
}

// AlwaysShowNameTag returns whether the name tag of the entity is shown at all
// distances instead of only when the entity is looked at from up close.
func (e *Ent) AlwaysShowNameTag() bool {
	return e.data.AlwaysShowNameTag
}

// SetAlwaysShowNameTag changes whether the name tag of the entity is shown at
// all distances instead of only when the entity is looked at from up close.
func (e *Ent) SetAlwaysShowNameTag(alwaysShow bool) {
	e.data.AlwaysShowNameTag = alwaysShow
	e.updateState()
}

// updateState updates the state of the entity for all viewers of the entity.
// Viewers get the entity of its species, such as a *Vindicator, since its type
// decides the metadata they show.
func (e *Ent) updateState() {
	var self world.Entity = e
	if ent, ok := e.handle.WorldEntity(e.tx); ok {
		self = ent
	}
	viewers := e.tx.Viewers(e.data.Pos)
	for _, v := range viewers {
		v.ViewEntityState(self)
	}
	e.tx.ReleaseViewers(viewers)
}

// AcceptItem returns whether the entity accepts the item stack passed. This may be called when a player tries to
// interact with the entity.
func (e *Ent) AcceptItem(from world.Entity, tx *world.Tx, ctx *item.UseContext) bool {
	if tx != nil {
		e.bindTx(tx)
	}
	if acceptor, ok := e.Behaviour().(interface {
		AcceptItem(e *Ent, from world.Entity, tx *world.Tx, ctx *item.UseContext) bool
	}); ok {
		return acceptor.AcceptItem(e, from, tx, ctx)
	}
	return false
}

// Attack is called when the entity is attacked by an attacker.
func (e *Ent) Attack(attacker world.Entity, tx *world.Tx) {
	if tx != nil {
		e.bindTx(tx)
	}
	if attackable, ok := e.Behaviour().(interface {
		Attack(e *Ent, attacker world.Entity, tx *world.Tx)
	}); ok {
		attackable.Attack(e, attacker, tx)
	}
}

// Tick ticks Ent, progressing its lifetime and closing the entity if it is
// in the void.
func (e *Ent) Tick(tx *world.Tx, current int64) {
	e.bindTx(tx)
	if tc := e.portalTravelComputer(); tc != nil {
		tc.deferred = true
		defer func() {
			tc.deferred = false
		}()
	}

	y := e.data.Pos[1]
	if y < float64(tx.Range()[0]) && current%10 == 0 {
		_ = e.CloseIn(tx)
		return
	}
	if e.tickFire(tx); e.handle.Closed() {
		return
	}
	if e.data.Riding != nil {
		e.data.Vel = mgl64.Vec3{}
	}

	m := e.Behaviour().Tick(e, tx)
	if e.handle.Closed() {
		// The behaviour removed the entity, as an item merging into another
		// does: its viewers no longer know it.
		if m != nil {
			m.releaseViewers()
		}
		return
	}
	if m != nil {
		e.data.OnGround = m.onGround
		e.tickFall(tx, m)
	}
	tickLeash(e, tx, m)
	// The blocks act on the concrete mob, whose Hurt and SetOnFire may
	// differ from those of Ent, as a strider is immune to fire.
	if e.data.OnGround && !e.handle.Closed() {
		stepOnBlock(tx, e.living())
	}
	checkEntityInsiders(tx, e.living())
	if e.data.FireDuration > 0 && e.inRain(tx) {
		// Entity.move: rain puts out the fire of an entity.
		e.SetOnFire(0)
	}
	if frozen := e.data.FrozenTicks; frozen > 0 || e.data.InPowderSnow || e.data.WasInPowderSnow {
		TickFreezing(tx, e.living(), e.data)
		if e.data.FrozenTicks != frozen {
			e.updateState()
		}
	}
	if e.finishPendingPortalTravel(tx) {
		if m != nil {
			m.releaseViewers()
		}
		return
	}
	if m != nil {
		if e.handle.Closed() {
			// Fire, lava or a cactus destroyed the entity while it moved.
			m.releaseViewers()
			return
		}
		m.Send()
	}
	e.stopPortalContact()
	e.data.Age += time.Second / 20
}

// OnGround reports whether the entity's last movement ended on a surface.
func (e *Ent) OnGround() bool {
	return e.data.OnGround
}

// TravelThroughPortal handles the entity touching a portal block.
func (e *Ent) TravelThroughPortal(tx *world.Tx, target world.Dimension) {
	if tc := e.portalTravelComputer(); tc != nil {
		if tc.deferred {
			tc.queuePortalTravel(tx, target)
			return
		}
		tc.EnterPortal(e, tx, target)
	}
}

func (e *Ent) portalTravelComputer() *PortalTravelComputer {
	if b, ok := e.Behaviour().(portalTravelComputerProvider); ok {
		return b.PortalTravelComputer()
	}
	return nil
}

func (e *Ent) stopPortalContact() {
	if tc := e.portalTravelComputer(); tc != nil {
		tc.StopPortalContact()
	}
}

func (e *Ent) finishPendingPortalTravel(tx *world.Tx) bool {
	if tc := e.portalTravelComputer(); tc != nil {
		return tc.finishPendingPortalTravel(e, tx)
	}
	return false
}

// Close closes the Ent and removes the associated entity from the world.
func (e *Ent) Close() error {
	e.once.Do(func() {
		if e.tx != nil {
			if d, ok := e.Behaviour().(dismounter); ok {
				d.DismountAll(e, e.tx)
			}
		}
		e.tx.RemoveEntity(e)
		_ = e.handle.Close()
	})
	return nil
}

func (e *Ent) bindTx(tx *world.Tx) {
	e.tx = tx
}

// BindTransaction binds the Ent to tx for the duration of an owner callback.
func (e *Ent) BindTransaction(tx *world.Tx) {
	e.bindTx(tx)
}

// CloseIn closes the Ent using the provided transaction and removes the associated entity from the world.
// This should be used from within world transactions (e.g., during ticks) to ensure a valid, active Tx is used.
func (e *Ent) CloseIn(tx *world.Tx) error {
	e.once.Do(func() {
		if d, ok := e.Behaviour().(dismounter); ok {
			d.DismountAll(e, tx)
		}
		tx.RemoveEntity(e)
		_ = e.handle.Close()
	})
	return nil
}

// SetPortalCooldown ports Entity.setPortalCooldown for an entity that may
// travel through portals.
func (e *Ent) SetPortalCooldown() {
	if tc := e.portalTravelComputer(); tc != nil {
		tc.SetPortalCooldown()
	}
}
