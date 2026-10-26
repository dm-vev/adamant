package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestLiveLightSourceRemovalMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_live_light.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Filter string
				Phase  int
				Other  bool
			}
			Samples []struct {
				Tick   int64
				Source string
				Other  string
				Values []struct{ Light, Sky int }
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
	if !ref.Complete || len(ref.Scenarios) != 16 {
		t.Fatal("incomplete light capture")
	}
	for _, trace := range ref.Scenarios {
		t.Run(fmt.Sprintf("%s/phase%d/other%v", trace.Scenario.Filter, trace.Scenario.Phase, trace.Scenario.Other), func(t *testing.T) {
			if len(trace.Samples) != 72 || len(trace.Changes) != 5 || trace.Changes[0].Tick&1 != int64(trace.Scenario.Phase) {
				t.Fatal("invalid live light trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			settings.Time = 6000
			settings.TimeCycle = false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				for x := -1; x <= 9; x++ {
					for y := 64; y <= 66; y++ {
						for z := -1; z <= 1; z++ {
							tx.SetBlock(cube.Pos{x, y, z}, Stone{}, nil)
						}
					}
				}
				for x := 0; x <= 8; x++ {
					tx.SetBlock(cube.Pos{x, 65, 0}, Air{}, nil)
				}
				var filter world.Block = Air{}
				switch trace.Scenario.Filter {
				case "glass":
					filter = Glass{}
				case "leaves":
					filter = Leaves{Persistent: true}
				case "stone":
					filter = Stone{}
				case "air":
				default:
					t.Fatal("unknown light filter")
				}
				tx.SetBlock(cube.Pos{1, 65, 0}, filter, nil)
				if trace.Scenario.Other {
					tx.SetBlock(cube.Pos{8, 65, 0}, Glowstone{}, nil)
				}
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick || len(sample.Values) != 9 {
						t.Fatal("invalid light sample")
					}
					id, _ := tx.Block(cube.Pos{0, 65, 0}).EncodeBlock()
					if id != sample.Source {
						t.Errorf("source=%s native=%s", id, sample.Source)
					}
					otherID, _ := tx.Block(cube.Pos{8, 65, 0}).EncodeBlock()
					if otherID != sample.Other {
						t.Errorf("other=%s native=%s", otherID, sample.Other)
					}
					// The SDK getter hides brightness inside the opaque source;
					// compare the independently observed transmitted field.
					for x := 1; x <= 8; x++ {
						if x == 8 && sample.Other == "minecraft:glowstone" {
							continue
						}
						p := cube.Pos{x, 65, 0}
						v := sample.Values[x]
						if got := int(tx.BlockLight(p)); got != v.Light {
							t.Errorf("tick%d x%d light=%d native=%d", sample.Tick, x, got, v.Light)
						}
						if got := int(tx.SkyLight(p)); got != v.Sky {
							t.Errorf("tick%d x%d sky=%d native=%d", sample.Tick, x, got, v.Sky)
						}
					}
					for _, change := range trace.Changes {
						if change.Tick == sample.Tick {
							var b world.Block = Glowstone{}
							if change.Kind == "off" || change.Kind == "all_off" {
								b = Air{}
							}
							tx.SetBlock(cube.Pos{0, 65, 0}, b, nil)
							if change.Kind == "all_off" {
								tx.SetBlock(cube.Pos{8, 65, 0}, Air{}, nil)
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
