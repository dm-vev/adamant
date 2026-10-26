package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestBookshelfHopperSignalsMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_bookshelf_hopper.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Mode                string
				Count, Facing, Mask, Phase int
			}
			Samples []struct {
				Tick        int64
				HopperCount int `json:"hopper_count"`
				Source      struct {
					ID     string
					States struct {
						Mask int `json:"books_stored"`
					}
				}
				Comparator, Wire torchNetworkBlock
			}
			Changes []struct{ Tick int64 }
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 50 {
		t.Fatal("incomplete native bookshelf transfer capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/facing%d/count%d", c.Block, c.Mode, c.Facing, c.Count), func(t *testing.T) {
			if len(trace.Samples) != 128 || len(trace.Changes) != 1 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native bookshelf transfer trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			sourcePos, hopperPos := cube.Pos{-1, 65, 0}, cube.Pos{-1, 66, 0}
			if c.Mode == "extract" {
				hopperPos[1] = 64
			}
			<-w.Exec(func(tx *world.Tx) {
				for x := -2; x <= 2; x++ {
					for z := -1; z <= 1; z++ {
						for y := 63; y <= 64; y++ {
							tx.SetBlock(cube.Pos{x, y, z}, Stone{}, nil)
						}
					}
				}
				switch c.Block {
				case "chiseled_bookshelf":
					b := NewChiseledBookshelf()
					b.Facing = [...]cube.Direction{cube.South, cube.West, cube.North, cube.East}[c.Facing]
					for i := range b.Items {
						if c.Mask&(1<<i) != 0 {
							b.Items[i] = item.NewStack(item.Book{}, 1)
						}
					}
					tx.SetBlock(sourcePos, b, nil)
				case "barrel":
					b := NewBarrel()
					if c.Mode == "extract" {
						_ = b.Inventory(tx, sourcePos).SetItem(0, item.NewStack(item.Book{}, c.Count))
					}
					tx.SetBlock(sourcePos, b, nil)
				default:
					t.Errorf("unknown native transfer source %s", c.Block)
					return
				}
				h := NewHopper()
				h.Facing = cube.FaceDown
				if c.Mode == "insert" {
					_ = h.Inventory(tx, hopperPos).SetItem(0, item.NewStack(item.Book{}, c.Count))
				}
				tx.SetBlock(hopperPos, h, nil)
				tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneComparator{Facing: cube.West}, nil)
				tx.SetBlock(cube.Pos{1, 65, 0}, RedstoneWire{}, nil)
			})
			if t.Failed() {
				return
			}
			for i, sample := range trace.Samples {
				w.AdvanceTick()
				// This audit compares settled transfer results and comparator
				// memory, not the hopper's individual transfer tick phase.
				if i < 112 {
					continue
				}
				<-w.Exec(func(tx *world.Tx) {
					compareComparatorNativeBlock(t, tx, sample.Tick, cube.Pos{0, 65, 0}, sample.Comparator)
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{1, 65, 0}, sample.Wire)
					b := tx.Block(sourcePos)
					id, states := b.EncodeBlock()
					if id != sample.Source.ID {
						t.Errorf("source=%s native=%s", id, sample.Source.ID)
					}
					if c.Block == "chiseled_bookshelf" && int(states["books_stored"].(int32)) != sample.Source.States.Mask {
						t.Errorf("shelf mask=%v native=%d", states["books_stored"], sample.Source.States.Mask)
					}
					h := tx.Block(hopperPos).(Hopper)
					count := 0
					for _, stack := range h.Inventory(tx, hopperPos).Slots() {
						count += stack.Count()
					}
					if count != sample.HopperCount {
						t.Errorf("hopper count=%d native=%d", count, sample.HopperCount)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
