package entity

import (
	"context"
	"math"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"log/slog"
)

// VehiclePassenger represents an entity that may ride another entity and expects to receive positional updates
// from the vehicle it is seated on. Entities returned from BoatBehaviour.Passengers will be asserted to this
// interface to keep rider transforms in sync with the boat.
type VehiclePassenger interface {
	// UpdatePassengerPosition updates the passenger position and rotation while seated on the vehicle. It
	// returns false if the passenger should be dismounted from the vehicle.
	UpdatePassengerPosition(ridden world.Entity, seat int, pos mgl64.Vec3, rot cube.Rotation) bool
}

// BoatBehaviourConfig is used to configure a boat entity upon creation.
type BoatBehaviourConfig struct {
	Variant BoatVariant
	Chest   bool
}

// Apply implements world.EntityConfig.
func (conf BoatBehaviourConfig) Apply(data *world.EntityData) {
	if conf.Variant == (BoatVariant{}) {
		conf.Variant, _ = BoatVariantByName("oak")
	}
	data.Data = conf.New()
}

// New creates a new BoatBehaviour using the config provided.
func (conf BoatBehaviourConfig) New() *BoatBehaviour {
	b := &BoatBehaviour{
		conf:     conf,
		mc:       &MovementComputer{Gravity: 0.04, Drag: 0.02, DragBeforeGravity: true},
		velocity: mgl64.Vec3{},
	}
	if conf.Chest {
		b.viewerMu = &sync.RWMutex{}
		b.viewers = make(map[block.ContainerViewer]struct{}, 1)
		b.inventory = inventory.New(27, b.onInventoryChange)
	}
	return b
}

// BoatBehaviour implements Behaviour for boat and chest boat entities.
type BoatBehaviour struct {
	conf BoatBehaviourConfig
	mc   *MovementComputer

	velocity       mgl64.Vec3
	leftPaddle     float32
	rightPaddle    float32
	passengerLock  sync.Mutex
	passengers     []*world.EntityHandle
	passengerCount atomic.Int32

	lastVehicleYaw float64
	haveVehicleYaw bool

	inventory *inventory.Inventory
	viewerMu  *sync.RWMutex
	viewers   map[block.ContainerViewer]struct{}

	input forwardInput
}

type forwardInput struct {
	forward float64
	left    bool
	right   bool

	vehicleYaw float64
	hasYaw     bool
}

func (b *BoatBehaviour) Variant() int32 { return b.conf.Variant.Variant() }

// HasChest reports if the boat contains a chest.
func (b *BoatBehaviour) HasChest() bool { return b.conf.Chest }

// Inventory returns the inventory of the chest boat. It returns nil for normal boats.
func (b *BoatBehaviour) Inventory() *inventory.Inventory { return b.inventory }

// SetInput sets the input state of the boat, typically coming from the rider.
func (b *BoatBehaviour) SetInput(forward float64, left, right bool, vehicleYaw float64, hasYaw bool) {
	validYaw := hasYaw && !math.IsNaN(vehicleYaw) && !math.IsInf(vehicleYaw, 1) && !math.IsInf(vehicleYaw, -1)
	var yaw float64
	if validYaw {
		yaw = wrapDegrees(vehicleYaw)
		b.lastVehicleYaw = yaw
		b.haveVehicleYaw = true
	} else {
		if !hasYaw {
			b.haveVehicleYaw = false
		}
		if b.haveVehicleYaw {
			yaw = b.lastVehicleYaw
		}
	}
	b.input = forwardInput{forward: forward, left: left, right: right, vehicleYaw: yaw, hasYaw: validYaw}
}

// AddPassenger attempts to add a passenger to the boat. It returns the seat index assigned to the passenger and
// true if successful.
func (b *BoatBehaviour) AddPassenger(e *world.EntityHandle) (int, bool) {
	b.passengerLock.Lock()
	defer b.passengerLock.Unlock()

	if len(b.passengers) >= b.maxPassengers() {
		return -1, false
	}
	if seat, ok := b.passengerSeatLocked(e); ok {
		return seat, true
	}
	b.passengers = append(b.passengers, e)
	b.passengerCount.Store(int32(len(b.passengers)))
	return len(b.passengers) - 1, true
}

