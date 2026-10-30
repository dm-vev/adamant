package player

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestTripwirePlayerPlacementMatchesBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_hook_placement.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Sounds    []tripwireNativeSound
		Scenarios []struct {
			Scenario struct {
				Direction int
				Gap       bool
				GameMode  string `json:"game_mode"`
			}
			Feet    mgl64.Vec3
			Changes []struct {
				Tick         int64
				BeforeSample bool `json:"before_sample"`
				Held         string
			}
			Results []struct{ Source tripwireBreakNativeBlock }
			Samples []struct {
				Tick        int64
				Left, Right tripwireBreakNativeBlock
				Strings     []tripwireBreakNativeBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 16 {
		t.Fatal("incomplete native player hook-placement capture")
	}
	var actual []tripwireNativeSound
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/direction%d/gap%t", c.GameMode, c.Direction, c.Gap), func(t *testing.T) {
			if len(trace.Changes) != 1 || len(trace.Results) != 1 || len(trace.Samples) < 40 || trace.Changes[0].Held != "minecraft:tripwire_hook" || trace.Results[0].Source.ID != "minecraft:tripwire_hook" {
				t.Fatal("missing accepted native hook placement")
			}
			if c.Direction < 0 || c.Direction > 3 {
				t.Fatal("invalid native hook facing")
			}
			dir := [...]cube.Direction{cube.North, cube.East, cube.South, cube.West}[c.Direction]
			step := (cube.Pos{}).Side(dir.Face())
			pos := func(i int) cube.Pos { return cube.Pos{step[0] * i, 65, step[2] * i} }
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			defer w.Close()
			observer := &tripwireNativeSoundObserver{}
			w.Handle(observer)
			if err := w.SetGameRule("block_drops", false); err != nil {
				t.Fatal(err)
			}
			var handle *world.EntityHandle
			applied := 0
			use := func(tx *world.Tx) {
				p, ok := handle.Entity(tx)
				if !ok {
					t.Fatal("missing hook-placement player")
				}
				click := mgl64.Vec3{.5, .5, .5}
				face := dir.Opposite().Face()
				switch face {
				case cube.FaceNorth:
					click[2] = 0
				case cube.FaceSouth:
					click[2] = 1
				case cube.FaceWest:
					click[0] = 0
				case cube.FaceEast:
					click[0] = 1
				}
				p.(*Player).UseItemOnBlock(pos(4), face, click)
				applied++
				compareTripwireBreakNative(t, tx, tx.CurrentTick(), pos(3), trace.Results[0].Source)
			}
			<-w.Exec(func(tx *world.Tx) {
				for x := -8; x <= 8; x++ {
					for z := -8; z <= 8; z++ {
						tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(pos(-2), block.Stone{}, nil)
				tx.SetBlock(pos(4), block.EndStone{}, nil)
				tx.SetBlock(pos(-1), block.TripwireHook{Facing: dir}, nil)
				for i := 0; i < 3; i++ {
					if !c.Gap || i != 1 {
						tx.SetBlock(pos(i), block.Tripwire{}, nil)
					}
				}
				var mode world.GameMode = world.GameModeCreative
				if c.GameMode == "survival" {
					mode = world.GameModeSurvival
				}
				handle = (world.EntitySpawnOpts{Position: trace.Feet}).New(Type, Config{Name: "RedstoneAudit", Position: trace.Feet, GameMode: mode})
				p := tx.AddEntity(handle).(*Player)
				p.SetHeldItems(item.NewStack(block.TripwireHook{}, 2), item.Stack{})
				tx.AddEntity((world.EntitySpawnOpts{Position: trace.Feet}).New(sculkInputType{}, sculkInputTick(func(tx *world.Tx, tick int64) {
					if trace.Changes[0].Tick == tick && trace.Changes[0].BeforeSample {
						use(tx)
					}
				})))
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Fatalf("tick %d, native %d", tx.CurrentTick(), sample.Tick)
					}
					compareTripwireBreakNative(t, tx, sample.Tick, pos(-1), sample.Left)
					compareTripwireBreakNative(t, tx, sample.Tick, pos(3), sample.Right)
					if len(sample.Strings) != 3 {
						t.Fatal("missing native line states")
					}
					for i, state := range sample.Strings {
						compareTripwireBreakNative(t, tx, sample.Tick, pos(i), state)
					}
					if trace.Changes[0].Tick == sample.Tick && !trace.Changes[0].BeforeSample {
						use(tx)
					}
				})
				if t.Failed() {
					break
				}
			}
			if !t.Failed() && applied != 1 {
				t.Errorf("applied %d placements, native 1", applied)
			}
			for _, packet := range observer.packets {
				if packet.SoundType == "attach" {
					actual = append(actual, packet)
				}
			}
		})
	}
	// Each replay has a fresh world; the capture also retains detach packets
	// from clearing the previous native circuit. Compare placement attachments.
	var expected []tripwireNativeSound
	for _, packet := range native.Sounds {
		if packet.SoundType == "attach" {
			expected = append(expected, packet)
		}
	}
	if len(expected) != 16 {
		t.Fatal("missing native attachment packet controls")
	}
	if len(actual) != len(expected) {
		t.Fatalf("placement attachment packets=%d, native=%d", len(actual), len(expected))
	}
	for i, packet := range expected {
		if actual[i] != packet {
			t.Errorf("attachment %d=%+v, native=%+v", i, actual[i], packet)
		}
	}
}
