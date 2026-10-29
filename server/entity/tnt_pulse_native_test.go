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

func TestTNTPulsesMatchBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_tnt_pulses.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Face, Phase, Width int
				Conducted          bool
			}
			Base, Support, Source cube.Pos
			SetupTick             int64 `json:"setup_tick"`
			Changes               []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct {
				Tick          int64
				Block, Source pistonEntityNativeBlock
				TNT           []struct {
					ActorID string `json:"actor_id"`
				}
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 72 {
		t.Fatal("incomplete native TNT pulse corpus")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("face%d/conducted%t/phase%d/width%d", c.Face, c.Conducted, c.Phase, c.Width), func(t *testing.T) {
			if len(trace.Samples) != 72 || len(trace.Changes) != 2 || trace.SetupTick%20 != 2 || trace.Changes[0].Tick%2 != int64(c.Phase) || trace.Changes[1].Tick-trace.Changes[0].Tick != int64(c.Width) {
				t.Fatal("invalid native TNT pulse controls")
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
				for x := -4; x <= 4; x++ {
					for z := -4; z <= 4; z++ {
						tx.SetBlock(cube.Pos{trace.Base[0] + x, 63, trace.Base[2] + z}, block.Bedrock{}, nil)
						tx.SetBlock(cube.Pos{trace.Base[0] + x, 69, trace.Base[2] + z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(trace.Base, block.TNT{}, nil)
				if c.Conducted {
					tx.SetBlock(trace.Support, block.Stone{}, nil)
				}
			})
			var handle *world.EntityHandle
			nativeID := ""
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native TNT pulse clock differs")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Base, frame.Block)
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Source, frame.Source)
					var actors []world.Entity
					for e := range tx.Entities() {
						if e.H().Type() == TNTType {
							actors = append(actors, e)
						}
					}
					if len(actors) != len(frame.TNT) || len(actors) > 1 {
						t.Fatalf("tick %d primed TNT actors=%d, native=%d", frame.Tick, len(actors), len(frame.TNT))
					}
					if len(actors) == 1 {
						if handle != nil && (handle != actors[0].H() || nativeID != frame.TNT[0].ActorID) {
							t.Fatal("primed TNT identity changed")
						}
						handle, nativeID = actors[0].H(), frame.TNT[0].ActorID
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							var source world.Block
							if c.Conducted {
								tx.SetBlock(trace.Source, nil, nil)
								source = block.Button{Type: block.StoneButton(), Facing: cube.Face(c.Face), Pressed: change.Powered}
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
