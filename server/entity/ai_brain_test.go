package entity

import (
	"testing"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestBrainDataOrientedSemantics verifies activity bitmasks, expiring memory
// bitsets, mid-tick activity switches during startBehaviors, priority-ordered
// running buffer tracking, and zero-allocation ticks.
func TestBrainDataOrientedSemantics(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
		m := testSpawnMob(t, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))

		br := newBrain()
		br.register(memIsPanicking)
		br.register(memAttackCoolingDown)
		br.register(memGolemDetectedRecently)

		var order []string
		// Priority 0 core oneShot switches activity to activityWork mid-tick.
		br.addActivityWith(activityCore, []prioritizedBehavior{
			{0, &oneShot{trigger: func(m *Mob, _ int64) bool {
				if br.has(memIsPanicking) && !br.isActive(activityWork) {
					order = append(order, "core-switch")
					br.setActive(activityWork)
					return true
				}
				return false
			}}},
		}, nil, nil)

		// Idle activity (priority 1) erases memAttackCoolingDown on stop.
		br.addActivityWith(activityIdle, []prioritizedBehavior{
			{1, &behavior{
				minDuration: 10, maxDuration: 10,
				start:       func(*Mob, int64) { order = append(order, "idle-start") },
				tick:        func(*Mob, int64) { order = append(order, "idle-tick") },
				canStillUse: func(*Mob, int64) bool { return true },
			}},
		}, nil, []memoryType{memAttackCoolingDown})

		// Work activity (priority 2) should start on the very same tick core switches to it.
		br.addActivityWith(activityWork, []prioritizedBehavior{
			{2, &behavior{
				minDuration: 10, maxDuration: 10,
				start:       func(*Mob, int64) { order = append(order, "work-start") },
				tick:        func(*Mob, int64) { order = append(order, "work-tick") },
				canStillUse: func(*Mob, int64) bool { return true },
			}},
		}, nil, nil)

		// Verify expiring memories: ttl=1 survives 1 tick (becomes 0), clears on 2nd tick;
		// ttl=0 clears on 1st tick.
		br.setWithExpiry(memAttackCoolingDown, true, 1)
		br.setWithExpiry(memGolemDetectedRecently, true, 0)

		if act, ok := br.activeNonCore(); !ok || act != activityIdle {
			t.Fatalf("activeNonCore = (%v, %v), want (activityIdle, true)", act, ok)
		}

		// Tick 1: memGolemDetectedRecently expires immediately; memAttackCoolingDown decrements to 0;
		// idle starts and ticks.
		br.tick(m)
		if br.has(memGolemDetectedRecently) {
			t.Fatal("ttl=0 memory did not clear on first tick")
		}
		if !br.has(memAttackCoolingDown) || br.timeUntilExpiry(memAttackCoolingDown) != 0 {
			t.Fatalf("ttl=1 memory after 1 tick: has=%v ttl=%d", br.has(memAttackCoolingDown), br.timeUntilExpiry(memAttackCoolingDown))
		}
		if len(order) != 2 || order[0] != "idle-start" || order[1] != "idle-tick" {
			t.Fatalf("tick 1 order = %v, want [idle-start idle-tick]", order)
		}

		// Copy memory into a second brain and verify ttl and expiring bit are preserved.
		br2 := newBrain()
		br2.copyMemoryFrom(br, memAttackCoolingDown)
		if !br2.has(memAttackCoolingDown) || br2.timeUntilExpiry(memAttackCoolingDown) != 0 {
			t.Fatalf("copied memory lost value/ttl: has=%v ttl=%d", br2.has(memAttackCoolingDown), br2.timeUntilExpiry(memAttackCoolingDown))
		}
		br2.tick(m)
		if br2.has(memAttackCoolingDown) {
			t.Fatal("copied expiring memory did not expire on tick")
		}

		// Tick 2: set memIsPanicking so core-switch switches Idle -> Work mid-tick.
		// Work must start in the same tick, and both Idle (already running, priority 1)
		// and Work (newly started, priority 2) must tick in priority order.
		order = order[:0]
		br.set(memIsPanicking, true)
		br.set(memAttackCoolingDown, true) // should be erased by Idle's eraseOnStop when switching to Work!
		br.tick(m)
		if br.has(memAttackCoolingDown) {
			t.Fatal("eraseOnStop did not erase memAttackCoolingDown on activity switch")
		}
		if act, ok := br.activeNonCore(); !ok || act != activityWork {
			t.Fatalf("activeNonCore after switch = (%v, %v), want (activityWork, true)", act, ok)
		}
		want := []string{"core-switch", "work-start", "idle-tick", "work-tick"}
		if len(order) != len(want) {
			t.Fatalf("tick 2 order = %v, want %v", order, want)
		}
		for i := range want {
			if order[i] != want[i] {
				t.Fatalf("tick 2 order[%d] = %q, want %q (full=%v)", i, order[i], want[i], order)
			}
		}

		// Verify steady-state Brain.tick allocates 0 times per tick.
		br.stopAll(m)
		br.erase(memIsPanicking)
		br.tick(m) // warm up runningBuf / tickBuf / activeCache
		allocs := testing.AllocsPerRun(50, func() {
			br.tick(m)
		})
		if allocs != 0 {
			t.Fatalf("steady-state Brain.tick allocs/op = %v, want 0", allocs)
		}
	})
}

