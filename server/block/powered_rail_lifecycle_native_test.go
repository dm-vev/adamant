package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestPoweredRailLifecycleMatchesBDS(t *testing.T) {
	testPoweredRailLifecycleNative(t, "bedrock_1_26_51_powered_rail_lifecycle.json", 32)
}

func TestPoweredRailPublicationEditsMatchBDS(t *testing.T) {
	testPoweredRailLifecycleNative(t, "bedrock_1_26_51_rail_publication_edits.json", 32)
}

func testPoweredRailLifecycleNative(t *testing.T, name string, count int) {
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Mode string
				Powered     bool
				Phase       int
				Flag        *bool
				EditOffset  *int `json:"edit_offset"`
			}
			SetupTick int64 `json:"setup_tick"`
			Source    cube.Pos
			Positions []cube.Pos
			Changes   []struct {
				Tick    int64
				Kind    string
				Powered bool
			}
			Samples []struct {
				Tick   int64
				Source nativeTripwireBlock
				Rails  []nativeTripwireBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native rail lifecycle capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		caseName := fmt.Sprintf("%s/%s/powered%t/phase%d", c.Block, c.Mode, c.Powered, c.Phase)
		if c.EditOffset != nil {
			if c.Flag == nil {
				t.Fatal("missing publication flag")
			}
			caseName += fmt.Sprintf("/edit+%d/flag%t", *c.EditOffset, *c.Flag)
		}
		t.Run(caseName, func(t *testing.T) {
			if len(trace.Samples) != 132 || len(trace.Changes) != 6 || len(trace.Positions) != 3 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid rail lifecycle trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			place := func(tx *world.Tx, p cube.Pos, powered bool, dir RailDirection) {
				switch c.Block {
				case "golden_rail":
					tx.SetBlock(p, PoweredRail{Powered: powered, Direction: dir}, nil)
				case "activator_rail":
					tx.SetBlock(p, ActivatorRail{Powered: powered, Direction: dir}, nil)
				default:
					t.Fatal("unhandled rail family", c.Block)
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				base := trace.Positions[0]
				loader.Move(tx, base.Vec3Centre())
				loader.Load(tx, 25)
				for x := base[0] - 3; x <= base[0]+6; x++ {
					for z := base[2] - 3; z <= base[2]+3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				for _, p := range trace.Positions {
					place(tx, p, c.Powered, RailEastWest)
				}
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick || len(frame.Rails) != 3 {
						t.Fatal("rail lifecycle replay clock/cell mismatch")
					}
					compareSculkContactNativeBlock(t, tx, frame.Tick, trace.Source, frame.Source)
					for i, p := range trace.Positions {
						compareSculkContactNativeBlock(t, tx, frame.Tick, p, frame.Rails[i])
					}
					target := trace.Positions[1]
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							switch change.Kind {
							case "power":
								var source world.Block = Air{}
								if change.Powered {
									source = RedstoneBlock{}
								}
								tx.SetBlock(trace.Source, source, nil)
							case "reset":
								tx.SetBlock(target, Air{}, nil)
								place(tx, target, false, RailEastWest)
							case "edit":
								switch c.Mode {
								case "flag":
									powered := false
									if c.Flag != nil {
										powered = *c.Flag
									}
									place(tx, target, powered, RailEastWest)
								case "axis":
									place(tx, target, true, RailNorthSouth)
								case "replace":
									tx.SetBlock(target, Air{}, nil)
									place(tx, target, false, RailNorthSouth)
								case "remove":
									tx.SetBlock(target, Air{}, nil)
								default:
									t.Fatal("unexpected lifecycle edit", c.Mode)
								}
							default:
								t.Fatal("unexpected lifecycle change", change.Kind)
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
