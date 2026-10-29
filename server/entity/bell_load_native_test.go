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

type nativeBellSavedActor struct {
	ID               string
	Ticks, Direction int32
	Ringing          uint8
}

func compareNativeBellSavedActor(t *testing.T, tx *world.Tx, tick int64, pos cube.Pos, want nativeBellSavedActor) {
	t.Helper()
	bell, ok := tx.Block(pos).(block.Bell)
	if !ok {
		t.Fatal("native bell actor disappeared")
	}
	got := bell.EncodeNBT()
	if got["id"] != want.ID || got["Ticks"] != want.Ticks || got["Direction"] != want.Direction || got["Ringing"] != want.Ringing {
		t.Fatalf("tick %d at %v bell actor=%v, native=%+v", tick, pos, got, want)
	}
}

func TestBellSavedAnimationMatchesBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_bell_loads.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Age, Phase   int
				InitialPower bool `json:"initial_power"`
			}
			Base, Source, Clone cube.Pos
			CloneSource         cube.Pos `json:"clone_source"`
			SetupTick           int64    `json:"setup_tick"`
			FirstRing           int64    `json:"first_ring"`
			Load                struct {
				Tick         int64
				Block, Clone pistonEntityNativeBlock
				Actor        nativeBellSavedActor
			}
			Changes []struct {
				Tick    int64
				Powered bool
				Target  string
			}
			Samples []struct {
				Tick                 int64
				Block, Source, Clone pistonEntityNativeBlock
				CloneSource          pistonEntityNativeBlock `json:"clone_source"`
				Actor                *nativeBellSavedActor
			}
		}
		Readback struct{ Snapshots int } `json:"actor_readback"`
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 16 || native.Readback.Snapshots != 1656 {
		t.Fatal("incomplete native bell save/load corpus")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("age%d/initial%t/phase%d", c.Age, c.InitialPower, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 160 || len(trace.Changes) != 6 || trace.Load.Tick != trace.FirstRing+int64(c.Age) || trace.Load.Actor.ID != "Bell" || trace.Changes[0].Tick%2 != int64(c.Phase) {
				t.Fatal("native bell save/load controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = trace.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				tx.SetBlock(trace.Base.Side(cube.FaceDown), block.Stone{}, nil)
				tx.SetBlock(trace.Clone.Side(cube.FaceDown), block.Stone{}, nil)
				b, ok := tx.World().BlockRegistry().BlockByName("minecraft:bell", map[string]any{"attachment": "standing", "direction": int32(0), "toggle_bit": uint8(0)})
				if !ok {
					t.Fatal("native bell permutation missing")
				}
				tx.SetBlock(trace.Base, b, nil)
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native bell load clock differs")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Base, frame.Block)
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Source, frame.Source)
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Clone, frame.Clone)
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.CloneSource, frame.CloneSource)
					if frame.Actor != nil {
						compareNativeBellSavedActor(t, tx, frame.Tick, trace.Clone, *frame.Actor)
					} else if frame.Tick > trace.Load.Tick {
						t.Fatal("missing native loaded bell actor")
					}
					if frame.Tick == trace.Load.Tick {
						compareNativeBellSavedActor(t, tx, frame.Tick, trace.Base, trace.Load.Actor)
						live := tx.Block(trace.Base).(block.Bell)
						name, states := live.EncodeBlock()
						prototype, ok := tx.World().BlockRegistry().BlockByName(name, states)
						if !ok {
							t.Fatal("saved bell permutation missing")
						}
						restored := prototype.(block.Bell).DecodeNBT(live.EncodeNBT()).(block.Bell)
						if c.InitialPower {
							tx.SetBlock(trace.CloneSource, block.RedstoneBlock{}, nil)
						}
						tx.SetBlock(trace.Clone, restored, nil)
						comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Clone, trace.Load.Clone)
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							pos := trace.Source
							if change.Target == "clone" {
								pos = trace.CloneSource
							}
							var power world.Block
							if change.Powered {
								power = block.RedstoneBlock{}
							}
							tx.SetBlock(pos, power, nil)
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
