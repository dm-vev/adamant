package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestObserverMatchesNativeCapture(t *testing.T) {
	testObserverNativeTrace(t, "testdata/bedrock_1_26_51_observer.json", 38)
}

func TestObserverOutputMatchesNativeCapture(t *testing.T) {
	testObserverNativeTrace(t, "testdata/bedrock_1_26_51_observer_power.json", 28)
}

func TestObserverLifecycleMatchesNativeCapture(t *testing.T) {
	testObserverNativeTrace(t, "testdata/bedrock_1_26_51_observer_lifecycle.json", 12)
}

func testObserverNativeTrace(t *testing.T, fixture string, count int) {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool `json:"complete"`
		Scenarios []struct {
			Scenario struct {
				Name, Facing, Changed, Output string
				Phase, Gap                    int
			} `json:"scenario"`
			Samples []struct {
				Tick     int64
				Observer struct {
					ID     string
					States struct {
						Powered bool `json:"powered_bit"`
					}
				}
				Output struct {
					ID     string
					States struct {
						Signal int `json:"redstone_signal"`
					}
				}
			} `json:"samples"`
			Changes []struct {
				Tick   int64
				Pos    cube.Pos
				Block  string
				Facing string
			} `json:"changes"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != count {
		t.Fatal("incomplete native observer capture")
	}
	faces := map[string]cube.Face{"down": cube.FaceDown, "up": cube.FaceUp, "north": cube.FaceNorth, "south": cube.FaceSouth, "west": cube.FaceWest, "east": cube.FaceEast}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/%s/%s/gap%d/phase%d", c.Name, c.Output, c.Facing, c.Changed, c.Gap, c.Phase), func(t *testing.T) {
			if len(trace.Samples) == 0 || len(trace.Changes) < 2 {
				t.Fatal("invalid native trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			pos := cube.Pos{0, 65, 0}
			outputPos := pos.Side(faces[c.Facing].Opposite())
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos, Observer{Facing: faces[c.Facing]}, nil)
				if c.Output != "" {
					if c.Output != "lamp" {
						tx.SetBlock(outputPos.Side(cube.FaceDown), Stone{}, nil)
					}
					var output world.Block
					switch c.Output {
					case "lamp":
						output = RedstoneLamp{}
					case "wire":
						output = RedstoneWire{}
					case "repeater":
						output = RedstoneRepeater{Facing: faces[c.Facing].Direction()}
					default:
						t.Errorf("unsupported native output %s", c.Output)
					}
					tx.SetBlock(outputPos, output, nil)
				}
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Errorf("replay clock %d, native %d", tx.CurrentTick(), sample.Tick)
					}
					o, ok := tx.Block(pos).(Observer)
					id, _ := tx.Block(pos).EncodeBlock()
					if id != sample.Observer.ID || (sample.Observer.ID == "minecraft:observer" && (!ok || o.Powered != sample.Observer.States.Powered)) {
						t.Errorf("tick %d: powered=%v, native=%v", sample.Tick, o.Powered, sample.Observer.States.Powered)
					}
					if c.Output != "" {
						b := tx.Block(outputPos)
						id, _ := b.EncodeBlock()
						if id != sample.Output.ID {
							t.Errorf("tick %d: output %s, native %s", sample.Tick, id, sample.Output.ID)
						}
						if c.Output == "wire" {
							if power := b.(RedstoneWire).Power; power != sample.Output.States.Signal {
								t.Errorf("tick %d: wire power=%d, native=%d", sample.Tick, power, sample.Output.States.Signal)
							}
						}
					}
					for _, change := range trace.Changes {
						if change.Tick != sample.Tick {
							continue
						}
						var block world.Block = Air{}
						if change.Block == "stone" {
							block = Stone{}
						} else if change.Block == "observer" {
							block = Observer{Facing: faces[change.Facing]}
						} else if change.Block != "air" {
							t.Errorf("unsupported native block %s", change.Block)
						}
						tx.SetBlock(change.Pos, block, nil)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}

// TestObserverAfterPistonMoveMatchesNativeCapture aligns the movement-complete
// callback with each captured native arrival. It checks the observer's pulse,
// independently of piston start/progress timing (audited separately).
func TestObserverAfterPistonMoveMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_observer_move.json")
	if err != nil {
		t.Fatal(err)
	}
	type nativeObserver struct {
		ID     string
		States struct {
			Powered bool `json:"powered_bit"`
		}
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Facing string
				Sticky bool
			}
			Samples []struct {
				Tick                int64
				Source, Destination nativeObserver
			}
			Changes []struct{ Tick int64 }
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 12 {
		t.Fatal("incomplete observer movement capture")
	}
	faces := map[string]cube.Face{"down": cube.FaceDown, "up": cube.FaceUp, "north": cube.FaceNorth, "south": cube.FaceSouth, "west": cube.FaceWest, "east": cube.FaceEast}
	for _, trace := range reference.Scenarios {
		for _, pulling := range []bool{false, true} {
			if pulling && !trace.Scenario.Sticky {
				continue
			}
			t.Run(fmt.Sprintf("%s/sticky%v/pull%v", trace.Scenario.Facing, trace.Scenario.Sticky, pulling), func(t *testing.T) {
				if len(trace.Changes) != 2 {
					t.Fatal("invalid native move changes")
				}
				arrival := -1
				for i, sample := range trace.Samples {
					observed := sample.Destination
					after := trace.Changes[0].Tick
					if pulling {
						observed = sample.Source
						after = trace.Changes[1].Tick
					}
					if sample.Tick > after && observed.ID == "minecraft:observer" {
						arrival = i
						break
					}
				}
				if arrival < 0 {
					t.Fatal("no native observer arrival")
				}
				settings := (world.NopProvider{}).Settings()
				settings.CurrentTick = trace.Samples[arrival].Tick - 1
				w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
				t.Cleanup(func() { _ = w.Close() })
				pos := cube.Pos{0, 65, 0}
				base := cube.Pos{-1, 65, 0}
				if pulling {
					pos, base = base, pos
				}
				<-w.Exec(func(tx *world.Tx) {
					p := Piston{Facing: cube.FaceEast, Sticky: trace.Scenario.Sticky, Extending: !pulling, Attached: []cube.Pos{base}}
					if !pulling {
						tx.SetBlock(cube.Pos{-3, 65, 0}, RedstoneBlock{}, nil)
					}
					tx.SetBlock(cube.Pos{-2, 65, 0}, p, nil)
					tx.SetBlock(pos, MovingBlock{Moving: Observer{Facing: faces[trace.Scenario.Facing]}}, nil)
					p.finishMove(cube.Pos{-2, 65, 0}, tx)
				})
				for i := arrival; i < len(trace.Samples) && i < arrival+4; i++ {
					if i > arrival {
						w.AdvanceTick()
					}
					sample := trace.Samples[i]
					expected := sample.Destination
					if pulling {
						expected = sample.Source
					}
					<-w.Exec(func(tx *world.Tx) {
						o, ok := tx.Block(pos).(Observer)
						if !ok || o.Powered != expected.States.Powered {
							t.Errorf("arrival +%d: powered=%v, native=%v", sample.Tick-trace.Samples[arrival].Tick, o.Powered, expected.States.Powered)
						}
					})
					if t.Failed() {
						break
					}
				}
			})
		}
	}
}
