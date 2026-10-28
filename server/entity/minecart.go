package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	minecartDefaultMaxSpeed = 0.4
	minecartDisplayOffset   = 6
	minecartSeatOffset      = 0.75
	minecartNetworkOffset   = float64(float32(0.35))
)

var minecartMatrix = [10][2][3]int{
	{{0, 0, -1}, {0, 0, 1}},
	{{-1, 0, 0}, {1, 0, 0}},
	{{-1, -1, 0}, {1, 0, 0}},
	{{-1, 0, 0}, {1, -1, 0}},
	{{0, 0, -1}, {0, -1, 1}},
	{{0, -1, -1}, {0, 0, 1}},
	{{0, 0, 1}, {1, 0, 0}},
	{{0, 0, 1}, {-1, 0, 0}},
	{{0, 0, -1}, {-1, 0, 0}},
	{{0, 0, -1}, {1, 0, 0}},
}

// MinecartBehaviourConfig configures a minecart behaviour.
type MinecartBehaviourConfig struct {
	DisplayBlock        world.Block
	DisplayOffset       int
	SlowWhenEmpty       bool
	Rideable            bool
	MaxSpeed            float64
	DerailedVelocityMod mgl64.Vec3
	FlyingVelocityMod   mgl64.Vec3
}

// Apply applies the configuration to the entity data.
func (conf MinecartBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates a new minecart behaviour.
func (conf MinecartBehaviourConfig) New() *MinecartBehaviour {
	if conf.DisplayOffset == 0 {
		conf.DisplayOffset = minecartDisplayOffset
	}
	if conf.MaxSpeed == 0 {
		conf.MaxSpeed = minecartDefaultMaxSpeed
	}
	if conf.DerailedVelocityMod == (mgl64.Vec3{}) {
		conf.DerailedVelocityMod = mgl64.Vec3{0.5, 0.5, 0.5}
	}
	if conf.FlyingVelocityMod == (mgl64.Vec3{}) {
		conf.FlyingVelocityMod = mgl64.Vec3{0.95, 0.95, 0.95}
	}
	if !conf.SlowWhenEmpty {
		conf.SlowWhenEmpty = true
	}
	b := &MinecartBehaviour{
		displayBlock:        conf.DisplayBlock,
		displayOffset:       conf.DisplayOffset,
		slowWhenEmpty:       conf.SlowWhenEmpty,
		rideable:            conf.Rideable,
		maxSpeed:            conf.MaxSpeed,
		derailedVelocityMod: conf.DerailedVelocityMod,
		flyingVelocityMod:   conf.FlyingVelocityMod,
		// Carts clip new contacts but do not recover an existing block
		// overlap by pushing themselves out (including slope exits).
		mc:               &MovementComputer{nativeCollisions: true, depenetrationBits: 1},
		rollingDirection: 1,
	}
	if conf.DisplayBlock != nil {
		if tile, ok := displayTileFromBlock(conf.DisplayBlock); ok {
			b.displayTile = tile
		} else {
			b.displayTile = int32(world.BlockRuntimeID(conf.DisplayBlock))
		}
	}
	return b
}

// MinecartBehaviour implements the behaviour of minecarts.
type MinecartBehaviour struct {
	mc *MovementComputer

	displayBlock  world.Block
	displayOffset int
	displayTile   int32
	customDisplay bool

	slowWhenEmpty       bool
	rideable            bool
	maxSpeed            float64
	derailedVelocityMod mgl64.Vec3
	flyingVelocityMod   mgl64.Vec3

	currentSpeed     float64
	passenger        *world.EntityHandle
	activatorPending bool
	activatorPowered bool

	rollingAmplitude int
	rollingDirection int
	damage           float64
	inReverse        bool
}

// Base returns the base minecart behaviour.
func (b *MinecartBehaviour) Base() *MinecartBehaviour {
	return b
}

// Tick ticks the minecart behaviour.
func (b *MinecartBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	prevPos, prevVel, prevRot := e.data.Pos, e.data.Vel, e.data.Rot
	if b.activatorPending {
		b.activatorPending = false
		if activator, ok := e.Behaviour().(interface{ Activate(*Ent, *world.Tx, bool) }); ok {
			activator.Activate(e, tx, b.activatorPowered)
		}
	}
	if b.rollingAmplitude > 0 {
		b.rollingAmplitude--
	}

	pos, vel := minecartCentre(e.data.Pos), minecartVec32(e.data.Vel)
	vel[1] = float64(float32(vel[1]) - float32(0.04))

	x, y, z := int(math.Floor(pos[0])), int(math.Floor(pos[1])), int(math.Floor(pos[2]))
	if isRailAt(tx, cube.Pos{x, y - 1, z}) {
		y--
	}
	railPos := cube.Pos{x, y, z}
	rail := tx.Block(railPos)

	if block.IsRail(rail) {
		b.applyNaturalSlowdown(e, &vel)
		pos, vel = b.moveOnRails(e, tx, railPos, rail, pos, vel)
	} else {
		pos, vel = b.moveDerailed(e, tx, pos, vel)
	}

	e.data.Pos, e.data.Vel = minecartFeet(pos), vel
	b.updateRotation(e, prevPos)
	b.updatePassenger(e, tx)
	b.checkPassengerAlive(e, tx)
	b.handleCollisions(e, tx)

	return b.newMovement(e, tx, prevPos, prevVel, prevRot)
}

// Explode adds velocity to the minecart when hit by an explosion.
func (b *MinecartBehaviour) Explode(e *Ent, src world.ExplosionSource, impact float64) {
	e.data.Vel = e.data.Vel.Add(ExplosionImpulse(e, src, impact))
}

// Activate triggers activator rail behaviour for the base minecart.
func (b *MinecartBehaviour) Activate(e *Ent, tx *world.Tx, powered bool) {
	if !powered || !b.rideable || b.passenger == nil {
		return
	}
	b.DismountAll(e, tx)
}

// InteractText returns the interaction prompt for the minecart.
func (b *MinecartBehaviour) InteractText() string {
	if b.rideable {
		if b.passenger == nil {
			return "action.interact.ride.minecart"
		}
		return ""
	}
	return ""
}

// CanRideTarget reports if the minecart may be ridden.
func (b *MinecartBehaviour) CanRideTarget() bool {
	return b.rideable
}

// RiderSeat returns the seat of Bedrock's minecart: one rider, centred.
func (b *MinecartBehaviour) RiderSeat(rider world.Entity) world.RiderSeat {
	return seat{pos: mgl64.Vec3{0, -0.2, 0}}.riderSeat(rider)
}

// SetVehicleInput updates the input used by the minecart.
func (b *MinecartBehaviour) SetVehicleInput(_, forward float64) {
	b.currentSpeed = forward
}

// Passenger returns the current passenger handle.
func (b *MinecartBehaviour) Passenger() *world.EntityHandle {
	return b.passenger
}

// DisplayBlock returns the display block, if any.
func (b *MinecartBehaviour) DisplayBlock() (world.Block, bool) {
	if b.displayBlock != nil {
		return b.displayBlock, true
	}
	if b.displayTile != 0 {
		if bl, ok := blockFromDisplayTile(b.displayTile); ok {
			return bl, true
		}
	}
	return nil, false
}

// DisplayOffset returns the display offset.
func (b *MinecartBehaviour) DisplayOffset() int {
	return b.displayOffset
}

// CustomDisplay reports if the minecart uses a custom display tile.
func (b *MinecartBehaviour) CustomDisplay() bool {
	return b.customDisplay || b.displayBlock != nil || b.displayTile != 0
}

// RollingAmplitude returns the rolling amplitude for hurt animation.
func (b *MinecartBehaviour) RollingAmplitude() int {
	return b.rollingAmplitude
}

// RollingDirection returns the rolling direction for hurt animation.
func (b *MinecartBehaviour) RollingDirection() int {
	return b.rollingDirection
}

// Damage returns the stored minecart damage value.
func (b *MinecartBehaviour) Damage() float64 {
	return b.damage
}

// Hurt applies damage to the minecart and returns true if it should be destroyed.
func (b *MinecartBehaviour) Hurt(amount float64) bool {
	b.rollingDirection *= -1
	b.rollingAmplitude = 9
	b.damage += amount * 10
	return b.damage > 40
}

// ResetDamage resets the damage to 0.
func (b *MinecartBehaviour) ResetDamage() {
	b.damage = 0
}

// SetDisplayBlock updates the display block on the minecart.
func (b *MinecartBehaviour) SetDisplayBlock(bl world.Block, custom bool) {
	b.displayBlock = bl
	if bl == nil {
		b.customDisplay = false
		b.displayTile = 0
		if custom {
			b.displayOffset = 0
		}
		return
	}
	b.customDisplay = custom
	if tile, ok := displayTileFromBlock(bl); ok {
		b.displayTile = tile
	} else {
		b.displayTile = int32(world.BlockRuntimeID(bl))
	}
	if custom {
		b.displayOffset = minecartDisplayOffset
	}
}

// SetDisplayOffset updates the display offset.
func (b *MinecartBehaviour) SetDisplayOffset(offset int) {
	b.displayOffset = offset
}

// Mount mounts a passenger on the minecart.
func (b *MinecartBehaviour) Mount(e *Ent, tx *world.Tx, passenger world.Entity) bool {
	if passenger == nil || b.passenger != nil {
		return false
	}
	if r, ok := passenger.(Rider); ok && r.Riding() != nil {
		return false
	}
	b.passenger = passenger.H()
	if r, ok := passenger.(Rider); ok {
		seatRider(r, e)
	}
	b.broadcastLink(e, tx, passenger.H(), true)
	b.broadcastState(e, tx)
	b.broadcastPassengerState(tx, passenger.H())
	return true
}

// Dismount dismounts the current passenger.
func (b *MinecartBehaviour) Dismount(e *Ent, tx *world.Tx) {
	if b.passenger == nil {
		return
	}
	passenger := b.passenger
	b.passenger = nil
	if ent, ok := passenger.Entity(tx); ok {
		if r, ok := ent.(Rider); ok {
			r.SetRiding(nil)
		}
	}
	b.broadcastLink(e, tx, passenger, false)
	b.broadcastState(e, tx)
	b.broadcastPassengerState(tx, passenger)
}

// DismountAll dismounts all passengers from the minecart.
func (b *MinecartBehaviour) DismountAll(e *Ent, tx *world.Tx) {
	b.Dismount(e, tx)
}

func (b *MinecartBehaviour) updateRotation(e *Ent, prevPos mgl64.Vec3) {
	diffX := prevPos[0] - e.data.Pos[0]
	diffZ := prevPos[2] - e.data.Pos[2]
	yaw := e.data.Rot.Yaw()
	if diffX*diffX+diffZ*diffZ > 0.001 {
		yaw = math.Atan2(diffZ, diffX) * 180 / math.Pi
	}
	if yaw < 0 {
		yaw -= yaw - yaw
	}
	e.data.Rot = cube.Rotation{yaw, 0}
}

func (b *MinecartBehaviour) updatePassenger(e *Ent, tx *world.Tx) {
	if b.passenger == nil {
		return
	}
	passenger, ok := b.passenger.Entity(tx)
	if !ok {
		b.passenger = nil
		return
	}
	target := e.data.Pos.Add(mgl64.Vec3{0, minecartSeatOffset, 0})
	if mover, ok := passenger.(interface {
		Move(deltaPos mgl64.Vec3, deltaYaw, deltaPitch float64)
		Position() mgl64.Vec3
	}); ok {
		mover.Move(target.Sub(mover.Position()), 0, 0)
	}
}

func (b *MinecartBehaviour) checkPassengerAlive(e *Ent, tx *world.Tx) {
	if b.passenger == nil {
		return
	}
	passenger, ok := b.passenger.Entity(tx)
	if !ok {
		b.passenger = nil
		return
	}
	if l, ok := passenger.(Living); ok && l.Dead() {
		b.Dismount(e, tx)
	}
}

func (b *MinecartBehaviour) handleCollisions(e *Ent, tx *world.Tx) {
	box := e.H().Type().BBox(e).Translate(e.data.Pos)
	search := box.Grow(1.0)
	collision := box.Grow(0.1)
	for other := range tx.EntitiesWithin(search) {
		if other.H() == e.H() {
			continue
		}
		if b.passenger != nil && other.H() == b.passenger {
			continue
		}
		if r, ok := other.(Rider); ok && r.Riding() == e.H() {
			continue
		}
		otherBox := other.H().Type().BBox(other).Translate(other.Position())
		if !collision.IntersectsWith(otherBox) {
			continue
		}
		b.applyEntityCollision(e, other)
	}
}

func (b *MinecartBehaviour) applyEntityCollision(e *Ent, other world.Entity) {
	if gm, ok := other.(interface{ GameMode() world.GameMode }); ok && !gm.GameMode().HasCollision() {
		return
	}
	if isMinecartEntity(other) {
		otherCart, ok := other.(interface {
			Velocity() mgl64.Vec3
			SetVelocity(v mgl64.Vec3)
		})
		if ok {
			b.applyMinecartCollision(e, other, otherCart)
			return
		}
	}
	motiveX := other.Position()[0] - e.data.Pos[0]
	motiveZ := other.Position()[2] - e.data.Pos[2]
	square := motiveX*motiveX + motiveZ*motiveZ
	if square < 1.0e-4 {
		return
	}
	square = math.Sqrt(square)
	motiveX /= square
	motiveZ /= square
	next := 1.0 / square
	if next > 1.0 {
		next = 1.0
	}
	motiveX *= next
	motiveZ *= next
	motiveX *= 0.1
	motiveZ *= 0.1
	motiveX *= 0.5
	motiveZ *= 0.5

	e.data.Vel[0] -= motiveX
	e.data.Vel[2] -= motiveZ
}

func (b *MinecartBehaviour) applyMinecartCollision(e *Ent, other world.Entity, otherCart interface {
	Velocity() mgl64.Vec3
	SetVelocity(v mgl64.Vec3)
}) {
	motiveX := other.Position()[0] - e.data.Pos[0]
	motiveZ := other.Position()[2] - e.data.Pos[2]
	square := motiveX*motiveX + motiveZ*motiveZ
	if square < 1.0e-4 {
		return
	}
	square = math.Sqrt(square)
	motiveX /= square
	motiveZ /= square
	next := 1.0 / square
	if next > 1.0 {
		next = 1.0
	}
	motiveX *= next
	motiveZ *= next
	motiveX *= 0.1
	motiveZ *= 0.1
	motiveX *= 0.5
	motiveZ *= 0.5

	vector := mgl64.Vec3{motiveX, 0, motiveZ}.Normalize()
	yaw := e.data.Rot.Yaw() * math.Pi / 180
	vec := mgl64.Vec3{math.Cos(yaw), 0, math.Sin(yaw)}.Normalize()
	densityXZ := math.Abs(vector.Dot(vec))
	if densityXZ < 0.8 {
		return
	}
	otherVel := otherCart.Velocity()
	motX := otherVel[0] + e.data.Vel[0]
	motZ := otherVel[2] + e.data.Vel[2]

	otherType := minecartEntityTypeID(other.H().Type().EncodeEntity())
	selfType := minecartEntityTypeID(e.H().Type().EncodeEntity())
	if otherType == 2 && selfType != 2 {
		e.data.Vel[0] *= 0.2
		e.data.Vel[2] *= 0.2
		e.data.Vel[0] += otherVel[0] - motiveX
		e.data.Vel[2] += otherVel[2] - motiveZ

		otherVel[0] *= 0.95
		otherVel[2] *= 0.95
		otherCart.SetVelocity(otherVel)
		return
	}
	if otherType != 2 && selfType == 2 {
		otherVel[0] *= 0.2
		otherVel[2] *= 0.2
		otherVel[0] += e.data.Vel[0] + motiveX
		otherVel[2] += e.data.Vel[2] + motiveZ
		otherCart.SetVelocity(otherVel)

		e.data.Vel[0] *= 0.95
		e.data.Vel[2] *= 0.95
		return
	}

	motX /= 2
	motZ /= 2

	e.data.Vel[0] *= 0.2
	e.data.Vel[2] *= 0.2
	e.data.Vel[0] += motX - motiveX
	e.data.Vel[2] += motZ - motiveZ

	otherVel[0] *= 0.2
	otherVel[2] *= 0.2
	otherVel[0] += motX + motiveX
	otherVel[2] += motZ + motiveZ
	otherCart.SetVelocity(otherVel)
}

func isMinecartEntity(e world.Entity) bool {
	return minecartEntityTypeID(e.H().Type().EncodeEntity()) != -1
}

func minecartEntityTypeID(entityID string) int {
	switch entityID {
	case "minecraft:minecart":
		return 0
	case "minecraft:chest_minecart":
		return 1
	case "minecraft:furnace_minecart":
		return 2
	case "minecraft:tnt_minecart":
		return 3
	case "minecraft:hopper_minecart":
		return 5
	default:
		return -1
	}
}

func (b *MinecartBehaviour) broadcastLink(e *Ent, tx *world.Tx, passenger *world.EntityHandle, mounted bool) {
	viewers := tx.Viewers(e.data.Pos)
	for _, v := range viewers {
		if linkViewer, ok := v.(interface {
			ViewEntityLink(ridden, rider *world.EntityHandle, mounted bool)
		}); ok {
			linkViewer.ViewEntityLink(e.H(), passenger, mounted)
		}
	}
	tx.ReleaseViewers(viewers)
}

func (b *MinecartBehaviour) broadcastState(e *Ent, tx *world.Tx) {
	viewers := tx.Viewers(e.data.Pos)
	for _, v := range viewers {
		v.ViewEntityState(e)
	}
	tx.ReleaseViewers(viewers)
}

func (b *MinecartBehaviour) broadcastPassengerState(tx *world.Tx, passenger *world.EntityHandle) {
	if passenger == nil {
		return
	}
	ent, ok := passenger.Entity(tx)
	if !ok {
		return
	}
	viewers := tx.Viewers(ent.Position())
	for _, v := range viewers {
		v.ViewEntityState(ent)
	}
	tx.ReleaseViewers(viewers)
}

func (b *MinecartBehaviour) newMovement(e *Ent, tx *world.Tx, prevPos, prevVel mgl64.Vec3, prevRot cube.Rotation) *Movement {
	viewers := tx.Viewers(prevPos)
	return &Movement{
		v:          viewers,
		release:    func() { tx.ReleaseViewers(viewers) },
		e:          e,
		pos:        e.data.Pos,
		vel:        e.data.Vel,
		dpos:       e.data.Pos.Sub(prevPos),
		dvel:       e.data.Vel.Sub(prevVel),
		rot:        e.data.Rot,
		onGround:   b.mc.onGround,
		rotChanged: rotationChanged(e.data.Rot, prevRot),
	}
}

func isRailAt(tx *world.Tx, pos cube.Pos) bool {
	return block.IsRail(tx.Block(pos))
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// Minecart is the base minecart entity.
type Minecart struct {
	*Ent
}

func (m *Minecart) base() *MinecartBehaviour {
	if b, ok := m.Behaviour().(*MinecartBehaviour); ok {
		return b
	}
	if b, ok := m.Behaviour().(interface{ Base() *MinecartBehaviour }); ok {
		return b.Base()
	}
	return nil
}

// SetVehicleInput updates the input used by the minecart.
func (m *Minecart) SetVehicleInput(strafe, forward float64) {
	if b := m.base(); b != nil {
		b.SetVehicleInput(strafe, forward)
	}
}

// Interact handles interaction with the minecart.
func (m *Minecart) Interact(tx *world.Tx, user item.User, _ *item.UseContext) bool {
	return m.Mount(tx, user)
}

// Mount seats passenger in a rideable minecart without a display block.
func (m *Minecart) Mount(tx *world.Tx, passenger world.Entity) bool {
	if b := m.base(); b != nil && b.rideable {
		if _, hasDisplay := b.DisplayBlock(); hasDisplay {
			return false
		}
		return b.Mount(m.Ent, tx, passenger)
	}
	return false
}

// Dismount removes the passenger from the minecart if it matches.
func (m *Minecart) Dismount(tx *world.Tx, passenger world.Entity) {
	b := m.base()
	if b == nil || passenger == nil || b.passenger == nil {
		return
	}
	if passenger.H() != b.passenger {
		return
	}
	b.Dismount(m.Ent, tx)
}

// Destroy destroys the minecart, dropping the item if appropriate.
func (m *Minecart) Destroy(tx *world.Tx, src world.DamageSource, causer world.Entity) bool {
	b := m.base()
	if b == nil {
		_ = m.CloseIn(tx)
		return true
	}
	damage := minecartDamageFromSource(src, causer)
	if damage >= 40 || b.Hurt(damage) {
		b.DismountAll(m.Ent, tx)
		if canDropMinecart(causer, tx.World()) {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, item.NewStack(item.Minecart{}, 1)))
		}
		_ = m.CloseIn(tx)
		return true
	}
	b.broadcastState(m.Ent, tx)
	return true
}