type sharedStateTestViewer struct {
	world.NopViewer
	sharedCalls int
	plainCalls  int
	lastVal     any
}

func (v *sharedStateTestViewer) ViewEntityState(world.Entity) {
	v.plainCalls++
}

func (v *sharedStateTestViewer) ViewEntityStateShared(_ world.Entity, b *world.Broadcast) {
	v.sharedCalls++
	if b.Load() == nil {
		b.Store("encoded-once")
	}
	v.lastVal = b.Load()
}

// TestEntUpdateStateUsesSharedBroadcast verifies Ent.updateState dispatches via
// world.BroadcastEntityState so EntityStateSharedViewer viewers share one Broadcast.
func TestEntUpdateStateUsesSharedBroadcast(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	v1 := &sharedStateTestViewer{}
	v2 := &sharedStateTestViewer{}
	l1 := world.NewLoader(2, w, v1)
	l2 := world.NewLoader(2, w, v2)

	var zh *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
		l1.Move(tx, mgl64.Vec3{0.5, 64, 0.5})
		l2.Move(tx, mgl64.Vec3{0.5, 64, 0.5})
		l1.Load(tx, 25)
		l2.Load(tx, 25)
		zh = tx.AddEntity(NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}})).H()
	})
	for range 3 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			l1.Load(tx, 25)
			l2.Load(tx, 25)
		})
	}
	<-w.Exec(func(tx *world.Tx) {
		e, ok := zh.Entity(tx)
		if !ok {
			t.Fatal("zombie missing from world")
		}
		v1.sharedCalls, v2.sharedCalls, v1.plainCalls, v2.plainCalls = 0, 0, 0, 0
		e.(*zombie).SetNameTag("Shared")
	})
	<-w.Exec(func(tx *world.Tx) {
		l1.Close(tx)
		l2.Close(tx)
	})

	if v1.sharedCalls != 1 || v2.sharedCalls != 1 {
		t.Fatalf("sharedCalls = (%d, %d), want (1, 1)", v1.sharedCalls, v2.sharedCalls)
	}
	if v1.plainCalls != 0 || v2.plainCalls != 0 {
		t.Fatalf("plainCalls = (%d, %d), want (0, 0)", v1.plainCalls, v2.plainCalls)
	}
	if v1.lastVal != "encoded-once" || v2.lastVal != "encoded-once" {
		t.Fatalf("broadcast value = (%v, %v), want encoded-once", v1.lastVal, v2.lastVal)
	}
}
