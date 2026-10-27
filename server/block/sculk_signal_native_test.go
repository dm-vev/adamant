package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestSculkSignalStrengthMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("../world/testdata/bedrock_1_26_51_vibration_selector.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Signals  []struct {
			Distance                    float32
			Radius, Strength, Frequency int
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Signals) != 110 {
		t.Fatal("incomplete executed-client sensor strength capture")
	}
	w := newMultifaceTestWorld(t)
	for i, c := range native.Signals {
		t.Run(fmt.Sprintf("%d/r%d/d%g", i, c.Radius, c.Distance), func(t *testing.T) {
			<-w.Exec(func(tx *world.Tx) {
				pos := cube.Pos{0, 65, 0}
				info := world.VibrationInfo{Event: world.GameEventBlockActivate}
				if c.Radius == 8 {
					s := NewSculkSensor()
					tx.SetBlock(pos, s, nil)
					s.ReceiveVibration(tx, pos, cube.Pos{}, info, c.Distance)
				} else if c.Radius == 16 {
					s := NewCalibratedSculkSensor()
					tx.SetBlock(pos, s, nil)
					s.ReceiveVibration(tx, pos, cube.Pos{}, info, c.Distance)
				} else {
					t.Fatal("unexpected native sensor radius")
				}
				b := tx.Block(pos).(interface {
					OutputPower() int
					LastVibrationFrequency() int
				})
				if b.OutputPower() != c.Strength || b.LastVibrationFrequency() != c.Frequency {
					t.Fatalf("strength/frequency=%d/%d, executed client=%d/%d", b.OutputPower(), b.LastVibrationFrequency(), c.Strength, c.Frequency)
				}
			})
		})
	}
}
