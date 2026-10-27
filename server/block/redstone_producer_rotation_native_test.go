package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestProducerRotationMatchesBDS(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_producer_rotation.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block     string
				From, To  int
				Replace   bool
				EdgePhase int `json:"edge_phase"`
			}
			Source     cube.Pos
			OldSupport cube.Pos `json:"old_support"`
			NewSupport cube.Pos `json:"new_support"`
			OldOutput  cube.Pos `json:"old_output"`
			NewOutput  cube.Pos `json:"new_output"`
			Changes    []struct {
				Tick int64
				Kind string
			}
			Samples []struct {
				Tick       int64
				Source     nativeTripwireBlock
				OldSupport nativeTripwireBlock `json:"old_support"`
				NewSupport nativeTripwireBlock `json:"new_support"`
				OldOutput  nativeTripwireBlock `json:"old_output"`
				NewOutput  nativeTripwireBlock `json:"new_output"`
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 216 {
		t.Fatal("incomplete native producer rotation capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%d-%d/replace%t/phase%d", c.Block, c.From, c.To, c.Replace, c.EdgePhase), func(t *testing.T) {
			if len(trace.Samples) != 90 || len(trace.Changes) != 3 || trace.Changes[1].Tick&1 != int64(c.EdgePhase) {
				t.Fatal("missing native producer edits or parity")
			}
			if trace.Samples[29].OldOutput.ID != "minecraft:lit_redstone_lamp" {
				t.Fatal("native source did not power its support before rotation")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			defer w.Close()
			if err := w.SetGameRule("block_drops", false); err != nil {
				t.Fatal(err)
			}
			producer := func(facing int) world.Block {
				switch c.Block {
				case "stone_button":
					return Button{Type: StoneButton(), Facing: cube.Face(facing), Pressed: true}
				case "wooden_button":
					return Button{Type: WoodButton(), Facing: cube.Face(facing), Pressed: true}
				case "lightning_rod":
					return LightningRod{Facing: cube.Face(facing), Powered: true}
				default:
					t.Fatalf("unknown native producer %s", c.Block)
					return nil
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(trace.OldSupport, Stone{}, nil)
				tx.SetBlock(trace.NewSupport, Stone{}, nil)
				tx.SetBlock(trace.OldOutput, RedstoneLamp{}, nil)
				tx.SetBlock(trace.NewOutput, RedstoneLamp{}, nil)
				tx.SetBlock(trace.Source, producer(c.From), nil)
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Fatalf("tick %d, native %d", tx.CurrentTick(), sample.Tick)
					}
					for _, observed := range []struct {
						pos   cube.Pos
						block nativeTripwireBlock
					}{
						{trace.Source, sample.Source}, {trace.OldSupport, sample.OldSupport}, {trace.NewSupport, sample.NewSupport},
						{trace.OldOutput, sample.OldOutput}, {trace.NewOutput, sample.NewOutput},
					} {
						compareSculkContactNativeBlock(t, tx, sample.Tick, observed.pos, observed.block)
					}
					for _, change := range trace.Changes {
						if change.Tick != sample.Tick {
							continue
						}
						if change.Kind == "reset" || c.Replace {
							tx.SetBlock(trace.Source, nil, nil)
						}
						facing := c.To
						if change.Kind == "reset" {
							facing = c.From
						}
						tx.SetBlock(trace.Source, producer(facing), nil)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
