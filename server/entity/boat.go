package entity

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	boatSeatOffset         = 0.6
	boatRiderOffsetXWhen2  = 0.2
	boatPassengerOffsetX   = -0.6
	boatSinkingDepth       = 0.07
	boatSinkingSpeed       = 0.0005
	boatSinkingMaxSpeed    = 0.005
	boatGravity            = 0.04
	boatWaterDrag          = 0.9
	boatNetworkOffset      = 0.375
	boatContainerTypeChest = 35
)

const boatBuoyancyData = "{\"apply_gravity\":true,\"base_buoyancy\":1.0,\"big_wave_probability\":0.02999999932944775,\"big_wave_speed\":10.0,\"drag_down_on_buoyancy_removed\":0.0,\"liquid_blocks\":[\"minecraft:water\",\"minecraft:flowing_water\"],\"simulate_waves\":true}"

// NewBoat creates a new boat entity with the variant passed.
func NewBoat(opts world.EntitySpawnOpts, variant int) *world.EntityHandle {
	conf := boatConf
	conf.Variant = variant
	return opts.New(BoatType, conf)
}

// NewChestBoat creates a new chest boat entity with the variant passed.
func NewChestBoat(opts world.EntitySpawnOpts, variant int) *world.EntityHandle {
	conf := chestBoatConf
	conf.Boat.Variant = variant
	return opts.New(ChestBoatType, conf)
}

// BoatBehaviourConfig configures boat behaviour.
type BoatBehaviourConfig struct {
	Variant       int
	MaxPassengers int
}