// Links returns the active links for the minecart.
func (m *Minecart) Links() []Link {
	b := m.base()
	if b == nil || b.passenger == nil {
		return nil
	}
	return []Link{{
		Ridden:         m.H(),
		Rider:          b.passenger,
		Type:           LinkRider,
		RiderInitiated: true,
	}}
}

func minecartDamageFromSource(src world.DamageSource, causer world.Entity) float64 {
	switch src.(type) {
	case AttackDamageSource:
		if attacker, ok := causer.(interface {
			HeldItems() (item.Stack, item.Stack)
		}); ok {
			main, _ := attacker.HeldItems()
			return main.AttackDamage()
		}
		return 1
	case ExplosionDamageSource:
		return 40
	case ProjectileDamageSource:
		return 40
	default:
		return 1
	}
}

func canDropMinecart(causer world.Entity, _ *world.World) bool {
	if causer == nil {
		return true
	}
	if gm, ok := causer.(interface {
		GameMode() world.GameMode
	}); ok {
		if gm.GameMode().CreativeInventory() {
			return false
		}
	}
	return true
}

// minecartType is a world.EntityType implementation for minecarts.
var MinecartType minecartType

type minecartType struct{}

func (minecartType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Minecart{Ent: &Ent{tx: tx, handle: handle, data: data}}
}

