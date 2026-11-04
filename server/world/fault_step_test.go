package world

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

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
			t.Fatalf("night %d: faults = %v, want %d", night, faults, night)
		}
		f := faults[night-1]
		if f.Kind != FaultEntity || f.Entity != badHandle || f.Type != "dragonfly:test_sleeper" || f.Value != "test: wake" || f.Count != night || f.Quarantined != (night == FaultLimit) {
			t.Fatalf("night %d: fault = %+v, want one of the sleeper, counted %d", night, f, night)
		}
	}
	if q := w.Quarantined(); len(q) != 1 || q[0].Entity != badHandle {
		t.Fatalf("Quarantined = %v, want the sleeper", q)
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
		t.Fatalf("the world failed: %v", f)
	}
	if n := len(sink.all(FaultSubsystem)); n != 0 {
		t.Fatalf("%d faults of a subsystem, want none", n)
	}
}