// Apply applies the configuration to the entity data.
func (conf BoatBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates a new boat behaviour.
func (conf BoatBehaviourConfig) New() *BoatBehaviour {
	if conf.MaxPassengers <= 0 {
		conf.MaxPassengers = 2
	}
	if conf.MaxPassengers > 2 {
		conf.MaxPassengers = 2
	}
	return &BoatBehaviour{
		mc:               &MovementComputer{},
		variant:          int32(conf.Variant),
		maxPassengers:    conf.MaxPassengers,
		sinking:          true,
		rollingDirection: 1,
	}
}

// BoatBehaviour implements the behaviour for boats.
type BoatBehaviour struct {
	mc *MovementComputer

	variant       int32
	maxPassengers int

	sinking bool
	inWater bool

	inputStrafe   float64
	inputForward  float64
	deltaRotation float32
	rowTimeLeft   float32
	rowTimeRight  float32

	passengers [2]*world.EntityHandle

	rollingAmplitude int
	rollingDirection int
	damage           float64
}

// Base returns the base boat behaviour.
func (b *BoatBehaviour) Base() *BoatBehaviour {
	return b
}

// Tick ticks the boat behaviour.
func (b *BoatBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	prevPos, prevVel, prevRot := e.data.Pos, e.data.Vel, e.data.Rot
	prevRowLeft, prevRowRight := b.rowTimeLeft, b.rowTimeRight
	if b.passengers[0] == nil {
		b.inputStrafe, b.inputForward = 0, 0
	}
	if b.rollingAmplitude > 0 {
		b.rollingAmplitude--
	}

	pos, vel := e.data.Pos, e.data.Vel
	waterDiff := b.waterLevelDiff(e, tx)
	friction := b.groundFriction(tx, e)

	b.applyInput(e, &vel, friction)
	vel[0] *= friction
	vel[2] *= friction
	b.applyVerticalMotion(&vel, waterDiff)

	dPos, newVel := b.mc.checkCollision(tx, e, pos, vel)
	e.data.Pos = pos.Add(dPos)
	e.data.Vel = newVel
	if b.passengers[0] == nil {
		b.updateRotation(e)
	}
	b.updatePassengers(e, tx)
	b.checkPassengersAlive(e, tx)
	if math.Abs(float64(b.rowTimeLeft-prevRowLeft)) > 1e-4 || math.Abs(float64(b.rowTimeRight-prevRowRight)) > 1e-4 {
		b.broadcastState(e, tx)
	}

	return b.newMovement(e, tx, prevPos, prevVel, prevRot)
}

// InteractText returns the interaction prompt for the boat.
func (b *BoatBehaviour) InteractText() string {
	if b.full() {
		return ""
	}
	return "action.interact.ride.boat"
}

// CanRideTarget reports if the boat can be ridden.
func (b *BoatBehaviour) CanRideTarget() bool {
	return true
}

// SeatOffset returns the seat offset metadata for the boat.
func (b *BoatBehaviour) SeatOffset() mgl32.Vec3 {
	return mgl32.Vec3{0, float32(boatSeatOffset), 0}
}

// Variant returns the boat variant metadata value.
func (b *BoatBehaviour) Variant() int32 {
	return b.variant
}

// Buoyant reports if the boat is buoyant.
func (b *BoatBehaviour) Buoyant() bool {
	return true
}

// BuoyancyData returns the buoyancy metadata JSON.
func (b *BoatBehaviour) BuoyancyData() string {
	return boatBuoyancyData
}

// ControllingSeatIndex returns the controlling seat index metadata.
func (b *BoatBehaviour) ControllingSeatIndex() int32 {
	return 0
}

// RowTimeLeft returns the left paddle animation timer metadata.
func (b *BoatBehaviour) RowTimeLeft() float32 {
	return b.rowTimeLeft
}

// RowTimeRight returns the right paddle animation timer metadata.
func (b *BoatBehaviour) RowTimeRight() float32 {
	return b.rowTimeRight
}

// SetVehicleInput updates the input used to control the boat.
func (b *BoatBehaviour) SetVehicleInput(strafe, forward float64) {
	b.inputStrafe = strafe
	b.inputForward = forward
}

// Passenger returns the controlling passenger handle.
func (b *BoatBehaviour) Passenger() *world.EntityHandle {
	return b.passengers[0]
}

// Mount mounts a passenger on the boat.
func (b *BoatBehaviour) Mount(e *Ent, tx *world.Tx, passenger world.Entity) bool {
	if passenger == nil || b.full() {
		return false
	}
	if b.waterLevelDiff(e, tx) < -boatSinkingDepth {
		return false
	}
	if r, ok := passenger.(Rider); ok && r.Riding() != nil {
		return false
	}
	seat := b.firstEmptySeat()
	if seat < 0 {
		return false
	}
	b.passengers[seat] = passenger.H()
	if r, ok := passenger.(Rider); ok {
		r.SetRiding(e.H())
	}
	b.broadcastLink(e, tx, passenger.H(), b.linkTypeForSeat(seat), true)
	b.broadcastState(e, tx)
	b.broadcastPassengerState(tx, passenger.H())
	return true
}

// Dismount removes a passenger from the boat.
func (b *BoatBehaviour) Dismount(e *Ent, tx *world.Tx, passenger world.Entity) {
	if passenger == nil {
		return
	}
	b.dismountHandle(e, tx, passenger.H())
}

// DismountAll dismounts all passengers from the boat.
func (b *BoatBehaviour) DismountAll(e *Ent, tx *world.Tx) {
	for i := 0; i < b.maxPassengers; i++ {
		if h := b.passengers[i]; h != nil {
			b.dismountHandle(e, tx, h)
		}
	}
}

// RollingAmplitude returns the rolling amplitude metadata.
func (b *BoatBehaviour) RollingAmplitude() int {
	return b.rollingAmplitude
}

// RollingDirection returns the rolling direction metadata.
func (b *BoatBehaviour) RollingDirection() int {
	return b.rollingDirection
}

// Damage returns the accumulated boat damage metadata.
func (b *BoatBehaviour) Damage() float64 {
	return b.damage
}

// Hurt applies damage and returns true if the boat should break.
func (b *BoatBehaviour) Hurt(amount float64) bool {
	if b.rollingDirection == 0 {
		b.rollingDirection = 1
	}
	b.rollingDirection *= -1
	b.rollingAmplitude = 9
	b.damage += amount * 10
	return b.damage > 40
}

func (b *BoatBehaviour) applyInput(e *Ent, vel *mgl64.Vec3, friction float64) {
	inputLeft := b.inputStrafe > 0.35
	inputRight := b.inputStrafe < -0.35
	inputUp := b.inputForward > 0.35
	inputDown := b.inputForward < -0.35

	delta := float32(1)
	if inputDown {
		delta = 0.1
	}
	if inputLeft {
		b.deltaRotation -= delta
	} else if inputRight {
		b.deltaRotation += delta
	}
	b.deltaRotation *= float32(friction)
	b.deltaRotation = float32(clamp(float64(b.deltaRotation), -5, 5))
	e.data.Rot = cube.Rotation{e.data.Rot.Yaw() + float64(b.deltaRotation), 0}

	speed := math.Sqrt(vel[0]*vel[0] + vel[2]*vel[2])
	animationSpeed := float32(math.Max(0.01, math.Min(0.08, speed*0.05)))
	if inputUp {
		b.rowTimeLeft += animationSpeed
		b.rowTimeRight += animationSpeed
	} else if inputDown {
		b.rowTimeLeft -= animationSpeed
		b.rowTimeRight -= animationSpeed
	} else {
		if !inputLeft {
			b.rowTimeLeft = 0
		}
		if !inputRight {
			b.rowTimeRight = 0
		}
	}

	acceleration := 0.0
	if inputRight != inputLeft && !inputUp && !inputDown {
		acceleration += 0.005
	}
	if inputUp {
		acceleration += 0.04
	} else if inputDown {
		acceleration -= 0.005
	}
	rad := (e.data.Rot.Yaw() - 90) * math.Pi / 180
	vel[0] += -math.Sin(rad) * acceleration
	vel[2] += math.Cos(rad) * acceleration
}

func (b *BoatBehaviour) applyVerticalMotion(vel *mgl64.Vec3, waterDiff float64) {
	if !b.inWater {
		vel[1] -= boatGravity
		b.sinking = true
		return
	}
	if waterDiff > boatSinkingDepth && !b.sinking {
		b.sinking = true
	} else if waterDiff < -boatSinkingDepth && b.sinking {
		b.sinking = false
	}

	if waterDiff < -boatSinkingDepth {
		vel[1] = math.Min(0.05, vel[1]+0.005)
	} else if waterDiff < 0 || !b.sinking {
		if vel[1] > boatSinkingMaxSpeed {
			vel[1] = math.Max(vel[1]-0.02, boatSinkingMaxSpeed)
		} else {
			vel[1] += boatSinkingSpeed
		}
	}

	if waterDiff > boatSinkingDepth || b.sinking {
		if waterDiff > 0.5 {
			vel[1] -= boatGravity
		} else if vel[1]-boatSinkingSpeed >= -0.005 {
			vel[1] -= boatSinkingSpeed
		}
	}
}

func (b *BoatBehaviour) updateRotation(e *Ent) {
	horizontal := mgl64.Vec3{e.data.Vel[0], 0, e.data.Vel[2]}
	if horizontal.LenSqr() <= 1.0e-4 {
		return
	}
	yaw := math.Atan2(horizontal[2], horizontal[0])*180/math.Pi + 90
	e.data.Rot = cube.Rotation{yaw, 0}
}

func (b *BoatBehaviour) updatePassengers(e *Ent, tx *world.Tx) {
	count := b.passengerCount()
	for i := 0; i < b.maxPassengers; i++ {
		h := b.passengers[i]
		if h == nil {
			continue
		}
		passenger, ok := h.Entity(tx)
		if !ok {
			b.passengers[i] = nil
			continue
		}
		offset := rotateBoatOffset(boatSeatVector(i, count), e.data.Rot.Yaw())
		target := e.data.Pos.Add(offset)
		if mover, ok := passenger.(interface {
			Move(deltaPos mgl64.Vec3, deltaYaw, deltaPitch float64)
			Position() mgl64.Vec3
		}); ok {
			mover.Move(target.Sub(mover.Position()), 0, 0)
		}
	}
}

func (b *BoatBehaviour) checkPassengersAlive(e *Ent, tx *world.Tx) {
	for i := 0; i < b.maxPassengers; i++ {
		h := b.passengers[i]
		if h == nil {
			continue
		}
		passenger, ok := h.Entity(tx)
		if !ok {
			b.passengers[i] = nil
			continue
		}
		if l, ok := passenger.(Living); ok && l.Dead() {
			b.dismountHandle(e, tx, h)
		}
	}
}

func (b *BoatBehaviour) dismountHandle(e *Ent, tx *world.Tx, passenger *world.EntityHandle) {
	seat := b.seatIndex(passenger)
	if seat < 0 {
		return
	}
	oldType := b.linkTypeForSeat(seat)
	b.passengers[seat] = nil

	if ent, ok := passenger.Entity(tx); ok {
		if r, ok := ent.(Rider); ok {
			r.SetRiding(nil)
		}
	}
	b.broadcastLink(e, tx, passenger, oldType, false)

	if seat == 0 && b.maxPassengers > 1 && b.passengers[1] != nil {
		promoted := b.passengers[1]
		b.passengers[0] = promoted
		b.passengers[1] = nil
		b.broadcastLink(e, tx, promoted, LinkPassenger, false)
		b.broadcastLink(e, tx, promoted, LinkRider, true)
		b.broadcastPassengerState(tx, promoted)
	}

	b.broadcastState(e, tx)
	b.broadcastPassengerState(tx, passenger)
}

func (b *BoatBehaviour) waterLevelDiff(e *Ent, tx *world.Tx) float64 {
	box := e.H().Type().BBox(e).Translate(e.data.Pos)
	minV, maxV := box.Min(), box.Max()
	minX, minY, minZ := int(math.Floor(minV[0])), int(math.Floor(minV[1])), int(math.Floor(minV[2]))
	maxX, maxY, maxZ := int(math.Ceil(maxV[0])), int(math.Ceil(maxV[1])), int(math.Ceil(maxV[2]))

	maxEntityY := box.Min()[1] + boatNetworkOffset
	diffY := math.MaxFloat64
	found := false

	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			for z := minZ; z <= maxZ; z++ {
				liquid, ok := tx.Liquid(cube.Pos{x, y, z})
				if !ok || liquid.LiquidType() != "water" {
					continue
				}
				level := float64(y) + liquidSurfaceHeight(liquid)
				if d := maxEntityY - level; d < diffY {
					diffY = d
				}
				found = true
			}
		}
	}
	b.inWater = found
	if !found {
		return math.MaxFloat64
	}
	return diffY
}

