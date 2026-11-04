package world_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
)

// countingStore keeps the columns of the worlds of a test in memory and
// counts how often each position is stored. A position in failStores fails
// that many stores before storing again.
type countingStore struct {
	mu         sync.Mutex
	columns    map[world.ChunkPos]*chunk.Column
	stores     map[world.ChunkPos]int
	failStores map[world.ChunkPos]int
}

var errInjectedStore = errors.New("injected: disk full")

// countingProvider is the provider of one world over a countingStore. A
// world cannot be opened with a provider closed before.
type countingProvider struct {
	world.NopProvider
	*countingStore
}

func newCountingStore() *countingStore {
	return &countingStore{
		columns:    make(map[world.ChunkPos]*chunk.Column),
		stores:     make(map[world.ChunkPos]int),
		failStores: make(map[world.ChunkPos]int),
	}
}

func (p *countingProvider) LoadColumn(pos world.ChunkPos, _ world.Dimension) (*chunk.Column, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if col, ok := p.columns[pos]; ok {
		return col, nil
	}
	return nil, leveldb.ErrNotFound
}

func (p *countingProvider) StoreColumn(pos world.ChunkPos, _ world.Dimension, col *chunk.Column) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failStores[pos] > 0 {
		p.failStores[pos]--
		return errInjectedStore
	}
	p.columns[pos] = col
	p.stores[pos]++
	return nil
}

// takeStores returns the stores counted since the last call.
func (p *countingStore) takeStores() map[world.ChunkPos]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	stores := p.stores
	p.stores = make(map[world.ChunkPos]int)
	return stores
}

func (p *countingStore) failNextStores(pos world.ChunkPos, n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failStores[pos] = n
}

// TestSaveWritesOnlyDirtyColumns checks that a save stores only the columns
// that changed since the last save, through the background saver or not: a
// second save with nothing changed stores nothing, a change stores its column
// only, a change of a container's contents stores its column, and a column
// whose store failed is stored again by the next save, loaded or unloaded.
func TestSaveWritesOnlyDirtyColumns(t *testing.T) {
	for _, synchronous := range []bool{true, false} {
		t.Run(fmt.Sprintf("synchronous=%v", synchronous), func(t *testing.T) {
			t.Parallel()
			p := newCountingStore()
			newWorld := func() *world.World {
				return world.Config{
					Provider:        &countingProvider{countingStore: p},
					Generator:       world.NopGenerator{},
					RandomTickSpeed: -1,
					Synchronous:     synchronous,
				}.New()
			}
			a, b, c := cube.Pos{1, 10, 1}, cube.Pos{33, 10, 1}, cube.Pos{65, 10, 1}
			posA, posB, posC := world.ChunkPos{0, 0}, world.ChunkPos{2, 0}, world.ChunkPos{4, 0}
			expect := func(step string, want map[world.ChunkPos]int) {
				t.Helper()
				got := p.takeStores()
				for pos, n := range want {
					if got[pos] != n {
						t.Errorf("%s: column %v stored %d times, want %d (all stores: %v)", step, pos, got[pos], n, got)
					}
				}
				for pos, n := range got {
					if _, ok := want[pos]; !ok {
						t.Errorf("%s: column %v stored %d times, want 0 (all stores: %v)", step, pos, n, got)
					}
				}
			}

			w := newWorld()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(a, block.Stone{}, nil)
				tx.SetBlock(b, block.Stone{}, nil)
				tx.SetBlock(c, block.NewChest(), nil)
			})
			w.Save()
			expect("first save", map[world.ChunkPos]int{posA: 1, posB: 1, posC: 1})

			w.Save()
			// The column with the chest is saved again: a container's
			// contents change without marking its column changed.
			expect("second save with nothing changed", map[world.ChunkPos]int{posC: 1})

			<-w.Exec(func(tx *world.Tx) { tx.SetBlock(a, block.Dirt{}, nil) })
			w.Save()
			expect("save after a change", map[world.ChunkPos]int{posA: 1, posC: 1})

			<-w.Exec(func(tx *world.Tx) {
				if _, err := tx.Block(c).(block.Chest).Inventory(tx, c).AddItem(item.NewStack(item.Diamond{}, 10)); err != nil {
					t.Errorf("add diamonds to the chest: %v", err)
				}
			})
			w.Save()
			expect("save after filling the chest", map[world.ChunkPos]int{posC: 1})

			// A store that fails leaves the column to the next save.
			p.failNextStores(posB, 1)
			<-w.Exec(func(tx *world.Tx) { tx.SetBlock(b, block.Dirt{}, nil) })
			w.Save()
			expect("save failing to store", map[world.ChunkPos]int{posC: 1})
			w.Save()
			expect("save after the failed store", map[world.ChunkPos]int{posB: 1, posC: 1})
			w.Save()
			expect("save after the store again", map[world.ChunkPos]int{posC: 1})

			// A column whose store failed and that was unloaded since is
			// stored by the next save.
			p.failNextStores(posA, 1)
			<-w.Exec(func(tx *world.Tx) { tx.SetBlock(a, block.Stone{}, nil) })
			w.Save()
			expect("save failing to store before unloading", map[world.ChunkPos]int{posC: 1})
			<-w.Exec(func(tx *world.Tx) {
				if n, _, _ := w.CollectGarbage(tx); n != 3 {
					t.Errorf("unloaded %d chunks, want 3", n)
				}
			})
			w.Save()
			// Unloading stores the column again, and the chest's column
			// as every unload does.
			expect("unloading and saving after the failed store", map[world.ChunkPos]int{posA: 1, posC: 1})

			if !synchronous {
				// The background saver fails to store a column unloaded
				// already: the next save stores it again as it was. Which
				// of the two saves does depends on when the store failed.
				p.failNextStores(posA, 1)
				<-w.Exec(func(tx *world.Tx) {
					tx.SetBlock(a, block.Dirt{}, nil)
					tx.SetBlock(a, block.Stone{}, nil)
					w.CollectGarbage(tx)
				})
				w.Save()
				w.Save()
				expect("saving after a failed store of an unloaded column", map[world.ChunkPos]int{posA: 1})
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}

			// The world holds every change after loading it again.
			w = newWorld()
			<-w.Exec(func(tx *world.Tx) {
				for pos, want := range map[cube.Pos]world.Block{a: block.Stone{}, b: block.Dirt{}} {
					if got := tx.Block(pos); got != want {
						t.Errorf("block at %v after loading again = %#v, want %#v", pos, got, want)
					}
				}
				it, err := tx.Block(c).(block.Chest).Inventory(tx, c).Item(0)
				if err != nil || it.Count() != 10 {
					t.Errorf("chest after loading again holds %v (%v), want 10 diamonds", it, err)
				}
				// Only the chest's contents change in the column loaded.
				if _, err := tx.Block(c).(block.Chest).Inventory(tx, c).AddItem(item.NewStack(item.Diamond{}, 5)); err != nil {
					t.Errorf("add diamonds to the chest: %v", err)
				}
			})
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			w = newWorld()
			<-w.Exec(func(tx *world.Tx) {
				it, err := tx.Block(c).(block.Chest).Inventory(tx, c).Item(0)
				if err != nil || it.Count() != 15 {
					t.Errorf("chest changed after loading holds %v (%v), want 15 diamonds", it, err)
				}
			})
		})
	}
}
