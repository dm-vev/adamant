package world

import "time"

// ReleasingGenerator is a Generator that keeps caches of what it computed for
// the chunks around those it generates, such as the terrain that decoration
// reads from the chunks next to the one it decorates. The caches are bounded,
// yet sized for the front of generation of a player at a view distance of 32
// chunks, which holds hundreds of megabytes once the front stops moving.
type ReleasingGenerator interface {
	Generator
	// ReleaseGenerationCaches drops the caches. What they held is computed
	// again, in the same bytes, if generation resumes. It is called from a
	// goroutine of its own, while chunks may be generated.
	ReleaseGenerationCaches()
}

// generatorIdleRelease is how long no chunk is generated before the world
// has its generator release its caches: a player who stands still, or whose
// view is complete, does not need them; one who walks on generates chunks
// every few seconds, and keeps them.
const generatorIdleRelease = 30 * time.Second

// armCacheRelease has the generator release its caches once the generation
// has been idle for generatorIdleRelease: it is called as each chunk is
// generated, and moves the time on.
func (w *World) armCacheRelease() {
	if w.releaser == nil {
		return
	}
	w.releaseMu.Lock()
	defer w.releaseMu.Unlock()
	if w.closed.Load() {
		return
	}
	if w.releaseTimer == nil {
		w.releaseTimer = time.AfterFunc(w.releaseAfter, w.releaseIdleCaches)
		return
	}
	w.releaseTimer.Reset(w.releaseAfter)
}

// releaseIdleCaches releases the caches of the generator unless a chunk
// waits to be generated or is being: the generation of that one arms the
// release again.
func (w *World) releaseIdleCaches() {
	if w.closed.Load() || w.generatorQueue.length.Load() > 0 || w.stats.GenerationBusy.Load() > 0 {
		return
	}
	w.releaser.ReleaseGenerationCaches()
	w.stats.CachesReleased.Inc()
}

// stopCacheRelease stops the release of the caches, for a closing world.
func (w *World) stopCacheRelease() {
	w.releaseMu.Lock()
	defer w.releaseMu.Unlock()
	if w.releaseTimer != nil {
		w.releaseTimer.Stop()
	}
}