func liquidSurfaceHeight(liquid world.Liquid) float64 {
	if liquid.LiquidFalling() {
		return 1
	}
	depth := liquid.LiquidDepth()
	if depth < 1 {
		depth = 1
	}
	if depth > 8 {
		depth = 8
	}
	return float64(depth) / 8
}

func (b *BoatBehaviour) groundFriction(tx *world.Tx, e *Ent) float64 {
	if b.inWater {
		return boatWaterDrag
	}
	box := e.H().Type().BBox(e).Translate(e.data.Pos)
	min, max := box.Min(), box.Max()
	under := cube.Box(min[0], min[1]-0.001, min[2], max[0], min[1], max[2])
	friction, count := 0.0, 0
	for x := int(math.Floor(min[0])); x < int(math.Ceil(max[0])); x++ {
		for z := int(math.Floor(min[2])); z < int(math.Ceil(max[2])); z++ {
			pos := cube.Pos{x, int(math.Floor(min[1] - 0.001)), z}
			block := tx.Block(pos)
			supported := false
			for _, collision := range block.Model().BBox(pos, tx) {
				if collision.Translate(mgl64.Vec3{float64(x), float64(pos[1]), float64(z)}).IntersectsWith(under) {
					supported = true
					break
				}
			}
			if !supported {
				continue
			}
			value := 0.6
			if f, ok := block.(interface{ Friction() float64 }); ok {
				value = f.Friction()
			}
			friction += value
			count++
		}
	}
	if count == 0 {
		return 0.6
	}
	return friction / float64(count)
}

