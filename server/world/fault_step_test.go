package world

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// briefFaults returns the kind, type, value, count and quarantine of each
// fault, without its stack.
func briefFaults(faults []Fault) []string {
	out := make([]string, len(faults))
	for i, f := range faults {
		out[i] = fmt.Sprintf("%v %v %v count=%d quarantined=%v", f.Kind, f.Type, f.Value, f.Count, f.Quarantined)
	}
	return out
}

// sleeperTestState is the state of a sleeperTestEntity, shared by every
// transaction it is opened in.
type sleeperTestState struct {
	sleeping bool
	wakes    int
	// panics makes Wake panic before the sleeper wakes.
	panics bool
}

// sleeperTestConfig spawns a sleeperTestEntity of a state.
type sleeperTestConfig struct{ state *sleeperTestState }

func (c sleeperTestConfig) Apply(data *EntityData) { data.Data = c }

type sleeperTestType struct{ testEntityType }

func (sleeperTestType) Open(tx *Tx, handle *EntityHandle, data *EntityData) Entity {
	return sleeperTestEntity{testEntity: testEntityType{}.Open(tx, handle, data).(*testEntity)}
}

func (sleeperTestType) EncodeEntity() string { return "dragonfly:test_sleeper" }

// sleeperTestEntity is an entity that sleeps, as players do.
type sleeperTestEntity struct{ *testEntity }

func (e sleeperTestEntity) state() *sleeperTestState {
	return e.data.Data.(sleeperTestConfig).state
}

func (e sleeperTestEntity) Name() string                      { return "sleeper" }
func (e sleeperTestEntity) UUID() uuid.UUID                   { return e.handle.UUID() }
func (e sleeperTestEntity) Messaget(chat.Translation, ...any) {}
func (e sleeperTestEntity) SendSleepingIndicator(int, int)    {}
func (e sleeperTestEntity) Sleep(cube.Pos)                    { e.state().sleeping = true }
func (e sleeperTestEntity) Sleeping() (cube.Pos, bool)        { return cube.Pos{}, e.state().sleeping }
func (e sleeperTestEntity) Wake() {
	s := e.state()
	s.wakes++
	if s.panics {
		panic("test: wake")
	}
	s.sleeping = false
}

// A panic waking a sleeper as the night is skipped is blamed on the sleeper
// as a panic of its own tick, not on the step of the tick: the other
// sleepers wake, the night is skipped, and the sleeper is quarantined after
// FaultLimit panics (D1). The world never fails for it.
func TestSleeperWakePanicIsBlamedOnTheSleeper(t *testing.T) {
	var sink faultSink
	rec := newFaultRecorder()
	w := sink.configure(Config{Synchronous: true, RandomTickSpeed: -1}, rec).New()
	defer func() { _ = w.Close() }()

	bad, good := &sleeperTestState{panics: true}, &sleeperTestState{}
	badHandle := EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}.New(sleeperTestType{}, sleeperTestConfig{bad})
	goodHandle := EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, 0.5}}.New(sleeperTestType{}, sleeperTestConfig{good})
	<-w.Exec(func(tx *Tx) {
		tx.AddEntity(badHandle)
		tx.AddEntity(goodHandle)
	})
	for night := 1; night <= FaultLimit; night++ {
		w.SetTime(int(w.Time()/TimeFull*TimeFull + 18000))
		bad.sleeping, good.sleeping = true, true
		w.SetRequiredSleepDuration(time.Second / 20)
		advance(t, w)

		if good.sleeping || good.wakes != night {
			t.Fatalf("night %d: the other sleeper woke %d times and sleeps: %v", night, good.wakes, good.sleeping)
		}
		if w.Time()%TimeFull >= 18000 {
			t.Fatalf("night %d: time = %d, the night was not skipped", night, w.Time())
		}
		faults := sink.all(0)
		if len(faults) != night {
			t.Fatalf("night %d: faults = %v, want %d", night, briefFaults(faults), night)
		}
		f := faults[night-1]
		if f.Kind != FaultEntity || f.Entity != badHandle || f.Type != "dragonfly:test_sleeper" || f.Value != "test: wake" || f.Count != night || f.Quarantined != (night == FaultLimit) {
			t.Fatalf("night %d: fault = %v, want one of the sleeper, counted %d", night, briefFaults([]Fault{f}), night)
		}
	}
	if q := w.Quarantined(); len(q) != 1 || q[0].Entity != badHandle {
		t.Fatalf("Quarantined = %v, want the sleeper", briefFaults(q))
	}
	// A sleeper in quarantine is no longer woken, and the nights still pass.
	w.SetTime(int(w.Time()/TimeFull*TimeFull + 18000))
	good.sleeping = true
	w.SetRequiredSleepDuration(time.Second / 20)
	advance(t, w)
	if bad.wakes != FaultLimit || good.sleeping || w.Time()%TimeFull >= 18000 {
		t.Fatalf("after quarantine: woken %d times, want %d; the other sleeps: %v; time %d", bad.wakes, FaultLimit, good.sleeping, w.Time())
	}
	if f := sink.fatals(); len(f) != 0 {
		t.Fatalf("the world failed: %v", briefFaults(f))
	}
	if n := len(sink.all(FaultSubsystem)); n != 0 {
		t.Fatalf("%d faults of a subsystem, want none", n)
	}
}

