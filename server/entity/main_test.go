package entity

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/internal/testfixture"
	"github.com/df-mc/dragonfly/server/world"
)

// TestMain makes every wait for a world owner from a world owner end the
// tests with its stack (world.PanicOnOwnerWait), so that none hides behind a
// task that recovers it, and fails the tests when code they run recovers a
// panic that none of them expects.
func TestMain(m *testing.M) {
	world.PanicOnOwnerWait = true
	stop := releaseClosedWorlds()
	code := testfixture.RunCheckingPanics(m)
	stop()
	os.Exit(code)
}

// releaseClosedWorlds drops the worlds that closed from
// wanderingTraderSpawners every second until the function it returns is
// called. The map keeps every overworld that ticked with mobs spawning, and
// so all of its chunks, after the world closed: the hundreds of worlds the
// tests make held gigabytes. A world that closed fails a task scheduled on
// it at once; one still open has the task cancelled before it could run.
func releaseClosedWorlds() (stop func()) {
	release := func() {
		wanderingTraderSpawners.Range(func(k, _ any) bool {
			w := k.(*world.World)
			task := w.DoAfter(time.Hour, func(*world.Tx) {})
			if errors.Is(task.Err(), world.ErrWorldClosed) {
				wanderingTraderSpawners.Delete(w)
			} else {
				task.Cancel()
			}
			return true
		})
	}
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				release()
			case <-done:
				release()
				return
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}