func (b *BoatBehaviour) passengerCount() int {
	count := 0
	for i := 0; i < b.maxPassengers; i++ {
		if b.passengers[i] != nil {
			count++
		}
	}
	return count
}

func (b *BoatBehaviour) full() bool {
	return b.passengerCount() >= b.maxPassengers
}

func (b *BoatBehaviour) firstEmptySeat() int {
	for i := 0; i < b.maxPassengers; i++ {
		if b.passengers[i] == nil {
			return i
		}
	}
	return -1
}

func (b *BoatBehaviour) seatIndex(handle *world.EntityHandle) int {
	if handle == nil {
		return -1
	}
	for i := 0; i < b.maxPassengers; i++ {
		if b.passengers[i] == handle {
			return i
		}
	}
	return -1
}

func (b *BoatBehaviour) linkTypeForSeat(seat int) LinkType {
	if seat == 0 {
		return LinkRider
	}
	return LinkPassenger
}

func boatSeatVector(seat, count int) mgl64.Vec3 {
	x := 0.0
	if count > 1 {
		if seat == 0 {
			x = boatRiderOffsetXWhen2
		} else {
			x = boatPassengerOffsetX
		}
	}
	return mgl64.Vec3{x, boatSeatOffset, 0}
}

func rotateBoatOffset(offset mgl64.Vec3, yaw float64) mgl64.Vec3 {
	rad := (yaw - 90) * math.Pi / 180
	sin, cos := math.Sin(rad), math.Cos(rad)
	x := offset[0]*cos - offset[2]*sin
	z := offset[0]*sin + offset[2]*cos
	return mgl64.Vec3{x, offset[1], z}
}