// RemovePassenger removes a passenger from the boat.
func (b *BoatBehaviour) RemovePassenger(e *world.EntityHandle) {
	b.passengerLock.Lock()
	defer b.passengerLock.Unlock()

	cleared := false
	for i, p := range b.passengers {
		if p == e {
			b.passengers = append(b.passengers[:i], b.passengers[i+1:]...)
			b.passengerCount.Store(int32(len(b.passengers)))
			cleared = i == 0
			break
		}
	}
	if cleared || len(b.passengers) == 0 {
		b.resetInputLocked()
	}
}

func (b *BoatBehaviour) resetInputLocked() {
	b.input = forwardInput{}
	b.haveVehicleYaw = false
	b.lastVehicleYaw = 0
	b.leftPaddle = 0
	b.rightPaddle = 0
	b.velocity = mgl64.Vec3{}
}

// PassengerSeat attempts to find the seat index the passenger occupies.
func (b *BoatBehaviour) PassengerSeat(e *world.EntityHandle) (int, bool) {
	b.passengerLock.Lock()
	defer b.passengerLock.Unlock()

	return b.passengerSeatLocked(e)
}

func (b *BoatBehaviour) passengerSeatLocked(e *world.EntityHandle) (int, bool) {
	for i, p := range b.passengers {
		if p == e {
			return i, true
		}
	}
	return -1, false
}

// Passengers returns the handles of all passengers currently seated in the boat.
func (b *BoatBehaviour) Passengers() []*world.EntityHandle {
	b.passengerLock.Lock()
	defer b.passengerLock.Unlock()

	out := make([]*world.EntityHandle, len(b.passengers))
	copy(out, b.passengers)
	return out
}

// PaddleTimes returns the current paddle animation values for metadata synchronisation.
func (b *BoatBehaviour) PaddleTimes() (float32, float32) {
	return b.leftPaddle, b.rightPaddle
}

// ControllingSeat returns the seat index that is permitted to control the boat.
func (b *BoatBehaviour) ControllingSeat() int32 {
	return 0
}

func (b *BoatBehaviour) maxPassengers() int {
	if b.conf.Chest {
		return 1
	}
	return 2
}

func (b *BoatBehaviour) onInventoryChange(slot int, before, after item.Stack) {
	if b.viewerMu == nil {
		return
	}
	b.viewerMu.RLock()
	defer b.viewerMu.RUnlock()
	for viewer := range b.viewers {
		viewer.ViewSlotChange(slot, after)
	}
}

// AddViewer adds a viewer to the chest boat inventory.
func (b *BoatBehaviour) AddViewer(v block.ContainerViewer) {
	if b.viewerMu == nil {
		return
	}
	b.viewerMu.Lock()
	b.viewers[v] = struct{}{}
	b.viewerMu.Unlock()
}

// RemoveViewer removes a viewer from the chest boat inventory.
func (b *BoatBehaviour) RemoveViewer(v block.ContainerViewer) {
	if b.viewerMu == nil {
		return
	}
	b.viewerMu.Lock()
	delete(b.viewers, v)
	b.viewerMu.Unlock()
}

// Tick implements Behaviour and performs movement and buoyancy handling for the boat.
func (b *BoatBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	pos := e.Position()
	vel := e.Velocity()

	var yawDeg float64
	vel, yawDeg = b.applyInput(vel, e, tx)
	vel = b.applyBuoyancy(pos, vel, tx)
	b.velocity = vel

	m := b.mc.TickMovement(e, pos, vel, e.Rotation(), tx)
	e.data.Pos, e.data.Vel = m.pos, m.vel

	// Record paddle animation values for metadata. Rough approximation of vanilla behaviour.
	forward := b.input.forward
	if forward > 0 {
		b.leftPaddle += 0.25
		b.rightPaddle += 0.25
	} else if forward < 0 {
		b.leftPaddle -= 0.3
		b.rightPaddle -= 0.3
	} else {
		if b.input.left {
			b.leftPaddle += 0.35
		} else {
			b.leftPaddle *= 0.9
		}
		if b.input.right {
			b.rightPaddle += 0.35
		} else {
			b.rightPaddle *= 0.9
		}
	}
	m.rot = e.data.Rot

	b.updatePassengers(e, tx)

	b.debugTick(tx, e, yawDeg, vel)

	return m
}