// A panic advancing the clock of the world (its tick, time, weather,
// border and sleep), which nothing can be blamed for, fails the world (D3)
// and releases the lock of its settings: the world still closes and saves,
// and what reads its settings does not wait forever.
func TestClockPanicFailsTheWorldAndReleasesItsSettings(t *testing.T) {
	isolated(t, func(t *testing.T) {
		var sink faultSink
		rec := newFaultRecorder()
		prov := &storeCountingProvider{}
		other := cancelBatchTestBlock{name: "test:other", id: 52, tick: func(cube.Pos, *Tx) {}}
		w := sink.configure(Config{Provider: prov, Blocks: testBlockRegistry(other), RandomTickSpeed: -1}, rec).New()
		closed := false
		defer func() {
			if !closed {
				_ = w.Close()
			}
		}()
		loadAround(t, w, mgl64.Vec3{}, 1)
		var ticks atomic.Int32
		within(t, 10*time.Second, w.Exec(func(tx *Tx) {
			tx.SetBlock(cube.Pos{0, 64, 0}, other, nil)
			w.testClockHook = func() {
				if ticks.Add(1) == 3 {
					panic("test: clock")
				}
			}
		}), "arming the hook")
		waitFor(t, 10*time.Second, "the world failing", func() bool { return len(sink.fatals()) != 0 })
		if fatal := sink.fatals(); len(fatal) != 1 || fatal[0].Kind != FaultFatal || fatal[0].Value != "test: clock" {
			t.Fatalf("Config.Fatal calls = %v, want one for the panic of the clock", briefFaults(fatal))
		}
		done := make(chan struct{})
		go func() {
			_ = w.Time()
			_ = w.Border()
			close(done)
		}()
		within(t, 10*time.Second, done, "reading the settings of the failed world")

		stored := prov.stores.Load()
		closed = true
		closing := make(chan struct{})
		go func() {
			if err := w.Close(); err != nil {
				t.Errorf("close: %v", err)
			}
			close(closing)
		}()
		within(t, 30*time.Second, closing, "closing the failed world")
		if prov.stores.Load() <= stored {
			t.Fatal("closing the failed world saved no column")
		}
	})
}

// timeTestViewer is a viewer counting the times it is shown the time, and
// told it failed. Once panics is set, being shown the time panics.
type timeTestViewer struct {
	NopViewer
	panics        *atomic.Bool
	shown, failed *atomic.Int32
}

func (v timeTestViewer) ViewTime(int) {
	if v.panics.Load() {
		panic("test: view time")
	}
	v.shown.Add(1)
}
func (v timeTestViewer) ViewFailed() { v.failed.Add(1) }

// A viewer that panics as the tick shows it the time or the weather is
// blamed for it, as one shown the blocks of a column is, and told
// (FailingViewer): the world does not fail, and the other viewers are still
// shown the time.
func TestViewerPanicShowingTheTimeFailsTheViewer(t *testing.T) {
	var sink faultSink
	w := sink.configure(Config{Synchronous: true, RandomTickSpeed: -1}, newFaultRecorder()).New()
	defer func() { _ = w.Close() }()
	bad := timeTestViewer{panics: new(atomic.Bool), shown: new(atomic.Int32), failed: new(atomic.Int32)}
	good := timeTestViewer{panics: new(atomic.Bool), shown: new(atomic.Int32), failed: new(atomic.Int32)}
	loaders := []*Loader{NewLoader(1, w, bad), NewLoader(1, w, good)}
	<-w.Exec(func(tx *Tx) {
		for _, l := range loaders {
			l.Move(tx, mgl64.Vec3{8, 64, 8})
		}
	})
	defer func() {
		<-w.Exec(func(tx *Tx) {
			for _, l := range loaders {
				l.Close(tx)
			}
		})
	}()
	shown := good.shown.Load()
	bad.panics.Store(true)
	for range 40 {
		advance(t, w)
	}
	if f := sink.fatals(); len(f) != 0 {
		t.Fatalf("the world failed: %v", briefFaults(f))
	}
	if n := good.shown.Load() - shown; n != 2 {
		t.Fatalf("the other viewer was shown the time %d times, want 2", n)
	}
	if n := bad.failed.Load(); n != 2 {
		t.Fatalf("the panicking viewer was told it failed %d times, want 2", n)
	}
	if faults := sink.all(0); len(faults) != 2 || faults[0].Kind != FaultViewer || faults[0].Type != "world.timeTestViewer" || faults[0].Value != "test: view time" {
		t.Fatalf("faults = %v, want two of the viewer", briefFaults(faults))
	}
}

