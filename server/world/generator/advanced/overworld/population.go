package overworld

import (
	"runtime"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/world"
)

type worldPointer struct {
	atomic.Pointer[world.World]
}

type populationJob struct {
	pos world.ChunkPos
}

var populationSetOpts = &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true}

// BindWorld provides the world handle used during chunk population.
func (g *Overworld) BindWorld(w *world.World) {
	if w == nil {
		return
	}
	g.world.Store(w)
	g.popOnce.Do(func() {
		go g.populate()
	})
}

func (g *Overworld) enqueuePopulation(pos world.ChunkPos) {
	if g.world.Load() == nil {
		return
	}
	select {
	case g.populationQueue <- populationJob{pos: pos}:
	default:
	}
}

func (g *Overworld) populate() {
	for job := range g.populationQueue {
		// Run inline to keep population concurrency bounded.
		g.runPopulationJob(job)
	}
}

func (g *Overworld) runPopulationJob(job populationJob) {
	w := g.world.Load()
	for w == nil {
		runtime.Gosched()
		w = g.world.Load()
	}

	for {
		var (
			skip  bool
			ready bool
			wait  <-chan struct{}
		)
		<-w.Exec(func(tx *world.Tx) {
			loaded, chunkReady := tx.ChunkState(job.pos)
			if !loaded {
				skip = true
				return
			}
			if chunkReady {
				ready = true
				return
			}
			wait, _ = tx.ChunkReadySignal(job.pos)
		})
		if skip {
			return
		}
		if ready {
			break
		}
		if wait != nil {
			<-wait
		} else {
			runtime.Gosched()
		}
	}

	<-w.Exec(func(tx *world.Tx) {
		if !tx.ChunkLoaded(job.pos) {
			return
		}
		g.populateChunk(tx, job.pos)
	})
}