func (b *BoatBehaviour) debugTick(tx *world.Tx, e *Ent, yawDeg float64, vel mgl64.Vec3) {
	if tx == nil {
		return
	}
	logger := tx.Log()
	if logger == nil || !logger.Enabled(context.Background(), slog.LevelDebug) {
		return
	}
	variant := b.conf.Variant.Name()
	if variant == "" {
		variant = "unknown"
	}
	attrs := []slog.Attr{
		slog.String("variant", variant),
		slog.Bool("chest", b.conf.Chest),
		slog.Float64("input_forward", b.input.forward),
		slog.Bool("input_left", b.input.left),
		slog.Bool("input_right", b.input.right),
		slog.Float64("yaw", yawDeg),
		slog.Float64("stored_yaw", b.lastVehicleYaw),
		slog.Float64("pos_x", e.data.Pos[0]),
		slog.Float64("pos_y", e.data.Pos[1]),
		slog.Float64("pos_z", e.data.Pos[2]),
		slog.Float64("vel_x", vel[0]),
		slog.Float64("vel_y", vel[1]),
		slog.Float64("vel_z", vel[2]),
		slog.Int("passengers", int(b.passengerCount.Load())),
	}
	logger.LogAttrs(context.Background(), slog.LevelDebug, "boat tick", attrs...)
}

func (b *BoatBehaviour) applyInput(vel mgl64.Vec3, e *Ent, tx *world.Tx) (mgl64.Vec3, float64) {
	rot := e.Rotation()
	yaw := rot.Yaw()
	if b.input.hasYaw {
		yaw = b.input.vehicleYaw
		b.lastVehicleYaw = yaw
		b.haveVehicleYaw = true
	} else if b.haveVehicleYaw {
		yaw = b.lastVehicleYaw
	}

	yaw = wrapDegrees(yaw)
	yawRad := yaw * (math.Pi / 180)

	if b.input.left {
		yawRad -= 0.05
	}
	if b.input.right {
		yawRad += 0.05
	}

	yawDeg := wrapDegrees(yawRad * (180 / math.Pi))
	b.lastVehicleYaw = yawDeg
	b.haveVehicleYaw = true
	b.input.vehicleYaw = yawDeg
	b.input.hasYaw = b.haveVehicleYaw
	e.data.Rot = cube.Rotation{yawDeg, rot.Pitch()}

	forward := b.input.forward
	if forward != 0 {
		// Boats accelerate more quickly on ice to emulate vanilla behaviour.
		speedMul := 0.02
		if b.onIce(e, tx) {
			speedMul = 0.05
		}
		vel[0] += -math.Sin(yawRad) * speedMul * forward
		vel[2] += math.Cos(yawRad) * speedMul * forward
	}

	vel[0] *= 0.96
	vel[2] *= 0.96

	return vel, yawDeg
}

func (b *BoatBehaviour) applyBuoyancy(pos, vel mgl64.Vec3, tx *world.Tx) mgl64.Vec3 {
	base := cube.PosFromVec3(pos)
	if liquid, ok := tx.Liquid(base); ok {
		vel[1] = b.applyLiquidBuoyancy(vel[1], pos[1], float64(base[1]), liquid, 1)
		return vel
	}
	if liquid, ok := tx.Liquid(base.Side(cube.FaceDown)); ok {
		vel[1] = b.applyLiquidBuoyancy(vel[1], pos[1], float64(base[1]-1), liquid, 0.6)
		return vel
	}
	return vel
}

func (b *BoatBehaviour) updatePassengers(e *Ent, tx *world.Tx) {
	b.passengerLock.Lock()
	defer b.passengerLock.Unlock()

	if len(b.passengers) == 0 {
		return
	}

	yaw := e.data.Rot.Yaw() * (math.Pi / 180)
	origin := e.data.Pos
	offsets := b.passengerOffsets()

	kept := b.passengers[:0]
	for seat, handle := range b.passengers {
		if handle == nil {
			continue
		}
		passenger, ok := handle.Entity(tx)
		if !ok {
			continue
		}
		vp, ok := passenger.(VehiclePassenger)
		if !ok {
			continue
		}
		idx := seat
		if idx < 0 {
			idx = 0
		}
		if idx >= len(offsets) {
			idx = len(offsets) - 1
		}
		offset := rotateOffset(offsets[idx], yaw)
		if vp.UpdatePassengerPosition(e, seat, origin.Add(offset), e.data.Rot) {
			kept = append(kept, handle)
		}
	}
	b.passengers = kept
	b.passengerCount.Store(int32(len(b.passengers)))
}

