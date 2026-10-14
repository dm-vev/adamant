package block

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// daylightUpdateTicks is how often, in game ticks, a daylight detector
// measures the sky: DaylightDetectorBlock.tickEntity runs on every 20th tick.
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

// UseOnBlock places the detector unpowered: like Java's default state, it
// only measures the sky on its next update tick.
func (d DaylightDetector) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, d)
	if !used {
		return false
	}
	d.Signal = 0
	place(tx, pos, d, user, ctx)
	return placed(ctx)
}

// Activate inverts the detector and measures the sky at once, as
// DaylightDetectorBlock.useWithoutItem does.
func (d DaylightDetector) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	d.Inverted = !d.Inverted
	tx.SetBlock(pos, d, nil)
	d.updateSignal(pos, tx)
	return true
}

// Tick measures the sky on every 20th game tick.
func (d DaylightDetector) Tick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	if currentTick%daylightUpdateTicks == 0 {
		d.updateSignal(pos, tx)
	}
}

func (d DaylightDetector) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	return d.Signal
}

func (d DaylightDetector) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if face == cube.FaceDown {
		return d.Signal
	}
	return 0
}

// EncodeNBT encodes the DaylightDetector block actor.
func (d DaylightDetector) EncodeNBT() map[string]any {
	return map[string]any{"id": "DaylightDetector"}
}

// DecodeNBT keeps the detector's state: its block actor holds no data.
func (d DaylightDetector) DecodeNBT(map[string]any) any {
	return d
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
	if signal := d.daylightSignal(pos, tx); signal != d.Signal {
		d.Signal = signal
		tx.SetBlock(pos, d, nil)
		tx.DoBlockUpdatesAround(pos.Side(cube.FaceDown))
	}
}

// daylightSignal ports DaylightDetectorBlock.updateSignalStrength: the sky
// brightness, scaled by the cosine of the sun angle eased a fifth of the way
// towards the nearest noon, or inverted.
func (d DaylightDetector) daylightSignal(pos cube.Pos, tx *world.Tx) int {
	target := int(tx.SkyLight(pos)) - tx.SkyDarken()
	sunAngle := tx.SunAngle() * float32(math.Pi/180)
	if d.Inverted {
		target = 15 - target
	} else if target > 0 {
		offset := float32(0)
		if sunAngle >= float32(math.Pi) {
			offset = float32(math.Pi * 2)
		}
		sunAngle += (offset - sunAngle) * 0.2
		target = int(math.Floor(float64(float32(target)*java26.Cos(float64(sunAngle)) + 0.5)))
	}
	return min(max(target, 0), 15)
}

func allDaylightDetectors() (detectors []world.Block) {
	for _, inverted := range []bool{false, true} {
		for signal := 0; signal <= 15; signal++ {
			detectors = append(detectors, DaylightDetector{Inverted: inverted, Signal: signal})
		}
	}
	return
}
