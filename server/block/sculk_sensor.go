package block

import (
	"math"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// SculkSensorPhase is the native sensor phase. Its numeric value is the exact
// Bedrock palette value in sculk_sensor_phase.
type SculkSensorPhase uint8

const (
	SculkSensorInactive SculkSensorPhase = iota
	SculkSensorActive
	SculkSensorCooldown
)

func (p SculkSensorPhase) String() string {
	switch p {
	case SculkSensorActive:
		return "active"
	case SculkSensorCooldown:
		return "cooldown"
	default:
		return "inactive"
	}
}

type sculkSensorData struct {
	power                   int
	lastVibrationFrequency  int
	listener                world.VibrationData
	nativeVibrationListener map[string]any
}

func newSculkSensorData() *sculkSensorData {
	return &sculkSensorData{}
}

func (d sculkSensorData) nativeListenerNBT() map[string]any {
	if d.nativeVibrationListener != nil {
		return d.nativeVibrationListener
	}
	return map[string]any{"event": int32(28), "selector": map[string]any{}}
}

// SculkSensor stores Java listener and comparator memory in actor data while
// its phase is represented by the native sculk_sensor_phase palette state.
type SculkSensor struct {
	sourceWaterDisplacer
	Phase SculkSensorPhase
	data  *sculkSensorData
}

// NewSculkSensor creates an inactive sensor with independent actor data.
func NewSculkSensor() SculkSensor {
	return SculkSensor{data: newSculkSensorData()}
}

// NewSculkSensorState constructs a Java-derived sensor state. Power is actor
// memory because the native block palette has no redstone power property.
func NewSculkSensorState(phase SculkSensorPhase, power, lastVibrationFrequency int) SculkSensor {
	s := NewSculkSensor()
	s.Phase = phase
	s.data.power = clampSensorPower(power)
	s.data.lastVibrationFrequency = clampSensorFrequency(lastVibrationFrequency)
	return s
}

// WithPower stores Java redstone power in per-block actor memory.
func (s SculkSensor) WithPower(power int) SculkSensor {
	d := s.dataValue()
	d.power = clampSensorPower(power)
	s.data = &d
	return s
}

// JavaPower returns the Java signal carried by the sensor actor.
func (s SculkSensor) JavaPower() int {
	if s.data == nil {
		return 0
	}
	return s.data.power
}

// LastVibrationFrequency returns the comparator value retained by the actor.
func (s SculkSensor) LastVibrationFrequency() int {
	if s.data == nil {
		return 0
	}
	return s.data.lastVibrationFrequency
}

func (s SculkSensor) EncodeBlock() (string, map[string]any) {
	return "minecraft:sculk_sensor", map[string]any{"sculk_sensor_phase": int32(s.Phase)}
}
func (SculkSensor) EncodeItem() (string, int16) { return "minecraft:sculk_sensor", 0 }
func (s SculkSensor) Hash() (uint64, uint64)    { return hashSculkSensor, uint64(s.Phase) }
func (SculkSensor) Model() world.BlockModel     { return model.Slab{} }
func (s SculkSensor) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, alwaysHarvestable, hoeEffective, silkTouchOnlyDrop(s))
}
func (s SculkSensor) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used {
		return false
	}
	s.data = newSculkSensorData()
	place(tx, pos, s, user, ctx)
	return placed(ctx)
}

