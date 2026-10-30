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

func TestBookRecordSignalsMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_book_record_signal.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block                                  string
				Disc, Book                             *string
				Paused                                 bool
				Pages, Page, Facing, Mask, Last, Phase int
			}
			Samples []struct {
				Tick                     int64
				Source, Comparator, Wire torchNetworkBlock
			}
			Changes []struct{ Tick int64 }
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 267 {
		t.Fatal("incomplete native book/record signal capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		name := c.Block
		var source world.Block
		switch c.Block {
		case "jukebox":
			j := Jukebox{}
			if c.Disc != nil {
				it, ok := world.ItemByName("minecraft:music_disc_"+*c.Disc, 0)
				if !ok {
					t.Fatalf("unknown native disc %s", *c.Disc)
				}
				j.Item = item.NewStack(it, 1)
				name += "/" + *c.Disc
			} else {
				name += "/empty"
			}
			source = j
			name += fmt.Sprintf("/paused%v", c.Paused)
		case "lectern":
			l := Lectern{Page: c.Page}
			if c.Book != nil {
				pages := make([]string, c.Pages)
				for i := range pages {
					pages[i] = fmt.Sprintf("page %d", i)
				}
				switch *c.Book {
				case "writable_book":
					l.Book = item.NewStack(item.BookAndQuill{Pages: pages}, 1)
				case "written_book":
					l.Book = item.NewStack(item.WrittenBook{Pages: pages}, 1)
				default:
					t.Fatalf("unknown native book %s", *c.Book)
				}
				name += "/" + *c.Book
			} else {
				name += "/empty"
			}
			source = l
			name += fmt.Sprintf("/pages%d/spread%d", c.Pages, c.Page)
		case "chiseled_bookshelf":
			// Native actor memory is one-based, including zero for no interaction.
			b := NewChiseledBookshelf()
			b.Facing = [...]cube.Direction{cube.South, cube.West, cube.North, cube.East}[c.Facing]
			b.LastInteractedSlot = c.Last - 1
			b.StoredMask = uint8(c.Mask)
			for i := range b.Items {
				if c.Mask&(1<<i) != 0 {
					b.Items[i] = item.NewStack(item.Book{}, 1)
				}
			}
			source = b
			name += fmt.Sprintf("/facing%d/mask%d/last%d", c.Facing, c.Mask, c.Last)
		default:
			t.Fatalf("unexpected native source %s", c.Block)
		}
		t.Run(name, func(t *testing.T) {
			if len(trace.Samples) != 16 || len(trace.Changes) != 1 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native book/record trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				for x := -2; x <= 2; x++ {
					for z := -1; z <= 1; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{-1, 65, 0}, source, nil)
				tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneComparator{Facing: cube.West}, nil)
				tx.SetBlock(cube.Pos{1, 65, 0}, RedstoneWire{}, nil)
			})
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
						t.Errorf("output=%d native=%d", got, sample.Wire.States.Signal)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
