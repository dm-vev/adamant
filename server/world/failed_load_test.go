package world_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
)

// columnStore holds columns in memory for the failingLoadProviders of the
// worlds of a test, one after another. While fail is set, loading the column
// at failPos fails as a corrupt or unreadable column would.
type columnStore struct {
	failPos world.ChunkPos

	mu      sync.Mutex
	fail    bool
	columns map[world.ChunkPos]*chunk.Column
	stores  map[world.ChunkPos]int
}

// failingLoadProvider is a provider of one world reading and writing a
// columnStore. A world cannot be opened with a provider closed before.
type failingLoadProvider struct {
	world.NopProvider
	*columnStore
}

var errInjectedLoad = errors.New("injected: corrupt column")

func (p *failingLoadProvider) LoadColumn(pos world.ChunkPos, _ world.Dimension) (*chunk.Column, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail && pos == p.failPos {
		return nil, errInjectedLoad
	}
	if col, ok := p.columns[pos]; ok {
		return col, nil
	}
	return nil, leveldb.ErrNotFound
}

func (p *failingLoadProvider) StoreColumn(pos world.ChunkPos, _ world.Dimension, col *chunk.Column) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.columns[pos] = col
	p.stores[pos]++
	return nil
}

func (s *columnStore) setFail(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = fail
	clear(s.stores)
}

func (s *columnStore) column(pos world.ChunkPos) *chunk.Column {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.columns[pos]
}

func (s *columnStore) storesAt(pos world.ChunkPos) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stores[pos]
}

// TestFailedLoadColumnNotSaved checks that the empty column a world puts in
// place of one that failed to load is never stored over it: not by World.Save,
// not when it is unloaded and not when the world closes, through the
// background saver or not. Other columns are saved as usual, the column can
// still be changed, and once the column loads again it is saved again.
func TestFailedLoadColumnNotSaved(t *testing.T) {
	for _, synchronous := range []bool{true, false} {
		t.Run(fmt.Sprintf("synchronous=%v", synchronous), func(t *testing.T) {
			t.Parallel()
			failPos, goodPos := world.ChunkPos{0, 0}, world.ChunkPos{4, 0}
			failBlock, goodBlock := cube.Pos{1, 10, 1}, cube.Pos{65, 10, 1}
			store := &columnStore{
				failPos: failPos,
				columns: make(map[world.ChunkPos]*chunk.Column),
				stores:  make(map[world.ChunkPos]int),
			}
			var logs bytes.Buffer
			var logMu sync.Mutex
			newWorld := func() *world.World {
				return world.Config{
					Provider:        &failingLoadProvider{columnStore: store},
					Generator:       world.NopGenerator{},
					RandomTickSpeed: -1,
					Synchronous:     synchronous,
					Log:             slog.New(slog.NewTextHandler(&lockedWriter{w: &logs, mu: &logMu}, nil)),
				}.New()
			}

			// The column on "disk": dirt where the test later sets stone.
			w := newWorld()
			<-w.Exec(func(tx *world.Tx) { tx.SetBlock(failBlock, block.Dirt{}, nil) })
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if store.storesAt(failPos) == 0 {
				t.Fatal("column was not stored to begin with")
			}
			stored := store.column(failPos)

			// The column fails to load. The world plays on in the empty
			// column standing in for it, but saving, unloading and
			// closing never store it.
			store.setFail(true)
			logMu.Lock()
			logs.Reset()
			logMu.Unlock()
			w = newWorld()
			<-w.Exec(func(tx *world.Tx) {
				if got := tx.Block(failBlock); got != (block.Air{}) {
					t.Errorf("block in the column that failed to load = %#v, want air", got)
				}
				tx.SetBlock(failBlock, block.Stone{}, nil)
				tx.SetBlock(goodBlock, block.Stone{}, nil)
				if got := tx.Block(failBlock); got != (block.Stone{}) {
					t.Errorf("block set in the column that failed to load = %#v, want stone", got)
				}
			})
			w.Save()
			<-w.Exec(func(tx *world.Tx) {
				// Saved since, the column beside it is stored by unloading
				// only if it changed again.
				tx.SetBlock(goodBlock, block.Dirt{}, nil)
				if n, _, _ := w.CollectGarbage(tx); n != 2 {
					t.Errorf("unloaded %d chunks, want 2", n)
				}
				// Loaded again, the column fails again and is replaced by a
				// new empty column, changed and left for Close.
				tx.SetBlock(failBlock, block.Stone{}, nil)
				tx.SetBlock(goodBlock, block.Stone{}, nil)
			})
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if n := store.storesAt(failPos); n != 0 {
				t.Fatalf("column that failed to load was stored %d times", n)
			}
			if store.column(failPos) != stored {
				t.Fatal("stored column of the position that failed to load was replaced")
			}
			if n := store.storesAt(goodPos); n < 3 {
				t.Fatalf("column next to it was stored %d times, want one save for World.Save, unloading and closing", n)
			}
			logMu.Lock()
			out := logs.String()
			logMu.Unlock()
			// One warning for each column standing in: the first one was
			// skipped twice (World.Save, unloading), the second once.
			if n := strings.Count(out, "Not saving chunk"); n != 2 {
				t.Fatalf("logged %d warnings about the unsaved column, want 2: %s", n, out)
			}
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "Not saving chunk") && (!strings.Contains(line, "level=WARN") || !strings.Contains(line, "X=0 Z=0") || !strings.Contains(line, errInjectedLoad.Error())) {
					t.Fatalf("warning without level, position or load error: %s", line)
				}
			}

			// The column loads again: it holds what was stored, and changes
			// to it are saved as usual.
			store.setFail(false)
			w = newWorld()
			<-w.Exec(func(tx *world.Tx) {
				if got := tx.Block(failBlock); got != (block.Dirt{}) {
					t.Errorf("block after loading the column again = %#v, want the dirt stored", got)
				}
				tx.SetBlock(failBlock, block.Stone{}, nil)
			})
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if n := store.storesAt(failPos); n == 0 {
				t.Fatal("column loaded again was not stored")
			}
			w = newWorld()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				if got := tx.Block(failBlock); got != (block.Stone{}) {
					t.Errorf("block after saving the column loaded again = %#v, want stone", got)
				}
			})
		})
	}
}

// lockedWriter serialises writes to w.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}