func (minecartType) EncodeEntity() string { return "minecraft:minecart" }

func (minecartType) BBox(e world.Entity) cube.BBox { return minecartBBox(e) }

// NetworkOffset converts the public base position to the native actor centre.
func (minecartType) NetworkOffset() float64 { return minecartNetworkOffset }

func (minecartType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := minecartConf
	beh := conf.New()
	readMinecartDisplayNBT(beh, m)
	data.Data = beh
}

func (minecartType) EncodeNBT(data *world.EntityData) map[string]any {
	b := minecartBaseBehaviour(data.Data)
	m := map[string]any{}
	writeMinecartDisplayNBT(b, m)
	return m
}

var minecartConf = MinecartBehaviourConfig{
	Rideable:      true,
	SlowWhenEmpty: true,
}

func minecartBaseBehaviour(data any) *MinecartBehaviour {
	switch b := data.(type) {
	case *MinecartBehaviour:
		return b
	case interface{ Base() *MinecartBehaviour }:
		return b.Base()
	default:
		return nil
	}
}

func readMinecartDisplayNBT(b *MinecartBehaviour, m map[string]any) {
	if b == nil {
		return
	}
	if _, ok := m["CustomDisplayTile"]; ok {
		if readNBTBool(m["CustomDisplayTile"]) {
			b.customDisplay = true
			b.displayTile = int32(readNBTInt(m["DisplayTile"]))
			b.displayOffset = int(readNBTInt(m["DisplayOffset"]))
			if bl, ok := blockFromDisplayTile(b.displayTile); ok {
				b.displayBlock = bl
			}
		}
		return
	}
	if b.displayBlock != nil {
		if b.displayTile == 0 {
			if tile, ok := displayTileFromBlock(b.displayBlock); ok {
				b.displayTile = tile
			} else {
				b.displayTile = int32(world.BlockRuntimeID(b.displayBlock))
			}
		}
		if b.displayOffset == 0 {
			b.displayOffset = minecartDisplayOffset
		}
	}
}

