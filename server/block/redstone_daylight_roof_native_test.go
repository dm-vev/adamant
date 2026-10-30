package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestDaylightLiveRoofsMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_daylight_roofs.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				X, Phase int
				Cover    string
			}
			Samples []struct {
				Tick   int64
				Time   int
				Values []struct {
					Source, Wire torchNetworkBlock
					Sky          int
				}
			}
			Changes []struct {
				Tick int64
				Kind string
			}
		}
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	if !ref.Complete || len(ref.Scenarios) != 24 {
		t.Fatal("incomplete live roof capture")
	}
	for _, trace := range ref.Scenarios {
		t.Run(fmt.Sprintf("x%d/%s/phase%d", trace.Scenario.X, trace.Scenario.Cover, trace.Scenario.Phase), func(t *testing.T) {
			if len(trace.Samples) != 80 || len(trace.Changes) != 3 || trace.Changes[0].Tick&1 != int64(trace.Scenario.Phase) {
				t.Fatal("invalid live roof trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			settings.Time = 6000
			settings.TimeCycle = false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			x := trace.Scenario.X
			<-w.Exec(func(tx *world.Tx) {
				for dx := x - 5; dx <= x+5; dx++ {
					for z := -5; z <= 13; z++ {
						tx.SetBlock(cube.Pos{dx, 64, z}, Stone{}, nil)
					}
				}
				for _, z := range []int{0, 8} {
					tx.SetBlock(cube.Pos{x, 65, z}, DaylightDetector{Inverted: z == 8}, nil)
					tx.SetBlock(cube.Pos{x + 1, 65, z}, RedstoneWire{}, nil)
				}
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick || sample.Time != 6000 || len(sample.Values) != 2 {
						t.Fatal("live roof sample clock drift")
					}
					for i, z := range []int{0, 8} {
						pos := cube.Pos{x, 65, z}
						v := sample.Values[i]
						compareTorchNetworkBlock(t, tx, sample.Tick, pos, v.Source)
						if got := tx.Block(pos).(DaylightDetector).Signal; got != v.Source.States.Signal {
							t.Errorf("tick%d %v detector=%d native=%d", sample.Tick, pos, got, v.Source.States.Signal)
						}
						compareTorchNetworkBlock(t, tx, sample.Tick, pos.Side(cube.FaceEast), v.Wire)
						if got := int(tx.SkyLight(pos)); got != v.Sky {
							t.Errorf("tick%d %v sky=%d native=%d", sample.Tick, pos, got, v.Sky)
						}
					}
					for _, change := range trace.Changes {
						if change.Tick != sample.Tick {
							continue
						}
						var roof world.Block = Air{}
						if change.Kind == "cover" {
							switch trace.Scenario.Cover {
							case "stone":
								roof = Stone{}
							case "glass":
								roof = Glass{}
							case "leaves":
								roof = Leaves{Persistent: true}
							default:
								t.Fatal("unknown native roof")
							}
						}
						for _, z := range []int{0, 8} {
							for dx := x - 2; dx <= x+2; dx++ {
								for dz := z - 2; dz <= z+2; dz++ {
									tx.SetBlock(cube.Pos{dx, 68, dz}, roof, nil)
								}
							}
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
