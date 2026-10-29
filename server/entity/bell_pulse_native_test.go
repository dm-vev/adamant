package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestBellRedstoneActorsMatchBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_bell_pulses.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Direction, Phase int
				Conducted        bool
				Edges            []int
			}
			Base, Source, Support cube.Pos
			SetupTick             int64 `json:"setup_tick"`
			Changes               []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct {
				Tick          int64
				Block, Source pistonEntityNativeBlock
				Actor         struct {
					ID               string
					Ticks, Direction int32
					Ringing          uint8
				}
			}
		}
		Readback struct{ Snapshots int } `json:"actor_readback"`
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 64 || native.Readback.Snapshots != 7168 {
		t.Fatal("incomplete native bell corpus")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("direction%d/conducted%t/phase%d/%v", c.Direction, c.Conducted, c.Phase, c.Edges), func(t *testing.T) {
			if len(trace.Samples) != 112 || len(trace.Changes) != len(c.Edges) || trace.SetupTick%20 != 2 || trace.Changes[0].Tick%2 != int64(c.Phase) {
				t.Fatal("invalid native bell source controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			settings.WeatherCycle = false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				tx.SetBlock(trace.Base.Side(cube.FaceDown), block.Stone{}, nil)
				bell, ok := tx.World().BlockRegistry().BlockByName("minecraft:bell", map[string]any{"attachment": "standing", "direction": int32(c.Direction), "toggle_bit": uint8(0)})
				if !ok {
					t.Fatal("native bell permutation missing")
				}
				tx.SetBlock(trace.Base, bell, nil)
				if c.Conducted {
					tx.SetBlock(trace.Support, block.Stone{}, nil)
				}
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native bell clock differs")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Base, frame.Block)
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Source, frame.Source)
					bell, ok := tx.Block(trace.Base).(block.Bell)
					if !ok {
						t.Fatal("bell actor removed")
					}
					actor := bell.EncodeNBT()
					if actor["id"] != frame.Actor.ID || actor["Ticks"] != frame.Actor.Ticks || actor["Direction"] != frame.Actor.Direction || actor["Ringing"] != frame.Actor.Ringing {
						t.Fatalf("tick %d bell actor=%v, native=%+v", frame.Tick, actor, frame.Actor)
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							var source world.Block
							if c.Conducted {
								tx.SetBlock(trace.Source, nil, nil)
								source = block.Button{Type: block.StoneButton(), Facing: cube.FaceWest, Pressed: change.Powered}
							} else if change.Powered {
								source = block.RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, source, nil)
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
