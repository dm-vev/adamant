package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

func TestInventorySignalsMatchNativeCapture(t *testing.T) {
	t.Run("containers", func(t *testing.T) {
		testInventorySignalCapture(t, "testdata/bedrock_1_26_51_inventory_signal.json", 81)
	})
	t.Run("pots", func(t *testing.T) {
		testInventorySignalCapture(t, "testdata/bedrock_1_26_51_pot_signal.json", 9)
	})
}

func testInventorySignalCapture(t *testing.T, filename string, count int) {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Pattern string
				Phase          int
			}
			Inventory []*struct {
				ID             string
				Count, Maximum int
			}
			Samples []struct {
				Tick                     int64
				Source, Comparator, Wire torchNetworkBlock
			}
			Changes []struct {
				Tick int64
				Kind string
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != count {
		t.Fatal("incomplete native inventory signal capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(c.Block+"/"+c.Pattern, func(t *testing.T) {
			if len(trace.Samples) != 16 || len(trace.Changes) != 1 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native inventory signal trace")
			}
			contents := make([]item.Stack, len(trace.Inventory))
			for i, entry := range trace.Inventory {
				if entry == nil {
					continue
				}
				it, ok := world.ItemByName(entry.ID, 0)
				if !ok {
					t.Fatalf("unknown native item %s", entry.ID)
				}
				contents[i] = item.NewStack(it, entry.Count)
				if contents[i].MaxCount() != entry.Maximum {
					t.Fatalf("%s max=%d native=%d", entry.ID, contents[i].MaxCount(), entry.Maximum)
				}
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				for x := -3; x <= 3; x++ {
					for z := -1; z <= 2; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				sourcePos := cube.Pos{-1, 65, 0}
				var source world.Block
				switch c.Block {
				case "chest":
					source = NewChest()
				case "double_chest":
					a, b := NewChest(), NewChest()
					a.Facing, b.Facing = cube.East, cube.East
					pairPos := cube.Pos{-1, 65, 1}
					tx.SetBlock(sourcePos, a, nil)
					tx.SetBlock(pairPos, b, nil)
					ch, other, ok := a.pair(tx, sourcePos, pairPos, a)
					if !ok {
						t.Error("could not pair kernel chest")
						return
					}
					tx.SetBlockEntity(pairPos, other)
					source = ch
				case "trapped_chest":
					source = NewTrappedChest()
				case "barrel":
					source = NewBarrel()
				case "hopper":
					source = NewHopper()
				case "dispenser":
					source = NewDispenser()
				case "dropper":
					source = NewDropper()
				case "furnace":
					source = NewFurnace(cube.North)
				case "brewing_stand":
					source = NewBrewingStand()
				case "decorated_pot":
					if len(contents) != 1 {
						t.Error("native pot has unexpected slot count")
						return
					}
					source = DecoratedPot{Item: contents[0]}
				default:
					t.Errorf("unknown native container %s", c.Block)
					return
				}
				tx.SetBlock(sourcePos, source, nil)
				// Fill the actual inventory even if the block accidentally fails
				// the complete Container interface used by comparatorOverride.
				if container, ok := source.(interface {
					Inventory(*world.Tx, cube.Pos) *inventory.Inventory
				}); ok {
					inv := container.Inventory(tx, sourcePos)
					if inv.Size() != len(contents) {
						t.Errorf("kernel slots=%d native=%d", inv.Size(), len(contents))
						return
					}
					for i, stack := range contents {
						if err := inv.SetItem(i, stack); err != nil {
							t.Error(err)
							return
						}
					}
				} else if c.Block != "decorated_pot" {
					t.Error("native container has no readable kernel inventory")
					return
				}
				tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneComparator{Facing: cube.West}, nil)
				tx.SetBlock(cube.Pos{1, 65, 0}, RedstoneWire{}, nil)
			})
			if t.Failed() {
				return
			}
			for i, sample := range trace.Samples {
				w.AdvanceTick()
				if i < 8 {
					continue
				}
				<-w.Exec(func(tx *world.Tx) {
					compareComparatorNativeBlock(t, tx, sample.Tick, cube.Pos{0, 65, 0}, sample.Comparator)
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{1, 65, 0}, sample.Wire)
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{-1, 65, 0}, sample.Source)
					got := tx.Block(cube.Pos{0, 65, 0}).(RedstoneComparator).Output
					if int(got) != sample.Wire.States.Signal {
						t.Error(fmt.Sprintf("output=%d native=%d", got, sample.Wire.States.Signal))
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
