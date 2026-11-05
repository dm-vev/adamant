package world

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
