package block

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// daylightUpdateTicks is how often, in game ticks, a daylight detector
// measures the sky, using Level's day clock rather than elapsed game ticks.
const daylightUpdateTicks = 20

// DaylightDetector is a sensor that outputs power based on the current daylight level.
type DaylightDetector struct {
	transparent
	sourceWaterDisplacer

	Inverted bool
	Signal   int
}

// Model uses the native constructor's body bounds, rather than carpet height.
func (DaylightDetector) Model() world.BlockModel { return model.DaylightDetector{} }

// ObstructsRain follows the native precipitation component for both variants.
func (DaylightDetector) ObstructsRain() bool { return true }

// BreakInfo ...
func (d DaylightDetector) BreakInfo() BreakInfo {
	return newBreakInfo(0.2, alwaysHarvestable, nothingEffective, oneOf(d))
}

// UseOnBlock places the detector unpowered until its next block actor sample.
func (d DaylightDetector) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, d)
	if !used {
		return false
	}
	d.Signal = 0
	place(tx, pos, d, user, ctx)
	return placed(ctx)
}

// Activate follows the native interaction: publish the complementary state,
// then immediately sample the sky through the new variant's callback.
func (d DaylightDetector) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	d.Inverted = !d.Inverted
	d.Signal = 15 - d.Signal
	tx.Redstone().SetProducerBlock(pos, d)
	d.updateSignal(pos, tx)
	return true
}

// Tick follows DaylightDetectorBlockActor::tick (client 0x4abfe70). Freezing
// the day clock on a nonmultiple of twenty also freezes detector sampling.
func (d DaylightDetector) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if int32(tx.World().Time())%daylightUpdateTicks == 0 {
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
	signal := d.daylightSignal(pos, tx)
	if signal != d.Signal {
		d.Signal = signal
		tx.SetBlock(pos, d, nil)
		tx.DoBlockUpdatesAround(pos.Side(cube.FaceDown))
	}
	// Native updateSignalStrength looks up the active scene graph even if
	// the block property is unchanged. Pending setup components are separate.
	tx.Redstone().SetProducerStrength(pos, signal)
}

func (d DaylightDetector) RedstoneProducerPower() int                   { return d.Signal }
func (d DaylightDetector) RedstoneProducerOutput(power int) world.Block { d.Signal = power; return d }

// daylightSignal follows DaylightDetectorBlock::updateSignalStrength
// (client 0x977f7c0), including the no-skylight dimension gate.
func (d DaylightDetector) daylightSignal(pos cube.Pos, tx *world.Tx) int {
	if tx.World().Dimension() != world.Overworld {
		return 0
	}
	angle := bedrockDaylightAngle(tx.World().Time(), tx.World().TimeCycle())
	target := int(tx.SkyLight(pos)) - bedrockDaylightDarken(angle, tx.Raining(), tx.Thundering())
	sunAngle := float32(angle*float32(math.Pi)) * 2
	if d.Inverted {
		target = 15 - target
	} else if target > 0 {
		offset := float32(0)
		if sunAngle >= float32(math.Pi) {
			offset = float32(math.Pi * 2)
		}
		sunAngle += (offset - sunAngle) * 0.2
		target = int(math.Round(float64(float32(target) * bedrockDaylightCos(sunAngle))))
	}
	return min(max(target, 0), 15)
}

// These calculations are separate from the world's Java 26.3 environment
// attributes: Bedrock's dimension and detector still use a 65536-entry table
// with binary32 index arithmetic and the older celestial-angle easing.
var bedrockDaylightSines = func() (table [65536]float32) {
	for i := range table {
		table[i] = float32(math.Sin(float64(i) * (math.Pi * 2 / 65536)))
	}
	return
}()

func bedrockDaylightCos(angle float32) float32 {
	index := float32(angle * 10430.378)
	return bedrockDaylightSines[uint16(int32(index+16384))]
}

// Dimension::getTimeOfDay, client 0x23122e0. The server tick supplies partial=1
// when the day clock advances, and zero when frozen. Native captures at the
// 12040 darkening boundary distinguish both paths.
func bedrockDaylightAngle(time int, advancing bool) float32 {
	partial := float32(0)
	if advancing {
		partial = 1
	}
	angle := (float32(int32(time)%24000)+partial)/24000 - .25
	if angle < 0 {
		angle++
	}
	if angle > 1 {
		angle--
	}
	eased := float32(bedrockDaylightCos(angle*float32(math.Pi)) + 1)
	eased = eased*-.5 + 1
	return angle + (eased-angle)/3
}

// Dimension::tick's cached sky darkening (0x230b0de..0x230b205) differs
// from its render brightness getter. Current world weather exposes only
// inactive/full-strength rain and thunder; fog and partial levels are absent.
func bedrockDaylightDarken(angle float32, rain, thunder bool) int {
	cosine := bedrockDaylightCos(float32(angle*float32(math.Pi)) * 2)
	darkness := float32(1) - (float32(cosine*2) + .5)
	brightness := float32(1) - min(max(darkness, 0), 1)
	if rain {
		brightness *= 1 - float32(5)/16
	}
	if thunder {
		brightness *= 1 - float32(5)/16
	}
	return int(float32(1-brightness) * 11)
}

func allDaylightDetectors() (detectors []world.Block) {
	for _, inverted := range []bool{false, true} {
		for signal := 0; signal <= 15; signal++ {
			detectors = append(detectors, DaylightDetector{Inverted: inverted, Signal: signal})
		}
	}
	return
}