// InitialiseVibrationListener ensures a freshly placed/generated actor has
// independent listener storage before the event dispatcher inspects it.
func (s SculkSensor) InitialiseVibrationListener() world.Block {
	if s.data == nil {
		s.data = newSculkSensorData()
	}
	return s
}
func (s SculkSensor) VibrationUser() world.VibrationUser { return s }
func (s SculkSensor) VibrationData() *world.VibrationData {
	if s.data == nil {
		return nil
	}
	return &s.data.listener
}
func (SculkSensor) RequiresAdjacentChunksToBeTicking() bool { return true }
func (SculkSensor) VibrationRadius() int                    { return 8 }
func (s SculkSensor) VibrationParticle(destination mgl64.Vec3, travelTicks int) world.Particle {
	return particle.VibrationSignal{Destination: destination, TravelTicks: travelTicks}
}
func (s SculkSensor) CanReceiveVibration(_ *world.Tx, listenerPos, eventPos cube.Pos, event world.GameEvent, _ world.GameEventContext) bool {
	return s.Phase == SculkSensorInactive && world.GameEventFrequency(event) != 0 &&
		!(eventPos == listenerPos && (event == world.GameEventBlockPlace || event == world.GameEventBlockDestroy))
}
func (s SculkSensor) ReceiveVibration(tx *world.Tx, listenerPos, _ cube.Pos, info world.VibrationInfo, distance float32) {
	s.receiveVibration(tx, listenerPos, info, distance, s.VibrationRadius(), 30, s)
}
func (s SculkSensor) receiveVibration(tx *world.Tx, pos cube.Pos, info world.VibrationInfo, distance float32, radius, activeTicks int, state world.Block) {
	if s.Phase != SculkSensorInactive {
		return
	}
	if s.data == nil {
		s.data = newSculkSensorData()
	}
	s.data.lastVibrationFrequency = world.GameEventFrequency(info.Event)
	s.data.power = max(1, 15-int(math.Floor(15*float64(distance)/float64(radius))))
	switch block := state.(type) {
	case SculkSensor:
		block.data = s.data
		block.Phase = SculkSensorActive
		state = block
	case CalibratedSculkSensor:
		block.SculkSensor.data = s.data
		block.Phase = SculkSensorActive
		state = block
	default:
		panic("vibration received by unsupported sensor block")
	}
	tx.SetBlock(pos, state, nil)
	tx.ScheduleBlockUpdate(pos, state, time.Duration(activeTicks)*time.Second/20)
	s.tryResonate(pos, tx, info)
	source, _ := info.SourceEntity(tx)
	tx.EmitGameEvent(world.GameEventSculkSensorTendrilsClicking, pos.Vec3Centre(), world.GameEventContext{Source: source})
	if !sensorWaterlogged(tx, pos) {
		tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.sculk_sensor.clicking", Volume: 1, Pitch: sensorClickPitch(tx)})
	}
}
func (s SculkSensor) tryResonate(pos cube.Pos, tx *world.Tx, info world.VibrationInfo) {
	frequency := world.GameEventFrequency(info.Event)
	if frequency < 1 || frequency > 15 {
		return
	}
	source, _ := info.SourceEntity(tx)
	tones := [...]int{0, 0, 2, 4, 6, 7, 9, 10, 12, 14, 15, 18, 19, 21, 22, 24}
	pitch := math.Pow(2, float64(tones[frequency]-12)/12)
	for _, face := range cube.Faces() {
		target := pos.Side(face)
		block := tx.Block(target)
		name, _ := block.EncodeBlock()
		resonator, err := javadata.BlockTagContains("minecraft:vibration_resonators", name)
		if err != nil {
			panic(err)
		}
		if !resonator {
			continue
		}
		tx.EmitGameEvent(world.GameEvent("minecraft:resonate_"+strconv.Itoa(frequency)), target.Vec3Centre(), world.GameEventContext{Source: source, AffectedState: block})
		tx.PlaySound(target.Vec3Centre(), sound.Custom{Name: "block.amethyst_block.resonate", Volume: 1, Pitch: pitch})
	}
}
func (s SculkSensor) EntityStepOn(pos cube.Pos, tx *world.Tx, entity world.Entity) {
	if entity == nil || entity.H().Type().EncodeEntity() == "minecraft:warden" || s.Phase != SculkSensorInactive {
		return
	}
	context := world.GameEventContext{AffectedState: s}
	if !s.CanReceiveVibration(tx, pos, pos, world.GameEventStep, context) {
		return
	}
	tx.ForceScheduleVibration(pos, world.GameEventStep, world.GameEventContext{Source: entity}, entity.Position())
}
func (s SculkSensor) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if s.data == nil {
		s.data = newSculkSensorData()
		tx.SetBlockEntity(pos, s)
	}
	tx.World().TickVibrationSystem(tx, pos, s, &s.data.listener)
}
func (s SculkSensor) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if s.Phase == SculkSensorActive {
		s.Phase = SculkSensorCooldown
		s = s.WithPower(0)
		tx.SetBlock(pos, s, nil)
		tx.ScheduleBlockUpdate(pos, s, 10*time.Second/20)
		return
	}
	if s.Phase == SculkSensorCooldown {
		s.Phase = SculkSensorInactive
		tx.SetBlock(pos, s, nil)
		if !sensorWaterlogged(tx, pos) {
			tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.sculk_sensor.clicking_stop", Volume: 1, Pitch: sensorClickPitch(tx)})
		}
	}
}
func (s SculkSensor) RedstonePower(_ cube.Pos, _ *world.Tx, _ cube.Face) int { return s.JavaPower() }
func (s SculkSensor) RedstoneStrongPower(_ cube.Pos, tx *world.Tx, face cube.Face) int {
	if face == cube.FaceUp {
		return s.RedstonePower(cube.Pos{}, tx, face)
	}
	return 0
}
func (s SculkSensor) ComparatorOutput(*world.Tx, cube.Pos) uint8 {
	if s.Phase == SculkSensorActive {
		return uint8(s.LastVibrationFrequency())
	}
	return 0
}
func (s SculkSensor) dataValue() sculkSensorData {
	if s.data == nil {
		return *newSculkSensorData()
	}
	return *s.data
}
func (s SculkSensor) EncodeNBT() map[string]any {
	d := s.dataValue()
	return map[string]any{
		"id":                 "SculkSensor",
		"BlockEntityVersion": int32(0),
		"VibrationListener":  d.nativeListenerNBT(),
		"adamant:java": map[string]any{
			"power":                    int32(d.power),
			"last_vibration_frequency": int32(d.lastVibrationFrequency),
			"listener":                 d.listener.EncodeNBT(),
		},
	}
}
func (s SculkSensor) DecodeNBT(m map[string]any) any {
	d := newSculkSensorData()
	if native, ok := m["VibrationListener"].(map[string]any); ok {
		d.nativeVibrationListener = native
	}
	if java, ok := m["adamant:java"].(map[string]any); ok {
		if n, ok := java["power"].(int32); ok {
			d.power = clampSensorPower(int(n))
		}
		if n, ok := java["last_vibration_frequency"].(int32); ok {
			d.lastVibrationFrequency = clampSensorFrequency(int(n))
		}
		if listener, ok := java["listener"].(map[string]any); ok {
			d.listener = world.DecodeVibrationData(listener)
		}
	}
	s.data = d
	return s
}

