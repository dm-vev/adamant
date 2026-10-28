package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestRailPlacementAndPowerDoNotLoadNeighbourChunks(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{15, 65, 0}
		tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil)
		tx.SetBlock(pos, PoweredRail{Direction: RailEastWest}, nil)
		state := tx.Block(pos).(PoweredRail).RedstoneCircuit().(railCircuit)
		state.CacheAfterSources(pos, tx)
		for _, outside := range []cube.Pos{{16, 65, 0}, {15, 65, -1}} {
			if _, loaded := tx.BlockLoaded(outside); loaded {
				t.Errorf("rail query loaded neighbouring chunk at %v", outside)
			}
		}
	})
}

func TestRailCurvePlacementMatchesBDS(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_rail_curve_placement.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Mode string
				Corner      RailDirection
			}
			SetupTick      int64 `json:"setup_tick"`
			Center, Target cube.Pos
			Arms           []cube.Pos
			Changes        []struct {
				Tick int64
				Kind string
			}
			Samples []struct {
				Tick   int64
				Blocks []nativeTripwireBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 16 {
		t.Fatal("incomplete native curve placement capture")
	}
	for _, trace := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/corner%d/%s", trace.Scenario.Block, trace.Scenario.Corner, trace.Scenario.Mode), func(t *testing.T) {
			if len(trace.Samples) != 64 || len(trace.Arms) != 2 || len(trace.Changes) != 1 || trace.Changes[0].Tick&1 != 0 {
				t.Fatal("invalid curve placement trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Center.Vec3Centre())
				loader.Load(tx, 25)
				for x := trace.Center[0] - 3; x <= trace.Center[0]+3; x++ {
					for z := trace.Center[2] - 3; z <= trace.Center[2]+3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				tx.SetBlock(trace.Arms[0], Rail{Direction: RailNorthSouth}, nil)
				initial := trace.Scenario.Corner
				if trace.Scenario.Mode == "add_corner" {
					initial = RailNorthSouth
				} else {
					tx.SetBlock(trace.Arms[1], Rail{Direction: RailEastWest}, nil)
				}
				tx.SetBlock(trace.Center, Rail{Direction: initial}, nil)
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick || len(frame.Blocks) != 4 {
						t.Fatal("curve replay clock/cell mismatch")
					}
					positions := []cube.Pos{trace.Center, trace.Arms[0], trace.Arms[1], trace.Target}
					for i, p := range positions {
						compareSculkContactNativeBlock(t, tx, frame.Tick, p, frame.Blocks[i])
					}
					if frame.Tick == trace.Changes[0].Tick {
						dir := RailNorthSouth
						if trace.Scenario.Mode == "add_corner" {
							dir = RailEastWest
						}
						switch trace.Scenario.Block {
						case "golden_rail":
							tx.SetBlock(trace.Target, PoweredRail{Direction: dir}, nil)
						case "activator_rail":
							tx.SetBlock(trace.Target, ActivatorRail{Direction: dir}, nil)
						default:
							t.Fatal("unhandled rail kind")
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
