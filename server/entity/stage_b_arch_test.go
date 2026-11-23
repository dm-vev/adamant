package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/ecs"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type stageBStaticGoal struct {
	flags       GoalFlag
	canUse      bool
	canContinue bool
	everyTick   bool

	starts int
	stops  int
	ticks  int
}

func (g *stageBStaticGoal) Flags() GoalFlag               { return g.flags }
func (g *stageBStaticGoal) CanUse(*Mob) bool              { return g.canUse }
func (g *stageBStaticGoal) CanContinueToUse(*Mob) bool    { return g.canContinue }
func (g *stageBStaticGoal) Start(*Mob)                    { g.starts++ }
func (g *stageBStaticGoal) Stop(*Mob)                     { g.stops++ }
func (g *stageBStaticGoal) Tick(*Mob)                     { g.ticks++ }
func (g *stageBStaticGoal) RequiresUpdateEveryTick() bool { return g.everyTick }

type stageBDynamicInterruptGoal struct {
	flags         GoalFlag
	canUse        bool
	interruptable bool
	starts        int
	stops         int
}

func (g *stageBDynamicInterruptGoal) Flags() GoalFlag       { return g.flags }
func (g *stageBDynamicInterruptGoal) CanUse(*Mob) bool      { return g.canUse }
func (g *stageBDynamicInterruptGoal) IsInterruptable() bool { return g.interruptable }
func (g *stageBDynamicInterruptGoal) Start(*Mob)            { g.starts++ }
func (g *stageBDynamicInterruptGoal) Stop(*Mob)             { g.stops++ }

type stageBMinimalGoal struct {
	canUse bool
}

func (g *stageBMinimalGoal) Flags() GoalFlag  { return GoalLook }
func (g *stageBMinimalGoal) CanUse(*Mob) bool { return g.canUse }

func TestStageBGoalSelectorFlagsAndHooks(t *testing.T) {
	t.Parallel()
	var s GoalSelector

	low := &stageBStaticGoal{flags: GoalMove, canUse: true, canContinue: true}
	dyn := &stageBDynamicInterruptGoal{flags: GoalMove, canUse: true, interruptable: false}
	look := &stageBMinimalGoal{canUse: true}

	s.Add(2, low)
	s.Add(1, dyn)
	s.Add(3, look)

	// On first tick, lower priority number `dyn` (priority 1) stops `low`?
	// Goals run in slice order: low starts (flag Move), then dyn sees locked Move -> not higher priority? Wait: dyn has priority 1 < 2, so it pre-empts low.
	// Wait: dyn has interruptable=false initially so once running it cannot be pre-empted until interruptable=true.
	high := &stageBStaticGoal{flags: GoalMove, canUse: false, canContinue: true, everyTick: true}
	s.Add(0, high)

	s.Tick(nil)
	if low.starts != 1 || low.stops != 1 {
		t.Fatalf("low starts/stops = (%d, %d), want (1, 1) after pre-emption by dyn", low.starts, low.stops)
	}
	if dyn.starts != 1 || dyn.stops != 0 {
		t.Fatalf("dyn starts/stops = (%d, %d), want (1, 0)", dyn.starts, dyn.stops)
	}
	if s.runningFlags != (GoalMove | GoalLook) {
		t.Fatalf("runningFlags = %#x, want %#x", s.runningFlags, GoalMove|GoalLook)
	}

	// Non-interruptable dyn blocks even priority-0 high goal.
	high.canUse = true
	s.Tick(nil)
	if high.starts != 0 || dyn.stops != 0 {
		t.Fatalf("non-interruptable dyn was pre-empted: high.starts=%d dyn.stops=%d", high.starts, dyn.stops)
	}

	// Dynamic toggle allows priority-0 high goal to replace dyn, and TickRunning(nil, false) skips non-everyTick goals.
	dyn.interruptable = true
	dyn.canUse = false
	s.Tick(nil)
	if high.starts != 1 || high.ticks != 1 || dyn.stops != 1 {
		t.Fatalf("high failed to pre-empt after interruptable=true: high=(%d,%d) dyn.stops=%d", high.starts, high.ticks, dyn.stops)
	}
	s.TickRunning(nil, false)
	if high.ticks != 2 {
		t.Fatalf("high.ticks = %d, want 2 on everyTick=true", high.ticks)
	}
	if !s.Running(func(g Goal) bool { return g == high }) {
		t.Fatal("expected high to report Running")
	}

	// Disabling GoalLook stops minimal goal `look` without needing an explicit Stop hook.
	s.SetControlFlag(GoalLook, false)
	s.Tick(nil)
	if s.runningFlags != GoalMove {
		t.Fatalf("runningFlags after disabling GoalLook = %#x, want %#x", s.runningFlags, GoalMove)
	}

	// Remove clears locked/runningFlags and stops high.
	s.Remove(nil, func(g Goal) bool { return g == high })
	if high.stops != 1 || s.runningFlags != 0 || s.Running(func(g Goal) bool { return g == high }) {
		t.Fatalf("Remove state invalid: high.stops=%d runningFlags=%#x", high.stops, s.runningFlags)
	}
}

