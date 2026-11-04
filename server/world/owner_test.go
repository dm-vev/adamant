package world

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
)

// TestGoroutineID checks that the ID of a goroutine is read from its stack on
// the toolchain the tests run with: a toolchain changing the format fails
// here rather than leaving owners unknown.
func TestGoroutineID(t *testing.T) {
	id := goroutineID()
	if id == 0 || goroutineID() != id {
		t.Fatalf("goroutineID() = %d, then %d, want the same ID, not 0", id, goroutineID())
	}
	other := make(chan uint64)
	go func() { other <- goroutineID() }()
	if o := <-other; o == 0 || o == id {
		t.Fatalf("goroutineID() of another goroutine = %d, want another ID than %d, not 0", o, id)
	}
}

// TestOwnersAreKnown checks that the goroutine of a World is an owner while
// it runs, and no longer once the World closed, and that a goroutine
// entering as an owner twice stays one until it leaves as the first.
func TestOwnersAreKnown(t *testing.T) {
	w := Config{Provider: NopProvider{}, Generator: NopGenerator{}}.New()
	var owner uint64
	got, err := Call(context.Background(), w, func(*Tx) (*World, error) {
		owner = goroutineID()
		return ownerOf(), nil
	})
	if err != nil {
		t.Fatalf("a Call from off any owner failed: %v", err)
	}
	if got != w {
		t.Errorf("ownerOf() on the goroutine of a World = %v, want the World", got)
	}
	if ownerOf() != nil {
		t.Error("the goroutine of the test is an owner")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := owners.Load(owner); ok {
		t.Error("the goroutine of a World closed is still an owner")
	}

	id, entered := enterOwner(w)
	if !entered || ownerOf() != w {
		t.Fatal("a goroutine entering as an owner is not one")
	}
	if _, again := enterOwner(w); again {
		t.Error("an owner entered again")
	}
	leaveOwner(id)
	if ownerOf() != nil {
		t.Error("a goroutine that left is still an owner")
	}
}

// ownerHelperEnv runs the helpers of TestCallFromOwnerFailsFast and
// TestCallFromRegionWorkerFailsFast in subprocesses: they set
// PanicOnOwnerWait, which the other tests of the package run with.
const ownerHelperEnv = "ADAMANT_OWNER_HELPER"

// runOwnerHelper runs the helper test of the name passed in a subprocess,
// returning its output and exit code.
func runOwnerHelper(t *testing.T, name, mode string) (string, int) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^"+name+"$", "-test.v")
	c.Env = append(os.Environ(), ownerHelperEnv+"="+mode)
	out, err := c.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("run %s: %v", name, err)
	}
	return string(out), exit.ExitCode()
}

// TestCallFromOwnerFailsFast checks that the waits for an owner from an owner
// fail at once with ErrOwnerWait: before the guard, a Call from the owner
// of the World it calls deadlocked, and the subprocess exited with status 3.
// With PanicOnOwnerWait, the wait panics with its stack and ends the program,
// although a task recovers the panics of its callback.
func TestCallFromOwnerFailsFast(t *testing.T) {
	if os.Getenv(ownerHelperEnv) != "" {
		t.Skip("in the subprocess")
	}
	out, code := runOwnerHelper(t, "TestOwnerWaitHelper", "error")
	if code != 0 {
		t.Fatalf("the waits from an owner exited with status %d, want 0:\n%s", code, out)
	}
	for _, want := range []string{
		"Call on its own world: " + ErrOwnerWait.Error(),
		"Call on another world: " + ErrOwnerWait.Error(),
		"Task.Wait: " + ErrOwnerWait.Error(),
		"Close: " + ErrOwnerWait.Error(),
		"Call on a synchronous world: <nil> 1",
		"the call's task ran: false",
	} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("the waits from an owner did not print %q:\n%s", want, out)
		}
	}

	out, code = runOwnerHelper(t, "TestOwnerWaitHelper", "panic")
	if code != 2 || !strings.Contains(out, ErrOwnerWait.Error()+": a call on the owner of world") || !strings.Contains(out, "owner_test.go") {
		t.Errorf("a wait from an owner with PanicOnOwnerWait exited with status %d, want 2, with the error and the stack of the wait:\n%s", code, out)
	}
}