func writeMinecartDisplayNBT(b *MinecartBehaviour, m map[string]any) {
	if b == nil {
		return
	}
	hasDisplay := b.customDisplay || b.displayBlock != nil || b.displayTile != 0
	m["CustomDisplayTile"] = boolByte(hasDisplay)
	if !hasDisplay {
		return
	}
	displayTile := b.displayTile
	if b.displayBlock != nil {
		if tile, ok := displayTileFromBlock(b.displayBlock); ok {
			displayTile = tile
		} else if displayTile == 0 {
			displayTile = int32(world.BlockRuntimeID(b.displayBlock))
		}
	}
	offset := b.displayOffset
	if offset == 0 && b.displayBlock != nil {
		offset = minecartDisplayOffset
	}
	m["DisplayTile"] = displayTile
	m["DisplayOffset"] = int32(offset)
}

func blockFromDisplayTile(tile int32) (world.Block, bool) {
	if bl, ok := world.BlockByRuntimeID(uint32(tile)); ok {
		return bl, true
	}

	id := int(tile & 0xffff)
	meta := int((uint32(tile) >> 16) & 0xffff)
	switch id {
	case 46:
		return block.TNT{}, true
	case 54:
		return block.Chest{Facing: legacyHorizontalMetaToDirection(meta)}, true
	case 154:
		return block.Hopper{Facing: legacyHopperMetaToFace(meta)}, true
	default:
		return nil, false
	}
}