func TestStageBFluidFlowStillSourceFastPath(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })

	<-w.Exec(func(tx *world.Tx) {
		reg := tx.World().BlockRegistry()

		// 3x3 still water pool with air step outside east edge.
		for x := -1; x <= 3; x++ {
			for z := -1; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		for x := 0; x <= 2; x++ {
			for z := 0; z <= 2; z++ {
				tx.SetLiquid(cube.Pos{x, 64, z}, block.Water{Depth: 8, Still: true})
			}
		}
		// Add a flowing drop-off outside east pool edge so edge source still flows while interior does not.
		tx.SetLiquid(cube.Pos{3, 64, 1}, block.Water{Depth: 7})

		// Interior cell has 4 matching still-source neighbors -> fast path returns zero matching fallback.
		interior := cube.Pos{1, 64, 1}
		il, _ := tx.Liquid(interior)
		irid, _ := tx.LiquidRID(interior)
		if got, want := fluidFlowInfoWithRID(tx, reg, interior, irid, liquidKindWater, 8, false), fluidFlowFallback(tx, interior, il); got != (mgl64.Vec3{}) || !got.ApproxEqualThreshold(want, 1e-9) {
			t.Fatalf("interior still water flow = %v, fallback = %v, want zero", got, want)
		}

		// Edge cell flows east into depth-7 water just like fallback.
		edge := cube.Pos{2, 64, 1}
		el, _ := tx.Liquid(edge)
		erid, _ := tx.LiquidRID(edge)
		gotEdge := fluidFlowInfoWithRID(tx, reg, edge, erid, liquidKindWater, 8, false)
		wantEdge := fluidFlowFallback(tx, edge, el)
		if !gotEdge.ApproxEqualThreshold(wantEdge, 1e-9) || gotEdge[0] <= 0 {
			t.Fatalf("edge water flow = %v, fallback = %v, want east flow", gotEdge, wantEdge)
		}

		// Center 3x3 still lava pool also matches fallback == zero and measureFluidsRange counts depth with zero sum.
		for x := 10; x <= 12; x++ {
			for z := 0; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				tx.SetLiquid(cube.Pos{x, 64, z}, block.Lava{Depth: 8, Still: true})
			}
		}
		lavaPos := cube.Pos{11, 64, 1}
		ll, _ := tx.Liquid(lavaPos)
		lrid, _ := tx.LiquidRID(lavaPos)
		if got, want := fluidFlowInfoWithRID(tx, reg, lavaPos, lrid, liquidKindLava, 8, false), fluidFlowFallback(tx, lavaPos, ll); got != (mgl64.Vec3{}) || !got.ApproxEqualThreshold(want, 1e-9) {
			t.Fatalf("interior still lava flow = %v, fallback = %v, want zero", got, want)
		}

		box := cube.Box(11.1, 64.0, 1.1, 11.9, 64.9, 1.9)
		fc := measureFluids(tx, box, true)
		if fc.lavaHeight <= 0 || fc.lava.count == 0 || fc.lava.sum != (mgl64.Vec3{}) {
			t.Fatalf("still lava fluidContact = %+v, want height>0 count>0 sum==zero", fc)
		}
	})
}

var stageBRoleKey = ecs.NewKey[string]("stage_b_test:role")

func TestStageBEntComponentsAndCustomGoals(t *testing.T) {
	t.Parallel()
	var nilEnt *Ent
	if nilEnt.Components() != nil {
		t.Fatal("expected (*Ent)(nil).Components() == nil")
	}

	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })

	goal := &stageBStaticGoal{flags: GoalLook, canUse: true, canContinue: true, everyTick: true}
	targetGoal := &stageBStaticGoal{flags: GoalTarget, canUse: true, canContinue: true, everyTick: true}

	<-w.Exec(func(tx *world.Tx) {
		for x := -1; x <= 2; x++ {
			for z := -1; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		h := NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}})
		ecs.Set(h.Components(), stageBRoleKey, "herder")

		cow := tx.AddEntity(h).(*Cow)
		if cow.Components() != h.Components() {
			t.Fatal("cow.Components() did not return handle's underlying *ecs.Bag")
		}
		if got, ok := ecs.Get(cow.Components(), stageBRoleKey); !ok || got != "herder" {
			t.Fatalf("ecs.Get(cow.Components()) = (%q, %v), want (%q, true)", got, ok, "herder")
		}
		if cow.MobAI() == nil {
			t.Fatal("expected Cow.MobAI() != nil")
		}

		cow.AddGoal(0, goal)
		cow.AddTargetGoal(0, targetGoal)
		cow.Tick(tx, 1)
	})

	if goal.starts != 1 || goal.ticks != 1 {
		t.Fatalf("custom goal starts/ticks = (%d, %d), want (1, 1)", goal.starts, goal.ticks)
	}
	if targetGoal.starts != 1 || targetGoal.ticks != 1 {
		t.Fatalf("custom target goal starts/ticks = (%d, %d), want (1, 1)", targetGoal.starts, targetGoal.ticks)
	}
}

