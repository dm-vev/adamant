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

func TestContainerLoadedFlagsMatchBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_container_load_flags.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Kind          string
				Stored, Input bool
				Phase         int
			}
			Base, Source cube.Pos
			SetupTick    int64 `json:"setup_tick"`
			Load         struct {
				Tick  int64
				Block pistonEntityNativeBlock
			}
			Changes []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct {
				Tick  int64
				Block pistonEntityNativeBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 32 {
		t.Fatal("incomplete native container flag corpus")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/stored%t/input%t/phase%d", c.Kind, c.Stored, c.Input, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 72 || len(trace.Changes) != 2 || trace.SetupTick%20 != 2 || trace.Load.Tick != trace.Changes[0].Tick || trace.Load.Tick%2 != int64(c.Phase) || trace.Changes[0].Powered != c.Input {
				t.Fatal("invalid native initial consumer controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			settings.WeatherCycle = false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			<-w.Exec(func(tx *world.Tx) { loader.Move(tx, trace.Base.Vec3Centre()); loader.Load(tx, 25) })
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native container load clock differs")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Base, frame.Block)
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							var source world.Block
							if change.Powered {
								source = block.RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, source, nil)
						}
					}
					if frame.Tick == trace.Load.Tick {
						var b world.Block
						switch c.Kind {
						case "crafter":
							v := block.NewCrafter()
							v.Triggered = c.Stored
							b = v
						case "dispenser":
							v := block.NewDispenser()
							v.Facing = cube.FaceNorth
							v.Triggered = c.Stored
							b = v
						case "dropper":
							v := block.NewDropper()
							v.Facing = cube.FaceNorth
							v.Triggered = c.Stored
							b = v
						case "hopper":
							v := block.NewHopper()
							v.Facing = cube.FaceNorth
							v.Powered = c.Stored
							b = v
						default:
							t.Fatal("unknown native container kind")
						}
						tx.SetBlock(trace.Base, b, nil)
						comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Base, trace.Load.Block)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
