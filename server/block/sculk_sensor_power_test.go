package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestSculkSensorsStronglyPowerOnlyTheBlockBelow(t *testing.T) {
	sensors := map[string]world.Block{
		"sculk sensor":            NewSculkSensorState(SculkSensorActive, 9, 1),
		"calibrated sculk sensor": CalibratedSculkSensor{SculkSensor: NewSculkSensorState(SculkSensorActive, 9, 1), Facing: cube.North},
	}
	for name, sensor := range sensors {
		w := newMultifaceTestWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			tx.SetBlock(cube.Pos{0, 0, 0}, Stone{}, nil)
			tx.SetBlock(cube.Pos{0, 2, 0}, Stone{}, nil)
			tx.SetBlock(cube.Pos{0, 1, 0}, sensor, nil)
			if below, above := tx.RedstoneStrongPower(cube.Pos{0, 0, 0}), tx.RedstoneStrongPower(cube.Pos{0, 2, 0}); below != 9 || above != 0 {
				t.Fatalf("%s strongly powers below %d and above %d, want 9 and 0", name, below, above)
			}
		})
	}
}