// CalibratedSculkSensor filters events by the redstone signal at its back and
// uses the native cardinal-direction palette property.
type CalibratedSculkSensor struct {
	SculkSensor
	Facing cube.Direction
}

func NewCalibratedSculkSensor() CalibratedSculkSensor {
	return CalibratedSculkSensor{SculkSensor: NewSculkSensor(), Facing: cube.North}
}
func NewCalibratedSculkSensorState(phase SculkSensorPhase, facing cube.Direction, power, lastVibrationFrequency int) CalibratedSculkSensor {
	s := NewCalibratedSculkSensor()
	s.Phase, s.Facing = phase, facing
	s.data.power = clampSensorPower(power)
	s.data.lastVibrationFrequency = clampSensorFrequency(lastVibrationFrequency)
	return s
}
func (s CalibratedSculkSensor) EncodeBlock() (string, map[string]any) {
	return "minecraft:calibrated_sculk_sensor", map[string]any{"minecraft:cardinal_direction": s.Facing.String(), "sculk_sensor_phase": int32(s.Phase)}
}
func (CalibratedSculkSensor) EncodeItem() (string, int16) {
	return "minecraft:calibrated_sculk_sensor", 0
}
func (s CalibratedSculkSensor) Hash() (uint64, uint64) {
	return hashCalibratedSculkSensor, uint64(s.Phase) | uint64(s.Facing)<<2
}
func (CalibratedSculkSensor) Model() world.BlockModel { return model.Slab{} }
func (s CalibratedSculkSensor) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, alwaysHarvestable, hoeEffective, silkTouchOnlyDrop(s))
}
func (s CalibratedSculkSensor) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used {
		return false
	}
	s.SculkSensor.data = newSculkSensorData()
	s.Facing = user.Rotation().Direction()
	place(tx, pos, s, user, ctx)
	return placed(ctx)
}
func (s CalibratedSculkSensor) InitialiseVibrationListener() world.Block {
	if s.data == nil {
		s.data = newSculkSensorData()
	}
	return s
}
func (s CalibratedSculkSensor) VibrationUser() world.VibrationUser { return s }
func (s CalibratedSculkSensor) VibrationRadius() int               { return 16 }
func (s CalibratedSculkSensor) CanReceiveVibration(tx *world.Tx, listenerPos, eventPos cube.Pos, event world.GameEvent, context world.GameEventContext) bool {
	if !s.SculkSensor.CanReceiveVibration(tx, listenerPos, eventPos, event, context) {
		return false
	}
	direction := s.Facing.Opposite()
	frequency := tx.RedstonePowerFrom(listenerPos, direction.Face())
	return frequency == 0 || frequency == world.GameEventFrequency(event)
}
func (s CalibratedSculkSensor) ReceiveVibration(tx *world.Tx, listenerPos, _ cube.Pos, info world.VibrationInfo, distance float32) {
	s.SculkSensor.receiveVibration(tx, listenerPos, info, distance, s.VibrationRadius(), 10, s)
}
func (s CalibratedSculkSensor) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if s.data == nil {
		s.data = newSculkSensorData()
		tx.SetBlockEntity(pos, s)
	}
	tx.World().TickVibrationSystem(tx, pos, s, &s.data.listener)
}
func (s CalibratedSculkSensor) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if s.Phase == SculkSensorActive {
		s.Phase = SculkSensorCooldown
		s.SculkSensor = s.SculkSensor.WithPower(0)
		tx.SetBlock(pos, s, nil)
		tx.ScheduleBlockUpdate(pos, s, 10*time.Second/20)
		return
	}
	if s.Phase == SculkSensorCooldown {
		s.Phase = SculkSensorInactive
		tx.SetBlock(pos, s, nil)
		if !sensorWaterlogged(tx, pos) {
			tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.sculk_sensor.clicking_stop", Volume: 1, Pitch: sensorClickPitch(tx)})
		}
	}
}
func (s CalibratedSculkSensor) RedstonePower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	if face == s.Facing.Face() {
		return 0
	}
	return s.SculkSensor.RedstonePower(pos, tx, face)
}
func (s CalibratedSculkSensor) RedstoneStrongPower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	if face == cube.FaceUp {
		return s.RedstonePower(pos, tx, face)
	}
	return 0
}
func (s CalibratedSculkSensor) ComparatorOutput(tx *world.Tx, pos cube.Pos) uint8 {
	return s.SculkSensor.ComparatorOutput(tx, pos)
}
func (s CalibratedSculkSensor) EntityStepOn(pos cube.Pos, tx *world.Tx, entity world.Entity) {
	if entity == nil || entity.H().Type().EncodeEntity() == "minecraft:warden" || s.Phase != SculkSensorInactive {
		return
	}
	context := world.GameEventContext{AffectedState: s}
	if !s.CanReceiveVibration(tx, pos, pos, world.GameEventStep, context) {
		return
	}
	tx.ForceScheduleVibration(pos, world.GameEventStep, world.GameEventContext{Source: entity}, entity.Position())
}
func (s CalibratedSculkSensor) EncodeNBT() map[string]any {
	d := s.dataValue()
	return map[string]any{
		"id":                 "CalibratedSculkSensor",
		"BlockEntityVersion": int32(0),
		"VibrationListener":  d.nativeListenerNBT(),
		"adamant:java": map[string]any{
			"power":                    int32(d.power),
			"last_vibration_frequency": int32(d.lastVibrationFrequency),
			"listener":                 d.listener.EncodeNBT(),
		},
	}
}
func (s CalibratedSculkSensor) DecodeNBT(m map[string]any) any {
	s.SculkSensor = s.SculkSensor.DecodeNBT(m).(SculkSensor)
	return s
}