type stageBBroadcastViewer struct {
	world.NopViewer
	sharedActions []*world.Broadcast
	sharedVels    []*world.Broadcast
	sharedItems   []*world.Broadcast
	sharedArmour  []*world.Broadcast
}

func (v *stageBBroadcastViewer) ViewEntityStateShared(world.Entity, *world.Broadcast) {}
func (v *stageBBroadcastViewer) ViewEntityActionShared(_ world.Entity, _ world.EntityAction, b *world.Broadcast) {
	v.sharedActions = append(v.sharedActions, b)
}
func (v *stageBBroadcastViewer) ViewEntityVelocityShared(_ world.Entity, _ mgl64.Vec3, b *world.Broadcast) {
	v.sharedVels = append(v.sharedVels, b)
}
func (v *stageBBroadcastViewer) ViewEntityItemsShared(_ world.Entity, b *world.Broadcast) {
	v.sharedItems = append(v.sharedItems, b)
}
func (v *stageBBroadcastViewer) ViewEntityArmourShared(_ world.Entity, b *world.Broadcast) {
	v.sharedArmour = append(v.sharedArmour, b)
}

type stageBUntrackedEntity struct{ e world.Entity }

func (u stageBUntrackedEntity) Close() error            { return u.e.Close() }
func (u stageBUntrackedEntity) H() *world.EntityHandle  { return u.e.H() }
func (u stageBUntrackedEntity) Position() mgl64.Vec3    { return u.e.Position() }
func (u stageBUntrackedEntity) Rotation() cube.Rotation { return u.e.Rotation() }

type stageBPlainViewer struct {
	world.NopViewer
	actions, vels, items, armour int
}

func (v *stageBPlainViewer) ViewEntityAction(world.Entity, world.EntityAction) { v.actions++ }
func (v *stageBPlainViewer) ViewEntityVelocity(world.Entity, mgl64.Vec3)       { v.vels++ }
func (v *stageBPlainViewer) ViewEntityItems(world.Entity)                      { v.items++ }
func (v *stageBPlainViewer) ViewEntityArmour(world.Entity)                     { v.armour++ }

func TestStageBSharedBroadcastViewers(t *testing.T) {
	t.Parallel()
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })

	bv1 := &stageBBroadcastViewer{}
	bv2 := &stageBBroadcastViewer{}
	pv := &stageBPlainViewer{}

	<-w.Exec(func(tx *world.Tx) {
		for _, v := range []world.Viewer{bv1, bv2, pv} {
			l := world.NewLoader(1, w, v)
			l.Move(tx, mgl64.Vec3{8, 64, 8})
			l.Load(tx, 9)
		}
	})

	<-w.Exec(func(tx *world.Tx) {
		cow := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 64, 8.5}})).(*Cow)
		// Reset counts after initial entity spawn visibility notifications.
		bv1.sharedActions, bv1.sharedVels, bv1.sharedItems, bv1.sharedArmour = nil, nil, nil, nil
		bv2.sharedActions, bv2.sharedVels, bv2.sharedItems, bv2.sharedArmour = nil, nil, nil, nil
		pv.actions, pv.vels, pv.items, pv.armour = 0, 0, 0, 0
		cow.viewAction(SwingArmAction{})
		viewEquipment(tx, cow)

		viewers := tx.Viewers(cow.Position())
		mov := &Movement{
			v:         viewers,
			releaseTx: tx,
			e:         stageBUntrackedEntity{cow},
			pos:       cow.Position(),
			vel:       mgl64.Vec3{0, 0.25, 0},
			dvel:      mgl64.Vec3{0, 0.25, 0},
		}
		mov.Send()
	})

	if len(bv1.sharedActions) != 1 || len(bv2.sharedActions) != 1 || bv1.sharedActions[0] != bv2.sharedActions[0] {
		t.Fatalf("sharedActions mismatch: %v vs %v", bv1.sharedActions, bv2.sharedActions)
	}
	if len(bv1.sharedItems) != 1 || len(bv2.sharedItems) != 1 || bv1.sharedItems[0] != bv2.sharedItems[0] {
		t.Fatalf("sharedItems mismatch: %v vs %v", bv1.sharedItems, bv2.sharedItems)
	}
	if len(bv1.sharedArmour) != 1 || len(bv2.sharedArmour) != 1 || bv1.sharedArmour[0] != bv2.sharedArmour[0] {
		t.Fatalf("sharedArmour mismatch: %v vs %v", bv1.sharedArmour, bv2.sharedArmour)
	}
	if len(bv1.sharedVels) != 1 || len(bv2.sharedVels) != 1 || bv1.sharedVels[0] != bv2.sharedVels[0] {
		t.Fatalf("sharedVels mismatch: %v vs %v", bv1.sharedVels, bv2.sharedVels)
	}
	if pv.actions != 1 || pv.items != 1 || pv.armour != 1 || pv.vels != 1 {
		t.Fatalf("plain viewer counts = (%d, %d, %d, %d), want all 1", pv.actions, pv.items, pv.armour, pv.vels)
	}
}