func (b *BoatBehaviour) broadcastLink(e *Ent, tx *world.Tx, passenger *world.EntityHandle, linkType LinkType, mounted bool) {
	viewers := tx.Viewers(e.data.Pos)
	for _, v := range viewers {
		if linkViewer, ok := v.(interface {
			ViewEntityLinkType(ridden, rider *world.EntityHandle, linkType LinkType, mounted bool)
		}); ok {
			linkViewer.ViewEntityLinkType(e.H(), passenger, linkType, mounted)
			continue
		}
		if linkViewer, ok := v.(interface {
			ViewEntityLink(ridden, rider *world.EntityHandle, mounted bool)
		}); ok {
			linkViewer.ViewEntityLink(e.H(), passenger, mounted)
		}
	}
	tx.ReleaseViewers(viewers)
}

func (b *BoatBehaviour) broadcastState(e *Ent, tx *world.Tx) {
	viewers := tx.Viewers(e.data.Pos)
	for _, v := range viewers {
		v.ViewEntityState(e)
	}
	tx.ReleaseViewers(viewers)
}

func (b *BoatBehaviour) broadcastPassengerState(tx *world.Tx, passenger *world.EntityHandle) {
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

func (b *BoatBehaviour) newMovement(e *Ent, tx *world.Tx, prevPos, prevVel mgl64.Vec3, prevRot cube.Rotation) *Movement {
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

// Boat is the base boat entity.
type Boat struct {
	*Ent
}

func (b *Boat) base() *BoatBehaviour {
	return boatBaseBehaviour(b.Behaviour())
}

// SetVehicleInput updates the input used by the boat.
func (b *Boat) SetVehicleInput(strafe, forward float64) {
	if beh := b.base(); beh != nil {
		beh.SetVehicleInput(strafe, forward)
	}
}

// ControlsVehicle reports if the passenger controls this boat.
func (b *Boat) ControlsVehicle(passenger world.Entity) bool {
	if passenger == nil {
		return false
	}
	beh := b.base()
	if beh == nil || beh.passengers[0] == nil {
		return false
	}
	return beh.passengers[0] == passenger.H()
}

// Interact handles interaction with the boat.
func (b *Boat) Interact(tx *world.Tx, user item.User, _ *item.UseContext) bool {
	if beh := b.base(); beh != nil {
		return beh.Mount(b.Ent, tx, user)
	}
	return false
}

// Dismount dismounts a passenger from the boat.
func (b *Boat) Dismount(tx *world.Tx, passenger world.Entity) {
	if beh := b.base(); beh != nil {
		beh.Dismount(b.Ent, tx, passenger)
	}
}

// Destroy destroys the boat.
func (b *Boat) Destroy(tx *world.Tx, src world.DamageSource, causer world.Entity) bool {
	beh := b.base()
	if beh == nil {
		_ = b.CloseIn(tx)
		return true
	}
	damage := minecartDamageFromSource(src, causer)
	if damage >= 40 || beh.Hurt(damage) {
		beh.DismountAll(b.Ent, tx)
		if canDropMinecart(causer, tx.World()) {
			stack := item.NewStack(item.Boat{Variant: item.BoatVariantFromInt(int(beh.variant))}, 1)
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: b.Position()}, stack))
		}
		_ = b.CloseIn(tx)
		return true
	}
	beh.broadcastState(b.Ent, tx)
	return true
}