func sensorWaterlogged(tx *world.Tx, pos cube.Pos) bool {
	liquid, ok := tx.Liquid(pos)
	if !ok {
		return false
	}
	water, ok := liquid.(Water)
	return ok && !water.Falling && water.Depth == 8
}
func sensorClickPitch(tx *world.Tx) float64 {
	return float64(tx.JavaRandom().NextFloat()*0.2 + 0.8)
}
func clampSensorPower(power int) int         { return max(0, min(15, power)) }
func clampSensorFrequency(frequency int) int { return max(0, min(15, frequency)) }

var (
	hashSculkSensor           = NextHash()
	hashCalibratedSculkSensor = NextHash()
)

func allSculkSensors() []world.Block {
	blocks := make([]world.Block, 0, 3)
	for phase := SculkSensorPhase(0); phase <= SculkSensorCooldown; phase++ {
		blocks = append(blocks, SculkSensor{Phase: phase})
	}
	return blocks
}
func allCalibratedSculkSensors() []world.Block {
	blocks := make([]world.Block, 0, 12)
	for phase := SculkSensorPhase(0); phase <= SculkSensorCooldown; phase++ {
		for facing := cube.Direction(0); facing <= cube.Direction(3); facing++ {
			blocks = append(blocks, CalibratedSculkSensor{SculkSensor: SculkSensor{Phase: phase}, Facing: facing})
		}
	}
	return blocks
}

var _ world.ScheduledTicker = SculkSensor{}
var _ world.ScheduledTicker = CalibratedSculkSensor{}
