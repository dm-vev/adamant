package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

func TestExecuteInDoesNotSuspendSourceBehindBusyDimension(t *testing.T) {
	source := world.Config{GeneratorWorkers: 1}.New()
	target := world.Config{Dim: world.Nether, GeneratorWorkers: 1}.New()
	busy, release := make(chan struct{}), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
		_ = source.Close()
		_ = target.Close()
	}()
	previousServer := commandServer
	previousTime, hadTime := cmd.ByAlias("time")
	defer func() {
		commandServer = previousServer
		if hadTime {
			cmd.Register(previousTime)
		}
	}()
	commandServer = executeDimensionAdapter{world: source, nether: target, end: target}
	cmd.Register(newTimeCommand())
	execute := cmd.New("execute", "", nil, executeCommand{})
	target.Do(func(*world.Tx) { close(busy); <-release })
	<-busy
	// Saturate the bounded ingress while Do keeps the additional work queued.
	for range 512 {
		target.Do(func(*world.Tx) {})
	}
	issued := source.Do(func(tx *world.Tx) {
		execute.Execute("in minecraft:the_nether run time set 6000", executeTestSource{}, tx)
		tx.World().SetTime(12000)
	})
	completed := false
	select {
	case <-issued.Done():
		completed = true
	case <-time.After(time.Second):
	}
	observed := source.Time()
	close(release)
	released = true
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := issued.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if !completed || observed < 12000 {
		t.Fatalf("execute suspended source: completed=%v time=%d", completed, observed)
	}
	for target.Time() < 6000 {
		select {
		case <-ctx.Done():
			t.Fatal("deferred command never changed destination time")
		case <-time.After(time.Millisecond):
		}
	}
}