func (b *BoatBehaviour) passengerOffsets() []mgl64.Vec3 {
	const seatHeight = 1.05
	if b.conf.Chest {
		return []mgl64.Vec3{{0, seatHeight, -0.1}}
	}
	return []mgl64.Vec3{{0, seatHeight, -0.25}, {0, seatHeight, 0.25}}
}

// SeatOffset returns the relative offset of a given seat index.
func (b *BoatBehaviour) SeatOffset(seat int) mgl64.Vec3 {
	offsets := b.passengerOffsets()
	if seat < 0 {
		seat = 0
	}
	if seat >= len(offsets) {
		seat = len(offsets) - 1
	}
	return offsets[seat]
}

func rotateOffset(offset mgl64.Vec3, yaw float64) mgl64.Vec3 {
	sin, cos := math.Sincos(yaw)
	return mgl64.Vec3{
		offset[0]*cos - offset[2]*sin,
		offset[1],
		offset[2]*cos + offset[0]*sin,
	}
}

func (b *BoatBehaviour) applyLiquidBuoyancy(current, posY, baseY float64, liquid world.Liquid, strength float64) float64 {
	surface := liquidSurfaceHeight(baseY, liquid)

	fullDraft := 0.024
	shallowDraft := 0.012

	if b.conf.Chest {
		fullDraft += 0.008
		shallowDraft += 0.006
	}

	if count := int(b.passengerCount.Load()); count > 0 {
		extra := 0.0035 * float64(count)
		fullDraft += extra
		shallowDraft += extra * 0.6
	}

	target := surface - fullDraft
	if strength < 1 {
		target = surface - shallowDraft
	}

	delta := target - posY

	gravityComp := b.mc.Gravity / (1 - b.mc.Drag)
	if strength < 0 {
		strength = 0
	} else if strength > 1 {
		strength = 1
	}

	upLimit := 0.32 * strength
	downLimit := 0.22 * strength
	accel := clampFloat64(delta*0.8, -downLimit, upLimit)

	if delta > 0 {
		current *= 0.4
	} else {
		current *= 0.7
	}

	return current + gravityComp*strength + accel
}

func liquidSurfaceHeight(baseY float64, liquid world.Liquid) float64 {
	depth := clampFloat64(float64(liquid.LiquidDepth()), 1, 8)
	return baseY + depth/8
}

func clampFloat64(v, minVal, maxVal float64) float64 {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

func wrapDegrees(v float64) float64 {
	v = math.Mod(v, 360)
	if v >= 180 {
		v -= 360
	}
	if v < -180 {
		v += 360
	}
	return v
}

func (b *BoatBehaviour) onIce(e *Ent, tx *world.Tx) bool {
	if tx == nil {
		return false
	}
	pos := cube.PosFromVec3(e.Position()).Side(cube.FaceDown)
	switch tx.Block(pos).(type) {
	case block.BlueIce, block.PackedIce:
		return true
	}
	return false
}

// BoatType implements world.EntityType for normal boats.
var BoatType boatType

// ChestBoatType implements world.EntityType for chest boats.
var ChestBoatType chestBoatType

type boatType struct{}

func (boatType) EncodeEntity() string { return "minecraft:boat" }
func (boatType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.7, 0, -0.7, 0.7, 0.6, 0.7)
}

func (boatType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (boatType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := BoatBehaviourConfig{}
	if variant, ok := m["Variant"].(int32); ok {
		for _, v := range BoatVariants() {
			if v.variant == variant {
				conf.Variant = v
				break
			}
		}
	}
	data.Data = conf.New()
}

func (boatType) EncodeNBT(data *world.EntityData) map[string]any {
	beh := data.Data.(*BoatBehaviour)
	return map[string]any{
		"Variant": beh.conf.Variant.variant,
	}
}

type chestBoatType struct{ boatType }

func (chestBoatType) EncodeEntity() string { return "minecraft:chest_boat" }

func (chestBoatType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := BoatBehaviourConfig{Chest: true}
	if variant, ok := m["Variant"].(int32); ok {
		for _, v := range BoatVariants() {
			if v.variant == variant {
				conf.Variant = v
				break
			}
		}
	}
	data.Data = conf.New()
}
