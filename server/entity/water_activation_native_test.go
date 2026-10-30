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

func TestWaterActivationMatchesBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_water_activation.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Kind, Update string
				Phase        int
			}
			Base, Source, Trigger cube.Pos
			Accepted              bool
			SetupTick             int64 `json:"setup_tick"`
			Changes               []struct {
				Tick    int64
				Kind    string
				Powered bool
			}
			Samples []struct {
				Tick  int64
				Cells []struct {
					Position cube.Pos
					Block    pistonEntityNativeBlock
				}
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 12 {
		t.Fatal("incomplete native water activation corpus")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/phase%d", c.Kind, c.Update, c.Phase), func(t *testing.T) {
			if !trace.Accepted || len(trace.Samples) != 128 || len(trace.Changes) != 3 {
				t.Fatal("invalid native water controls")
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
				tx.SetBlock(trace.Base.Side(cube.FaceDown).Side(cube.FaceDown), block.Stone{}, nil)
				tx.SetBlock(trace.Base, block.Note{}, nil)
				if c.Update == "remove" {
					tx.SetBlock(trace.Trigger, block.Stone{}, nil)
				}
				tx.SetBlock(trace.Base.Side(cube.FaceUp), block.Water{Still: c.Kind == "water", Depth: 8}, nil)
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native water clock differs")
					}
					for _, cell := range frame.Cells {
						comparePistonEntityNativeBlock(t, tx, frame.Tick, cell.Position, cell.Block)
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							if change.Kind == "neighbour" {
								switch c.Update {
								case "place":
									tx.SetBlock(trace.Trigger, block.Stone{}, nil)
								case "remove":
									tx.SetBlock(trace.Trigger, nil, nil)
								}
							} else {
								var source world.Block
								if change.Powered {
									source = block.RedstoneBlock{}
								}
								tx.SetBlock(trace.Source, source, nil)
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

func TestPhysicalWaterPlacementFlowsInBothLayers(t *testing.T) {
	for _, waterlogged := range []bool{false, true} {
		t.Run(fmt.Sprintf("waterlogged%t", waterlogged), func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
			defer w.Close()
			pos := cube.Pos{8, 65, 8}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos.Side(cube.FaceDown), block.Stone{}, nil)
				if waterlogged {
					tx.SetBlock(pos, block.WoodFence{}, nil)
				}
				// Bucket.UseOnBlock uses WithDepth followed by SetLiquid.
				tx.SetLiquid(pos, (block.Water{Still: true, Depth: 8}).WithDepth(8, false))
			})
			for range 8 {
				w.AdvanceTick()
			}
			<-w.Exec(func(tx *world.Tx) {
				if waterlogged {
					if _, ok := tx.Block(pos).(block.WoodFence); !ok {
						t.Fatal("water displaced the waterlogged fence")
					}
				}
				for _, face := range []cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
					liquid, ok := tx.Liquid(pos.Side(face))
					if !ok || liquid.LiquidType() != "water" || liquid.LiquidDepth() != 7 || liquid.LiquidFalling() {
						t.Fatalf("physical placement did not flow through %v: %v", face, liquid)
					}
				}
			})
		})
	}
}
