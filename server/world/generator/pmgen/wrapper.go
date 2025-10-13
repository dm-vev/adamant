package pmgen

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Overworld is a thread-safe wrapper around the pm-gen terrain generator that satisfies
// the world.Generator interface used by Dragonfly worlds. BindWorld must be called once
// the world is available so population routines can access it safely.
type Overworld struct {
	seed int64

	mu  sync.RWMutex
	gen *Generator
}

// NewOverworld creates a new wrapper using the seed provided.
func NewOverworld(seed int64) *Overworld {
	// Create the underlying generator immediately so worlds can request
	// generation before BindWorld is invoked during world construction.
	return &Overworld{seed: seed, gen: New(seed)}
}

// BindWorld initialises the underlying pm-gen generator with the world handle. It is safe to
// call BindWorld multiple times; only the first call will initialise the generator.
func (o *Overworld) BindWorld(w *world.World) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.gen == nil {
		o.gen = New(o.seed)
	}
	o.gen.BindWorld(w)
}

// GenerateChunk delegates generation to the underlying pm-gen implementation. BindWorld must
// have been called before this method is invoked.
func (o *Overworld) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	o.mu.RLock()
	gen := o.gen
	o.mu.RUnlock()
	// gen is created in NewOverworld, so this should not be nil.
	gen.GenerateChunk(pos, c)
}
