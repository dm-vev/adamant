package world_test

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

var errInvalidGenerationState = errors.New("invalid saved generation state")

type persistentTestGenerator struct {
	mu    sync.Mutex
	value byte
}

func (g *persistentTestGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	if pos[1] != 0 || pos[0] < 3 || pos[0] > 5 {
		return world.GenerationResult{}
	}
	g.mu.Lock()
	g.value++
	var b world.Block = block.Stone{}
	if g.value%2 == 0 {
		b = block.Diamond{}
	}
	g.mu.Unlock()
	c.SetBlock(0, 64, 0, 0, world.BlockRuntimeID(b))
	return world.GenerationResult{}
}
func (*persistentTestGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{0, 64, 0} }
func (g *persistentTestGenerator) LoadGenerationState(data []byte) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(data) == 0 {
		g.value = 0
		return nil
	}
	if len(data) != 1 {
		return errInvalidGenerationState
	}
	g.value = data[0]
	return nil
}
func (g *persistentTestGenerator) SaveGenerationState() ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return []byte{g.value}, nil
}

func checkStatefulGeneratedBlock(t *testing.T, w *world.World, x int, diamond bool) {
	t.Helper()
	task := w.Do(func(tx *world.Tx) {
		b := tx.Block(cube.Pos{x, 64, 0})
		if diamond {
			if _, ok := b.(block.Diamond); !ok {
				t.Errorf("state-restored generated block at %d = %T, want diamond", x, b)
			}
		} else if _, ok := b.(block.Stone); !ok {
			t.Errorf("stateful generated block at %d = %T, want stone", x, b)
		}
	})
	<-task.Done()
	if err := task.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentGeneratorStateSurvivesMCDBRestartAndIsDimensionScoped(t *testing.T) {
	for _, synchronous := range []bool{true, false} {
		name := "workers"
		if synchronous {
			name = "synchronous"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			open := func(dim world.Dimension, readOnly bool) *world.World {
				db, err := mcdb.Open(dir)
				if err != nil {
					t.Fatal(err)
				}
				return world.Config{Provider: db, Dim: dim, Generator: &persistentTestGenerator{}, Synchronous: synchronous, GeneratorWorkers: 2, ReadOnly: readOnly}.New()
			}
			closeWorld := func(w *world.World) {
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
			}
			w := open(world.Overworld, false)
			checkStatefulGeneratedBlock(t, w, 48, false)
			w.Save()
			closeWorld(w)

			w = open(world.Overworld, false)
			checkStatefulGeneratedBlock(t, w, 48, false)
			checkStatefulGeneratedBlock(t, w, 64, true)
			closeWorld(w)

			w = open(world.Nether, false)
			checkStatefulGeneratedBlock(t, w, 48, false)
			closeWorld(w)

			w = open(world.Overworld, true)
			checkStatefulGeneratedBlock(t, w, 80, false)
			w.Save()
			closeWorld(w)

			w = open(world.Overworld, false)
			checkStatefulGeneratedBlock(t, w, 80, false)
			closeWorld(w)

			w = open(world.Nether, false)
			checkStatefulGeneratedBlock(t, w, 64, true)
			closeWorld(w)
		})
	}
}

func TestPersistentGeneratorRejectsInvalidStateBeforeGeneratingOrSaving(t *testing.T) {
	provider := world.NopProvider{Set: &world.Settings{SeedSet: true}, WorldData: new(world.WorldDataMemory)}
	invalid := []byte{7, 9}
	if err := provider.SaveWorldData("generation:0", invalid); err != nil {
		t.Fatal(err)
	}
	g := &persistentTestGenerator{}
	var failure any
	func() {
		defer func() { failure = recover() }()
		w := world.Config{Provider: provider, Generator: g, Synchronous: true}.New()
		_ = w.Close()
	}()
	if err, ok := failure.(error); !ok || !errors.Is(err, errInvalidGenerationState) {
		t.Fatalf("startup failure = %v, want saved generation state error", failure)
	}
	if g.value != 0 {
		t.Fatalf("invalid state generated %d chunks", g.value)
	}
	stored, err := provider.LoadWorldData("generation:0")
	if err != nil || !bytes.Equal(stored, invalid) {
		t.Fatalf("failed startup replaced saved state: data=%v err=%v", stored, err)
	}
}
