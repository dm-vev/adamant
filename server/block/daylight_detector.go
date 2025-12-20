package block

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const daylightUpdateTicks = 20

// DaylightDetector is a sensor that outputs power based on the current daylight level.
type DaylightDetector struct {
	carpet
	transparent
	sourceWaterDisplacer

	Inverted bool
	Signal   int
}

// BreakInfo ...
func (d DaylightDetector) BreakInfo() BreakInfo {
	return newBreakInfo(0.2, alwaysHarvestable, nothingEffective, oneOf(d))
}

// UseOnBlock ...
func (d DaylightDetector) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, d)
	if !used {
		return false
	}
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		return false
	}
	d.Signal = d.daylightSignal(pos, tx)
	place(tx, pos, d, user, ctx)
	tx.DoBlockUpdatesAround(pos.Side(cube.FaceDown))
	return placed(ctx)
}

// Activate ...
func (d DaylightDetector) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	d.Inverted = !d.Inverted
	d.updateSignal(pos, tx)
	return true
}

// NeighbourUpdateTick ...
func (d DaylightDetector) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		breakBlock(d, pos, tx)
		return
	}
	d.updateSignal(pos, tx)
}

// ScheduledTick ...
func (d DaylightDetector) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	d.updateSignal(pos, tx)
}

// RedstoneWeakPower ...
func (d DaylightDetector) RedstoneWeakPower(cube.Face) uint8 {
	return uint8(d.Signal)
}

// RedstoneStrongPower ...
func (d DaylightDetector) RedstoneStrongPower(face cube.Face) uint8 {
	if face == cube.FaceDown {
		return uint8(d.Signal)
	}
	return 0
}

// EncodeItem ...
func (DaylightDetector) EncodeItem() (name string, meta int16) {
	return "minecraft:daylight_detector", 0
}

// EncodeBlock ...
func (d DaylightDetector) EncodeBlock() (string, map[string]any) {
	name := "minecraft:daylight_detector"
	if d.Inverted {
		name = "minecraft:daylight_detector_inverted"
	}
	return name, map[string]any{"redstone_signal": int32(d.Signal)}
}

func (d DaylightDetector) updateSignal(pos cube.Pos, tx *world.Tx) {
	newSignal := d.daylightSignal(pos, tx)
	if newSignal != d.Signal {
		d.Signal = newSignal
		tx.SetBlock(pos, d, nil)
		tx.DoBlockUpdatesAround(pos.Side(cube.FaceDown))
	}
	tx.ScheduleBlockUpdate(pos, d, redstoneTicks(daylightUpdateTicks))
}

func (d DaylightDetector) daylightSignal(pos cube.Pos, tx *world.Tx) int {
	sky := int(tx.SkyLight(pos))
	if sky <= 0 {
		return d.applyInversion(0)
	}
	time := tx.World().Time() % world.TimeFull
	angle := float64(time) / float64(world.TimeFull) * (2 * math.Pi)
	brightness := math.Sin(angle)
	if brightness < 0 {
		brightness = 0
	}
	signal := int(math.Round(brightness * float64(sky)))
	if signal > 15 {
		signal = 15
	}
	return d.applyInversion(signal)
}

func (d DaylightDetector) applyInversion(signal int) int {
	if d.Inverted {
		return 15 - signal
	}
	return signal
}

func allDaylightDetectors() (detectors []world.Block) {
	for _, inverted := range []bool{false, true} {
		for signal := 0; signal <= 15; signal++ {
			detectors = append(detectors, DaylightDetector{Inverted: inverted, Signal: signal})
		}
	}
	return
}
