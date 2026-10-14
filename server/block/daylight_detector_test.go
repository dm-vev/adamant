package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestDaylightDetectorMeasuresSkyOnUpdateTicks checks the detector follows
// Java: it is placed unpowered, measures the sky only on every 20th game
// tick, outputs full power at noon and none at midnight, and inverted it
// outputs what the night sky leaves dark.
func TestDaylightDetectorMeasuresSkyOnUpdateTicks(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	w.SetTimeCycle(false)
	pos := cube.Pos{0, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		w.SetTime(6000)
		tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
		DaylightDetector{}.UseOnBlock(pos.Side(cube.FaceDown), cube.FaceUp, mgl64.Vec3{}, tx, &pistonTestUser{pos: mgl64.Vec3{5, 64, 5}}, &item.UseContext{})
		detector := tx.Block(pos).(DaylightDetector)
		if detector.Signal != 0 {
			t.Fatalf("placed detector signal = %d, want 0", detector.Signal)
		}
		detector.Tick(19, pos, tx)
		if got := tx.Block(pos).(DaylightDetector).Signal; got != 0 {
			t.Fatalf("signal after tick 19 = %d, want 0 until the next 20th tick", got)
		}
		detector.Tick(20, pos, tx)
		if got := tx.Block(pos).(DaylightDetector).Signal; got != 15 {
			t.Fatalf("noon signal = %d, want 15", got)
		}

		w.SetTime(18000)
		tx.Block(pos).(DaylightDetector).Tick(40, pos, tx)
		if got := tx.Block(pos).(DaylightDetector).Signal; got != 0 {
			t.Fatalf("midnight signal = %d, want 0", got)
		}
		tx.SetBlock(pos, DaylightDetector{Inverted: true}, nil)
		tx.Block(pos).(DaylightDetector).Tick(60, pos, tx)
		if got := tx.Block(pos).(DaylightDetector).Signal; got != 11 {
			t.Fatalf("inverted midnight signal = %d, want 11", got)
		}
	})
}