// barTestType is a test entity listed as a player, whose boss bars panic.
type barTestType struct{ testEntityType }

func (barTestType) Open(tx *Tx, handle *EntityHandle, data *EntityData) Entity {
	return barTestEntity{testEntity: testEntityType{}.Open(tx, handle, data).(*testEntity)}
}

func (barTestType) EncodeEntity() string { return "minecraft:player" }

type barTestEntity struct{ *testEntity }

func (barTestEntity) SendBossBar(bossbar.BossBar) { panic("test: boss bar") }
func (barTestEntity) RemoveBossBarID(string)      { panic("test: boss bar") }

// A panic showing a player the bar of a raid, in the step of the tick that
// ticks raids, is blamed on the player, not on the step: it is counted and
// quarantined at the limit (a real player is disconnected), and the world
// never fails, though the bar is shown every tick.
func TestRaidBarPanicIsBlamedOnThePlayer(t *testing.T) {
	var sink faultSink
	w := sink.configure(Config{Synchronous: true, RandomTickSpeed: -1}, newFaultRecorder()).New()
	defer func() { _ = w.Close() }()
	h := EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}.New(barTestType{}, regionProbeConfig{tick: func(*Tx, *testEntity) {}})
	<-w.Exec(func(tx *Tx) {
		tx.AddEntity(h)
		w.raids[1] = &Raid{id: 1, status: RaidVictory, members: map[uuid.UUID]struct{}{}, heroes: map[uuid.UUID]struct{}{}}
	})
	for range stepFaultLimit + 5 {
		advance(t, w)
	}
	faults := sink.all(0)
	if len(faults) != FaultLimit {
		t.Fatalf("faults = %v, want %d of the player", briefFaults(faults), FaultLimit)
	}
	for i, f := range faults {
		if f.Kind != FaultEntity || f.Entity != h || f.Value != "test: boss bar" || f.Count != i+1 {
			t.Fatalf("fault %d = %v, want one of the player", i, briefFaults([]Fault{f}))
		}
	}
	if !faults[FaultLimit-1].Quarantined {
		t.Fatal("the player was not quarantined")
	}
	if f := sink.fatals(); len(f) != 0 {
		t.Fatalf("the world failed: %v", briefFaults(f))
	}
}

// A viewer that panics as a skipped night shows it the new time is blamed
// for it, not the step of the tick: the night is still skipped, rain and
// all, and the sleepers wake.
func TestViewerPanicAtNightSkipFailsTheViewer(t *testing.T) {
	var sink faultSink
	w := sink.configure(Config{Synchronous: true, RandomTickSpeed: -1}, newFaultRecorder()).New()
	defer func() { _ = w.Close() }()
	bad := timeTestViewer{panics: new(atomic.Bool), shown: new(atomic.Int32), failed: new(atomic.Int32)}
	l := NewLoader(1, w, bad)
	<-w.Exec(func(tx *Tx) { l.Move(tx, mgl64.Vec3{8, 64, 8}) })
	defer func() { <-w.Exec(func(tx *Tx) { l.Close(tx) }) }()
	sleeper := &sleeperTestState{sleeping: true}
	<-w.Exec(func(tx *Tx) {
		tx.AddEntity(EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}.New(sleeperTestType{}, sleeperTestConfig{sleeper}))
	})
	// The next tick is not one of the ticks that show viewers the time.
	for w.CurrentTick()%20 != 1 {
		advance(t, w)
	}
	w.SetTime(18000)
	w.StartRaining(time.Hour)
	w.SetRequiredSleepDuration(time.Second / 20)
	bad.panics.Store(true)
	advance(t, w)
	if sleeper.sleeping || w.Time()%TimeFull >= 18000 || w.raining() {
		t.Fatalf("the night was not skipped: asleep %v, time %d, raining %v", sleeper.sleeping, w.Time(), w.raining())
	}
	if n := bad.failed.Load(); n != 1 {
		t.Fatalf("the viewer was told it failed %d times, want once", n)
	}
	if faults := sink.all(0); len(faults) != 1 || faults[0].Kind != FaultViewer || faults[0].Value != "test: view time" {
		t.Fatalf("faults = %v, want one of the viewer", briefFaults(faults))
	}
}
