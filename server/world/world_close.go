package world

import (
	"sync"
	"sync/atomic"
	"time"
)

// lifecycle is whether the world is closing, and when it was made.
type lifecycle struct {
	// closed flips once close starts; new tasks fail with ErrWorldClosed.
	// closeAcceptingEntityTasks is true only during the close transaction, when
	// entity Close methods may still schedule final work that close drains.
	closed                    atomic.Bool
	closeAcceptingEntityTasks atomic.Bool
	o                         sync.Once
	// closeStarted closes as soon as World.Close begins, before the close
	// transaction runs; closing closes once the world stops ticking.
	closeStarted chan struct{}
	closing      chan struct{}
	running      sync.WaitGroup
	// made is when the world was made.
	made time.Time
}

// Close closes the world and saves all chunks currently loaded. Called from a
// world owner or a region worker, it returns ErrOwnerWait without closing.
func (w *World) Close() error {
	if w == nil {
		return nil
	}
	if !w.conf.Synchronous {
		if err := checkOwnerWait("World.Close"); err != nil {
			return err
		}
	}
	w.o.Do(w.close)
	return nil
}

// close stops the World from ticking, saves all chunks to the Provider and
// updates the world's settings.
func (w *World) close() {
	w.scheduleMu.Lock()
	w.closed.Store(true)
	close(w.closeStarted)
	w.scheduleMu.Unlock()

	w.scheduling.Wait()
	w.stopCacheRelease()
	w.drainGenerationQueue()
	w.generatorRunning.Wait()
	w.scheduleMu.Lock()
	w.closeAcceptingEntityTasks.Store(true)
	w.scheduleMu.Unlock()
	<-w.exec(func(tx *Tx) {
		// Let user code run anything that needs to be finished before closing.
		w.Handler().HandleClose(tx)
		tx.runDeferred()
		w.Handle(NopHandler{})
		clear(w.chunkRequests)
		tx.LoadDragonArena(false)

		if w.conf.ReadOnly {
			for pos, c := range w.chunks {
				w.closeChunk(tx, pos, c)
			}
		} else {
			// The saver writes the columns closed before it stops below.
			_ = w.capture(tx, true, nil).write(false)
		}
	})
	if w.saver != nil {
		w.saver.close()
	}
	w.scheduleMu.Lock()
	w.closeAcceptingEntityTasks.Store(false)
	w.scheduleMu.Unlock()
	w.scheduling.Wait()

	close(w.closing)
	w.running.Wait()

	close(w.queueClosing)
	w.queueing.Wait()

	w.set.Lock()
	w.set.unregisterWorldLocked(w)
	w.set.Unlock()
	if !releaseProvider(w.providerUse) {
		return
	}

	// The last world of the provider writes what was staged after the
	// worlds captured themselves as they closed.
	if !w.conf.ReadOnly {
		if err := w.providerUse.staging.write(w.conf.Provider, w.providerUse.staging.take()); err != nil {
			w.saveLog.Error("save staged world data: " + err.Error())
		}
	}
	w.saveLog.Debug("Closing provider...")
	saveErr, closeErr := w.providerUse.closeProvider(w.conf.Provider)
	if saveErr != nil {
		w.saveLog.Error("save position tracking data: " + saveErr.Error())
	}
	if closeErr != nil {
		w.saveLog.Error("close world provider: " + closeErr.Error())
	}
}
