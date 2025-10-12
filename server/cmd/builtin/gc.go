package builtin

import (
	"runtime"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

type gcCommand struct{}

func newGCCommand(_ serverAdapter) cmd.Command {
	return cmd.New("gc", "Runs chunk cleanup and triggers a Go garbage collection cycle.", nil, gcCommand{})
}

func (g gcCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	if w == nil {
		o.Error("world unavailable")
		return
	}

	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	chunksBefore := w.LoadedChunkCount()
	entitiesBefore := w.EntityCount()

	chunksCollected, entitiesCollected, blockEntitiesCollected := w.CollectGarbage(tx)
	chunksAfter := w.LoadedChunkCount()
	entitiesAfter := w.EntityCount()

	runtime.GC()

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	freedBytes := uint64(0)
	if before.HeapAlloc > after.HeapAlloc {
		freedBytes = before.HeapAlloc - after.HeapAlloc
	}

	o.Print("---- Garbage collection result ----")
	o.Printf("Chunks closed: %d (now %d loaded, %d before)", chunksCollected, chunksAfter, chunksBefore)
	o.Printf("Entities closed: %d (now %d active, %d before)", entitiesCollected, entitiesAfter, entitiesBefore)
	o.Printf("Block entities removed: %d", blockEntitiesCollected)
	o.Printf("Heap memory freed: %.2f MiB (current heap %.2f MiB)", bytesToMiB(freedBytes), bytesToMiB(after.HeapAlloc))
}
