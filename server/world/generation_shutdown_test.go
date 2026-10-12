package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type shutdownGenerator struct {
	started chan struct{}
	release chan struct{}
	handle  *EntityHandle
}

func (*shutdownGenerator) DefaultSpawn(Dimension) cube.Pos { return cube.Pos{32, 70, 0} }

func (g *shutdownGenerator) GenerateChunk(pos ChunkPos, _ *chunk.Chunk, _ GenerationContext) GenerationResult {
	if pos != (ChunkPos{}) {
		return GenerationResult{}
	}
	g.handle = (EntitySpawnOpts{}).New(taskTestEntityType{}, taskTestEntityConfig{})
	close(g.started)
	<-g.release
	return GenerationResult{Entities: []*EntityHandle{g.handle}}
}

func TestCloseConsumesInFlightGeneratedEntities(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		name := "writable"
		if readOnly {
			name = "read-only"
		}
		t.Run(name, func(t *testing.T) {
			ctx := testContext(t)
			g := &shutdownGenerator{started: make(chan struct{}), release: make(chan struct{})}
			w := Config{Generator: g, GeneratorWorkers: 1, ReadOnly: readOnly}.New()
			task := w.Do(func(*Tx) {
				if _, err := w.loadChunk(ChunkPos{}); err != nil {
					t.Error(err)
				}
			})
			<-task.Done()
			if err := task.Err(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-g.started:
			case <-ctx.Done():
				t.Fatal("generator did not start")
			}
			closed := make(chan struct{})
			go func() {
				_ = w.Close()
				close(closed)
			}()
			// Force the result to arrive after shutdown has stopped publication.
			<-w.closeStarted
			close(g.release)
			select {
			case <-closed:
			case <-ctx.Done():
				t.Fatal("close blocked on generated entity")
			}
			if !g.handle.Closed() {
				t.Fatal("in-flight generated entity was discarded without closing")
			}
			if count := w.entityCount.Load(); count != 0 {
				t.Fatalf("closed world retains %d entities", count)
			}
		})
	}
}
