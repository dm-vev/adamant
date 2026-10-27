package block

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestPistonInterruptionMatchesBDS(t *testing.T) {
	testPistonInterruptionNative(t, "bedrock_1_26_51_piston_interruption.json")
}

func TestPistonHeadRemovalMatchesBDS(t *testing.T) {
	testPistonInterruptionNative(t, "bedrock_1_26_51_piston_head_removal.json")
}

func testPistonInterruptionNative(t *testing.T, file string) {
	data, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Sticky                       bool
				Facing, Phase, Offset        int
				Motion, Payload, Replacement string
			}
			Base, Source cube.Pos
			Front        []cube.Pos
			SetupTick    int64 `json:"setup_tick"`
			Changes      []struct {
				Tick    int64
				Powered bool
			}
			Interruption struct {
				Tick     int64
				Position *cube.Pos
				After    struct {
					Base  nativeTripwireBlock
					Actor *nativePistonActor
					Front []nativePistonBlock
				}
			}
			Samples []struct {
				Tick         int64
				Base, Source nativeTripwireBlock
				Front        []nativePistonBlock
				Actor        *nativePistonActor
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 384 {
		t.Fatal("incomplete native interruption fixture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("sticky%t/face%d/phase%d/%s/offset%d/%s/%s", c.Sticky, c.Facing, c.Phase, c.Motion, c.Offset, c.Payload, c.Replacement), func(t *testing.T) {
			if len(trace.Samples) != 100 || len(trace.Changes) != 2 || len(trace.Front) != 4 || trace.Changes[0].Tick&1 != int64(c.Phase) || trace.Interruption.Tick == 0 {
				t.Fatal("invalid native interruption trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			if err := w.SetGameRule("block_drops", false); err != nil {
				t.Fatal(err)
			}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(trace.Base, Piston{Facing: cube.Face(c.Facing), Sticky: c.Sticky}, nil)
				if c.Payload == "chest" {
					chest := NewChest()
					if err := chest.OwnInventory().SetItem(0, item.NewStack(item.Diamond{}, 7)); err != nil {
						t.Fatal(err)
					}
					tx.SetBlock(trace.Front[0], chest, nil)
				} else {
					for _, pos := range trace.Front[:3] {
						tx.SetBlock(pos, Stone{}, nil)
					}
				}
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Fatal("interruption replay clock mismatch")
					}
					if sample.Actor != nil && sample.Actor.Attached == nil {
						t.Fatal("missing native attached-block observation")
					}
					comparePistonNativeActor(t, tx, sample.Tick, trace.Base, sample.Actor)
					compareSculkContactNativeBlock(t, tx, sample.Tick, trace.Base, sample.Base)
					compareSculkContactNativeBlock(t, tx, sample.Tick, trace.Source, sample.Source)
					if len(sample.Front) != len(trace.Front) {
						t.Fatal("missing native interruption cells")
					}
					for i, pos := range trace.Front {
						b := sample.Front[i]
						compareSculkContactNativeBlock(t, tx, sample.Tick, pos, b.nativeTripwireBlock)
						if b.Inventory != nil {
							comparePistonNativeChest(t, tx, sample.Tick, pos, b.Inventory)
						}
					}
					for _, change := range trace.Changes {
						if change.Tick == sample.Tick {
							var b world.Block = Air{}
							if change.Powered {
								b = RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, b, nil)
						}
					}
					if trace.Interruption.Tick == sample.Tick {
						var b world.Block = Air{}
						if c.Replacement == "stone" {
							b = Stone{}
						}
						position := trace.Base
						if trace.Interruption.Position != nil {
							position = *trace.Interruption.Position
						}
						tx.SetBlock(position, b, nil)
						after := trace.Interruption.After
						if after.Actor != nil && after.Actor.Attached == nil {
							t.Fatal("missing immediate native attached-block observation")
						}
						if len(after.Front) != len(trace.Front) {
							t.Fatal("missing immediate native interruption observation")
						}
						compareSculkContactNativeBlock(t, tx, sample.Tick, trace.Base, after.Base)
						comparePistonNativeActor(t, tx, sample.Tick, trace.Base, after.Actor)
						for i, pos := range trace.Front {
							compareSculkContactNativeBlock(t, tx, sample.Tick, pos, after.Front[i].nativeTripwireBlock)
							if after.Front[i].Inventory != nil {
								comparePistonNativeChest(t, tx, sample.Tick, pos, after.Front[i].Inventory)
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

func comparePistonNativeChest(t *testing.T, tx *world.Tx, tick int64, pos cube.Pos, want []nativePistonStack) {
	t.Helper()
	chest, ok := tx.Block(pos).(Chest)
	if !ok {
		t.Errorf("tick %d missing restored chest at %v", tick, pos)
		return
	}
	got := make([]nativePistonStack, 0)
	for slot, stack := range chest.OwnInventory().Slots() {
		if !stack.Empty() {
			id, _ := stack.Item().EncodeItem()
			got = append(got, nativePistonStack{slot, id, stack.Count()})
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("tick %d restored inventory=%v, native=%v", tick, got, want)
	}
}