// Links returns the active links for the boat.
func (b *Boat) Links() []Link {
	beh := b.base()
	if beh == nil {
		return nil
	}
	links := make([]Link, 0, beh.maxPassengers)
	if beh.passengers[0] != nil {
		links = append(links, Link{Ridden: b.H(), Rider: beh.passengers[0], Type: LinkRider, RiderInitiated: true})
	}
	if beh.maxPassengers > 1 && beh.passengers[1] != nil {
		links = append(links, Link{Ridden: b.H(), Rider: beh.passengers[1], Type: LinkPassenger, RiderInitiated: true})
	}
	return links
}

func boatBaseBehaviour(data any) *BoatBehaviour {
	switch b := data.(type) {
	case *BoatBehaviour:
		return b
	case interface{ Base() *BoatBehaviour }:
		return b.Base()
	default:
		return nil
	}
}

// BoatType is a world.EntityType implementation for boats.
var BoatType boatType

type boatType struct{}

func (boatType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Boat{Ent: &Ent{tx: tx, handle: handle, data: data}}
}

func (boatType) EncodeEntity() string   { return "minecraft:boat" }
func (boatType) NetworkOffset() float64 { return boatNetworkOffset }
func (boatType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.7, 0, -0.7, 0.7, 0.455, 0.7)
}

func (boatType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := boatConf
	beh := conf.New()
	if v, ok := m["Variant"]; ok {
		beh.variant = readNBTInt(v)
	}
	data.Data = beh
}

func (boatType) EncodeNBT(data *world.EntityData) map[string]any {
	b := boatBaseBehaviour(data.Data)
	if b == nil {
		return map[string]any{}
	}
	return map[string]any{"Variant": b.variant}
}

var boatConf = BoatBehaviourConfig{MaxPassengers: 2}

// ChestBoatBehaviourConfig configures chest boat behaviour.
type ChestBoatBehaviourConfig struct {
	Boat BoatBehaviourConfig
	Size int
}

