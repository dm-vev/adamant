package block

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Replay all native frames, including the initial oscillator phase, burnout,
// independent input, neighbour recovery and loop removal/reconnection.
func TestTorchWorldTimingMatchesNativeCapture(t *testing.T) {
	t.Run("initial-capture", func(t *testing.T) { testTorchWorldCapture(t, "bedrock_1_26_51_torch.json", 6, 300) })
	t.Run("recovery-matrix", func(t *testing.T) { testTorchWorldCapture(t, "bedrock_1_26_51_torch_recovery.json", 18, 180) })
	t.Run("reignition-boundary", func(t *testing.T) { testTorchWorldCapture(t, "bedrock_1_26_51_torch_reignite.json", 10, 180) })
}

func testTorchWorldCapture(t *testing.T, filename string, cases, samples int) {
	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Name  string
				Phase int
			}
			Samples []struct {
				Tick                           int64
				Torch, Top, Side, Input, Extra struct {
					ID     string
					States struct {
						Signal int `json:"redstone_signal"`
					}
				}
			}
			Changes []struct {
				Tick  int64
				Pos   cube.Pos
				Block string
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != cases {
		t.Fatal("incomplete native torch capture")
	}
	for _, trace := range reference.Scenarios {
		external := strings.HasPrefix(trace.Scenario.Name, "strong_")
		loop := !external && trace.Scenario.Name != "isolated" && !strings.HasPrefix(trace.Scenario.Name, "external_")
		t.Run(fmt.Sprintf("%s/phase%d", trace.Scenario.Name, trace.Scenario.Phase), func(t *testing.T) {
			if len(trace.Samples) != samples || len(trace.Changes) == 0 {
				t.Fatal("invalid native torch trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 64, 0}, Stone{}, nil)
				tx.SetBlock(cube.Pos{1, 63, 0}, Stone{}, nil)
				if external {
					tx.SetBlock(cube.Pos{-1, 63, 0}, Stone{}, nil)
					tx.SetBlock(cube.Pos{-1, 64, 0}, RedstoneRepeater{Facing: cube.West}, nil)
				} else if loop {
					tx.SetBlock(cube.Pos{1, 65, 0}, Stone{}, nil)
					tx.SetBlock(cube.Pos{1, 66, 0}, RedstoneWire{}, nil)
					tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneWire{}, nil)
				}
				if trace.Scenario.Name == "wire_corner" {
					tx.SetBlock(cube.Pos{2, 63, 0}, Stone{}, nil)
					tx.SetBlock(cube.Pos{2, 64, 0}, RedstoneWire{}, nil)
				}
				tx.SetBlock(cube.Pos{1, 64, 0}, RedstoneTorch{Facing: cube.FaceWest, Lit: true}, nil)
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Errorf("world tick %d, native sample tick %d", tx.CurrentTick(), sample.Tick)
					}
					for _, test := range []struct {
						Pos   cube.Pos
						ID    string
						Power int
					}{
						{cube.Pos{1, 64, 0}, sample.Torch.ID, 0},
						{cube.Pos{1, 66, 0}, sample.Top.ID, sample.Top.States.Signal},
						{cube.Pos{0, 65, 0}, sample.Side.ID, sample.Side.States.Signal},
						{cube.Pos{-1, 64, 0}, sample.Input.ID, sample.Input.States.Signal},
						{cube.Pos{2, 64, 0}, sample.Extra.ID, sample.Extra.States.Signal},
					} {
						if test.ID == "" {
							continue
						}
						b := tx.Block(test.Pos)
						id, _ := b.EncodeBlock()
						if id != test.ID {
							t.Errorf("tick %d at %v: %s, native %s", sample.Tick, test.Pos, id, test.ID)
						}
						if wire, ok := b.(RedstoneWire); ok && wire.Power != test.Power {
							t.Errorf("tick %d at %v: power %d, native %d", sample.Tick, test.Pos, wire.Power, test.Power)
						}
					}
					for _, change := range trace.Changes {
						if change.Tick != sample.Tick || change.Block == "" {
							continue
						}
						var b world.Block = Air{}
						if change.Block == "redstone_block" {
							b = RedstoneBlock{}
						} else if change.Block == "redstone_wire" {
							b = RedstoneWire{}
						} else if change.Block == "stone" {
							b = Stone{}
						} else if change.Block != "air" {
							t.Errorf("unexpected change %s", change.Block)
						}
						// BDS /setblock on an unchanged permutation is a no-op.
						if world.BlockHash(tx.Block(change.Pos)) != world.BlockHash(b) {
							tx.SetBlock(change.Pos, b, nil)
						}
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}

func TestTorchLegacyCallbacksUseLiveCircuitState(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{1, 64, 0}
	stale := RedstoneTorch{Facing: cube.FaceNorth, Lit: true}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceWest), Stone{}, nil)
		tx.SetBlock(pos, RedstoneTorch{Facing: cube.FaceWest}, nil)
		stale.ScheduledTick(pos, tx, nil)
		live, ok := tx.Block(pos).(RedstoneTorch)
		if !ok || live.Facing != cube.FaceWest || live.Lit {
			t.Errorf("stale callback changed live torch immediately: %+v", live)
		}
		if tx.HasScheduledBlockUpdate(pos, live) {
			t.Error("legacy callback started a private inversion clock")
		}
	})
	for range 3 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		live, ok := tx.Block(pos).(RedstoneTorch)
		if !ok || live.Facing != cube.FaceWest || !live.Lit {
			t.Errorf("live capacitor did not publish its own orientation: %+v", live)
		}
		tx.SetBlock(pos, Stone{}, nil)
		stale.ScheduledTick(pos, tx, nil)
	})
	for range 3 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(pos).(Stone); !ok {
			t.Errorf("stale callback restored removed torch: %T", tx.Block(pos))
		}
	})
}
