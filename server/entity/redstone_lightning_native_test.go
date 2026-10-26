package entity_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Replay real native lightning entities, including the block permutation and
// dust publication. Calling LightningRod.Strike alone misses entity integration.
func TestLightningRodsMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_lightning.json")
	if err != nil {
		t.Fatal(err)
	}
	type nativeBlock struct {
		ID     string
		States struct {
			Powered bool `json:"powered_bit"`
			Facing  int  `json:"facing_direction"`
			Signal  int  `json:"redstone_signal"`
		}
	}
	var capture struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Material            string
				Face, Phase, Repeat int
				On                  *int
				Height              *float64
			}
			Wire       [3]int
			StrongWire [3]int `json:"strong_wire"`
			Changes    []struct {
				Tick     int64
				Kind     string
				Location struct{ X, Y, Z float64 }
			}
			Samples []struct {
				Tick         int64
				Source, Wire nativeBlock
				StrongWire   nativeBlock `json:"strong_wire"`
				Bolts        []json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	if !capture.Complete || len(capture.Scenarios) != 100 {
		t.Fatal("incomplete native lightning capture")
	}
	for _, trace := range capture.Scenarios {
		c := trace.Scenario
		on, height := 8, .5
		if c.On != nil {
			on = *c.On
		}
		if c.Height != nil {
			height = *c.Height
		}
		t.Run(fmt.Sprintf("%s/face%d/phase%d/on%d/height%g/repeat%d", c.Material, c.Face, c.Phase, on, height, c.Repeat), func(t *testing.T) {
			expectedChanges := 2
			if c.Repeat != 0 {
				expectedChanges++
			}
			if len(trace.Samples) != 36 || len(trace.Changes) != expectedChanges || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native lightning trace")
			}
			seenBolt, seenPower := false, false
			for _, s := range trace.Samples {
				seenBolt = seenBolt || len(s.Bolts) != 0
				seenPower = seenPower || s.Source.States.Powered
			}
			if !seenBolt || (c.Height == nil && !seenPower) {
				t.Fatal("missing real lightning positive control")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			defer w.Close()
			w.SetDifficulty(world.DifficultyPeaceful)
			pos, wire, strongWire := cube.Pos{0, 65, 0}, cube.Pos(trace.Wire), cube.Pos(trace.StrongWire)
			strikeAt := func(tx *world.Tx, tick int64) {
				for _, change := range trace.Changes {
					if change.Kind == "strike" && change.Tick == tick {
						tx.AddEntity(entity.NewLightning(world.EntitySpawnOpts{Position: mgl64.Vec3{change.Location.X, change.Location.Y, change.Location.Z}}))
					}
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				for x := -4; x <= 4; x++ {
					for z := -4; z <= 4; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(pos.Side(cube.Face(c.Face).Opposite()), block.Stone{}, nil)
				rod, ok := world.BlockByName("minecraft:"+c.Material, map[string]any{"facing_direction": int32(c.Face), "powered_bit": false})
				if !ok {
					t.Fatal("unregistered rod", c.Material)
				}
				tx.SetBlock(pos, rod, nil)
				tx.SetBlock(wire, block.RedstoneWire{}, nil)
				tx.SetBlock(strongWire.Side(cube.FaceDown), block.Stone{}, nil)
				tx.SetBlock(strongWire, block.RedstoneWire{}, nil)
				strikeAt(tx, trace.Changes[0].Tick)
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Fatalf("tick %d, native %d", tx.CurrentTick(), sample.Tick)
					}
					r := tx.Block(pos).(block.LightningRod)
					id, _ := r.EncodeBlock()
					if id != sample.Source.ID || r.Powered != sample.Source.States.Powered || int(r.Facing) != sample.Source.States.Facing {
						t.Errorf("tick %d rod=%+v (%s), native=%+v", sample.Tick, r, id, sample.Source)
					}
					if v := tx.Block(wire).(block.RedstoneWire).Power; v != sample.Wire.States.Signal {
						t.Errorf("tick %d wire=%d, native=%d", sample.Tick, v, sample.Wire.States.Signal)
					}
					if v := tx.Block(strongWire).(block.RedstoneWire).Power; v != sample.StrongWire.States.Signal {
						t.Errorf("tick %d support wire=%d, native=%d", sample.Tick, v, sample.StrongWire.States.Signal)
					}
					strikeAt(tx, sample.Tick)
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