func displayTileFromBlock(bl world.Block) (int32, bool) {
	switch b := bl.(type) {
	case block.TNT:
		return 46, true
	case block.Chest:
		return 54 | (legacyHorizontalDirectionToMeta(b.Facing) << 16), true
	case block.Hopper:
		return 154 | (legacyHopperFaceToMeta(b.Facing) << 16), true
	default:
		return 0, false
	}
}

func legacyHorizontalDirectionToMeta(dir cube.Direction) int32 {
	switch dir {
	case cube.North:
		return 2
	case cube.South:
		return 3
	case cube.West:
		return 4
	case cube.East:
		return 5
	default:
		return 2
	}
}

func legacyHorizontalMetaToDirection(meta int) cube.Direction {
	switch meta {
	case 3:
		return cube.South
	case 4:
		return cube.West
	case 5:
		return cube.East
	default:
		return cube.North
	}
}

func legacyHopperFaceToMeta(face cube.Face) int32 {
	switch face {
	case cube.FaceNorth:
		return 2
	case cube.FaceSouth:
		return 3
	case cube.FaceWest:
		return 4
	case cube.FaceEast:
		return 5
	default:
		return 0
	}
}

func legacyHopperMetaToFace(meta int) cube.Face {
	switch meta {
	case 2:
		return cube.FaceNorth
	case 3:
		return cube.FaceSouth
	case 4:
		return cube.FaceWest
	case 5:
		return cube.FaceEast
	default:
		return cube.FaceDown
	}
}

func readNBTInt(v any) int32 {
	switch n := v.(type) {
	case int8:
		return int32(n)
	case uint8:
		return int32(n)
	case int16:
		return int32(n)
	case uint16:
		return int32(n)
	case int32:
		return n
	case uint32:
		return int32(n)
	case int64:
		return int32(n)
	case uint64:
		return int32(n)
	default:
		return 0
	}
}

func readNBTBool(v any) bool {
	switch n := v.(type) {
	case uint8:
		return n != 0
	case int8:
		return n != 0
	case int16:
		return n != 0
	case int32:
		return n != 0
	case int64:
		return n != 0
	case bool:
		return n
	default:
		return false
	}
}