// Apply applies the configuration to the entity data.
func (conf ChestBoatBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates a new chest boat behaviour.
func (conf ChestBoatBehaviourConfig) New() *ChestBoatBehaviour {
	if conf.Size <= 0 {
		conf.Size = 27
	}
	conf.Boat.MaxPassengers = 1
	base := conf.Boat.New()
	b := &ChestBoatBehaviour{
		BoatBehaviour: base,
		containerSize: int32(conf.Size),
		viewerMu:      new(sync.RWMutex),
		viewers:       make(map[block.ContainerViewer]struct{}, 1),
	}
	b.inv = inventory.New(conf.Size, func(slot int, _, after item.Stack) {
		b.viewerMu.RLock()
		defer b.viewerMu.RUnlock()
		for viewer := range b.viewers {
			viewer.ViewSlotChange(slot, after)
		}
	})
	return b
}

// ChestBoatBehaviour implements behaviour for chest boats.
type ChestBoatBehaviour struct {
	*BoatBehaviour

	inv *inventory.Inventory

	viewerMu *sync.RWMutex
	viewers  map[block.ContainerViewer]struct{}

	containerSize int32
}

// Base returns the base boat behaviour.
func (b *ChestBoatBehaviour) Base() *BoatBehaviour {
	return b.BoatBehaviour
}

// Inventory returns the chest boat inventory.
func (b *ChestBoatBehaviour) Inventory() *inventory.Inventory {
	return b.inv
}

// ContainerType returns the container type metadata value.
func (b *ChestBoatBehaviour) ContainerType() int32 {
	return boatContainerTypeChest
}

// ContainerSize returns the container size metadata value.
func (b *ChestBoatBehaviour) ContainerSize() int32 {
	return b.containerSize
}

// ContainerStrengthModifier returns the container strength metadata value.
func (b *ChestBoatBehaviour) ContainerStrengthModifier() int32 {
	return 0
}

// InteractText returns the interaction prompt for chest boats.
func (b *ChestBoatBehaviour) InteractText() string {
	return "action.interact.opencontainer"
}

// AddViewer adds an inventory viewer.
func (b *ChestBoatBehaviour) AddViewer(v block.ContainerViewer) {
	b.viewerMu.Lock()
	b.viewers[v] = struct{}{}
	b.viewerMu.Unlock()
}

// RemoveViewer removes an inventory viewer.
func (b *ChestBoatBehaviour) RemoveViewer(v block.ContainerViewer) {
	b.viewerMu.Lock()
	delete(b.viewers, v)
	b.viewerMu.Unlock()
}

// ChestBoat is a chest boat entity.
type ChestBoat struct {
	*Boat
}

func (b *ChestBoat) chest() *ChestBoatBehaviour {
	if beh, ok := b.Behaviour().(*ChestBoatBehaviour); ok {
		return beh
	}
	return nil
}

// Interact handles interaction with a chest boat.
func (b *ChestBoat) Interact(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	beh := b.chest()
	if beh == nil {
		return false
	}
	if sneaking, ok := user.(interface{ Sneaking() bool }); ok && sneaking.Sneaking() {
		if opener, ok := user.(ContainerOpener); ok {
			opener.OpenEntityContainer(b, beh.Inventory(), byte(boatContainerTypeChest), tx)
			return true
		}
		return false
	}
	return b.Boat.Interact(tx, user, ctx)
}

// Destroy destroys the chest boat.
func (b *ChestBoat) Destroy(tx *world.Tx, src world.DamageSource, causer world.Entity) bool {
	beh := b.chest()
	if beh == nil {
		_ = b.CloseIn(tx)
		return true
	}
	damage := minecartDamageFromSource(src, causer)
	if damage >= 40 || beh.Hurt(damage) {
		for _, stack := range beh.inv.Clear() {
			if stack.Empty() {
				continue
			}
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: b.Position()}, stack))
		}
		beh.DismountAll(b.Ent, tx)
		if canDropMinecart(causer, tx.World()) {
			stack := item.NewStack(item.ChestBoat{Variant: item.BoatVariantFromInt(int(beh.variant))}, 1)
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: b.Position()}, stack))
		}
		_ = b.CloseIn(tx)
		return true
	}
	beh.broadcastState(b.Ent, tx)
	return true
}

// AddViewer adds an inventory viewer for the chest boat.
func (b *ChestBoat) AddViewer(v block.ContainerViewer) {
	if beh := b.chest(); beh != nil {
		beh.AddViewer(v)
	}
}

// RemoveViewer removes an inventory viewer for the chest boat.
func (b *ChestBoat) RemoveViewer(v block.ContainerViewer) {
	if beh := b.chest(); beh != nil {
		beh.RemoveViewer(v)
	}
}

// ChestBoatType is a world.EntityType implementation for chest boats.
var ChestBoatType chestBoatType

type chestBoatType struct{}

func (chestBoatType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &ChestBoat{Boat: &Boat{Ent: &Ent{tx: tx, handle: handle, data: data}}}
}

func (chestBoatType) EncodeEntity() string   { return "minecraft:chest_boat" }
func (chestBoatType) NetworkOffset() float64 { return boatNetworkOffset }
func (chestBoatType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.7, 0, -0.7, 0.7, 0.455, 0.7)
}

func (chestBoatType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := chestBoatConf
	beh := conf.New()
	if v, ok := m["Variant"]; ok {
		beh.variant = readNBTInt(v)
	}
	nbtconv.InvFromNBT(beh.inv, nbtconv.Slice(m, "Items"))
	data.Data = beh
}

func (chestBoatType) EncodeNBT(data *world.EntityData) map[string]any {
	beh, ok := data.Data.(*ChestBoatBehaviour)
	if !ok {
		return map[string]any{}
	}
	return map[string]any{
		"Variant": beh.variant,
		"Items":   nbtconv.InvToNBT(beh.inv),
	}
}

var chestBoatConf = ChestBoatBehaviourConfig{
	Boat: BoatBehaviourConfig{MaxPassengers: 1},
	Size: 27,
}