// TestOwnerWaitHelper is the subprocess of TestCallFromOwnerFailsFast.
func TestOwnerWaitHelper(t *testing.T) {
	mode := os.Getenv(ownerHelperEnv)
	if mode != "error" && mode != "panic" {
		t.Skip("only run by TestCallFromOwnerFailsFast")
	}
	PanicOnOwnerWait = mode == "panic"
	go func() {
		time.Sleep(3 * time.Second)
		fmt.Println("DEADLOCK: a wait from an owner did not return within 3s")
		os.Exit(3)
	}()
	w := Config{Provider: NopProvider{}, Generator: NopGenerator{}}.New()
	other := Config{Provider: NopProvider{}, Generator: NopGenerator{}}.New()
	syncWorld := Config{Synchronous: true, Provider: NopProvider{}, Generator: NopGenerator{}}.New()
	ctx := context.Background()
	var ran atomic.Bool
	if err := w.Do(func(*Tx) {
		_, err := Call(ctx, w, func(*Tx) (int, error) {
			ran.Store(true)
			return 1, nil
		})
		fmt.Printf("Call on its own world: %v\n", err)
		// Callbacks that cannot finish before the caller looks: one that
		// did finish was never waited for.
		_, err = Call(ctx, other, func(*Tx) (int, error) {
			time.Sleep(100 * time.Millisecond)
			return 1, nil
		})
		fmt.Printf("Call on another world: %v\n", err)
		fmt.Printf("Task.Wait: %v\n", other.Do(func(*Tx) { time.Sleep(100 * time.Millisecond) }).Wait(ctx))
		fmt.Printf("Close: %v\n", other.Close())
		v, err := Call(ctx, syncWorld, func(*Tx) (int, error) { return 1, nil })
		fmt.Printf("Call on a synchronous world: %v %d\n", err, v)
	}).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	// The task of the Call on its own world was cancelled: it never runs.
	if err := w.Do(func(*Tx) {}).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("the call's task ran: %v\n", ran.Load())
	for _, w := range []*World{w, other, syncWorld} {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCallFromRegionWorkerFailsFast checks that an entity ticking in a region
// phase, on the owner or on a worker of its own, fails to call another world
// with ErrOwnerWait: the owner of that world may be waiting for this one,
// which does not run until the phase ends.
func TestCallFromRegionWorkerFailsFast(t *testing.T) {
	if os.Getenv(ownerHelperEnv) != "" {
		t.Skip("in the subprocess")
	}
	out, code := runOwnerHelper(t, "TestRegionWaitHelper", "region")
	if code != 0 {
		t.Fatalf("the waits from regions exited with status %d, want 0:\n%s", code, out)
	}
	if n := strings.Count(out, "region call: "+ErrOwnerWait.Error()+"\n"); n != 2 {
		t.Errorf("%d calls from regions failed with ErrOwnerWait, want 2:\n%s", n, out)
	}
}

// TestRegionWaitHelper is the subprocess of TestCallFromRegionWorkerFailsFast.
func TestRegionWaitHelper(t *testing.T) {
	if os.Getenv(ownerHelperEnv) != "region" {
		t.Skip("only run by TestCallFromRegionWorkerFailsFast")
	}
	PanicOnOwnerWait = false
	go func() {
		time.Sleep(10 * time.Second)
		fmt.Println("DEADLOCK: the entities did not tick within 10s")
		os.Exit(3)
	}()
	w := Config{Regions: true, Provider: NopProvider{}, Generator: NopGenerator{}}.New()
	other := Config{Provider: NopProvider{}, Generator: NopGenerator{}}.New()
	ctx := context.Background()
	// A call that cannot finish before the caller looks: one that did
	// finish was never waited for.
	slow := func(*Tx) (int, error) {
		time.Sleep(100 * time.Millisecond)
		return 1, nil
	}

	var (
		mu      sync.Mutex
		done    sync.WaitGroup
		workers = map[uint64]bool{}
	)
	done.Add(2)
	// call returns the tick of an entity that calls other once.
	call := func() func(tx *Tx, e *testEntity) {
		var once sync.Once
		return func(tx *Tx, _ *testEntity) {
			once.Do(func() {
				_, err := Call(ctx, other, slow)
				mu.Lock()
				workers[goroutineID()] = tx.region != nil
				fmt.Printf("region call: %v\n", err)
				mu.Unlock()
				done.Done()
			})
		}
	}
	positions := []mgl64.Vec3{{8.5, 4, 8.5}, {1024 + 8.5, 4, 8.5}}
	<-w.Exec(func(tx *Tx) {
		for _, pos := range positions {
			// A forced chunk ticks its entities without a loader, once the
			// chunks around it are ready.
			c := chunkPosFromVec3(pos)
			if err := tx.ForceChunk(c, true); err != nil {
				t.Error(err)
			}
			for dx := int32(-2); dx <= 2; dx++ {
				for dz := int32(-2); dz <= 2; dz++ {
					tx.chunk(ChunkPos{c[0] + dx, c[1] + dz})
				}
			}
			tx.AddEntity(EntitySpawnOpts{Position: pos}.New(testEntityType{}, regionProbeConfig{tick: call()}))
		}
	})
	done.Wait()
	mu.Lock()
	t.Logf("the entities ticked on %d goroutines: %v", len(workers), workers)
	mu.Unlock()
	if w.RegionPhases() == 0 {
		t.Error("the entities did not tick in a region phase")
	}
	for _, w := range []*World{w, other} {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
