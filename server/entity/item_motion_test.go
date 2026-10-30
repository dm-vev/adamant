package entity

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestItemMovementMatchesNativeAirSteps(t *testing.T) {
	testItemMotionTrace(t, "air")
}

func TestItemLandingAppliesFrictionAfterCollision(t *testing.T) {
	testItemMotionTrace(t, "landing")
}

func testItemMotionTrace(t *testing.T, name string) {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_item_motion.json")
	if err != nil {
		t.Fatal(err)
	}
	type point struct{ X, Y, Z float64 }
	vec := func(p point) mgl64.Vec3 { return mgl64.Vec3{p.X, p.Y, p.Z} }
	type sample struct {
		Position, Velocity point
		OnGround           bool `json:"on_ground"`
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Name    string
			Spawn   sample
			Samples []sample
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 2 {
		t.Fatal("incomplete original-BDS item motion fixture")
	}
	var initial sample
	var steps []sample
	for _, c := range native.Scenarios {
		if c.Name == name {
			initial, steps = c.Spawn, c.Samples
		}
	}
	if len(steps) != 3 {
		t.Fatal("missing original-BDS motion steps")
	}
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Do(func(tx *world.Tx) {
		cell := cube.PosFromVec3(vec(initial.Position))
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{cell[0] + x, 64, cell[2] + z}, block.Stone{}, nil)
			}
		}
		e := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: vec(initial.Position), Velocity: vec(initial.Velocity)}, item.NewStack(item.Diamond{}, 1))).(*Ent)
		for tick, want := range steps {
			e.Tick(tx, int64(tick))
			if e.Position() != vec(want.Position) || e.Velocity() != vec(want.Velocity) || e.OnGround() != want.OnGround {
				t.Errorf("tick %d: position=%v velocity=%v grounded=%t, original BDS=%v %v %t", tick+1, e.Position(), e.Velocity(), e.OnGround(), vec(want.Position), vec(want.Velocity), want.OnGround)
			}
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestItemMergePreservesExistingActors(t *testing.T) {
	for _, test := range []struct {
		name                  string
		first, second         int
		wantFirst, wantSecond int
		merged                bool
	}{
		{name: "smaller into larger", first: 3, second: 5, wantSecond: 8, merged: true},
		{name: "larger survives its own tick", first: 5, second: 3, wantFirst: 8, merged: true},
		{name: "partial merge retains both", first: 40, second: 50, wantFirst: 26, wantSecond: 64, merged: true},
		{name: "full stack unchanged", first: 64, second: 3, wantFirst: 64, wantSecond: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			t.Cleanup(func() { _ = w.Close() })
			if err := w.Do(func(tx *world.Tx) {
				first := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{4, 10, 4}, Velocity: mgl64.Vec3{0.1, 0.2, 0}}, item.NewStack(item.Diamond{}, test.first))).(*Ent)
				second := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{4.25, 10, 4}, Velocity: mgl64.Vec3{-0.1, -0.2, 0}}, item.NewStack(item.Diamond{}, test.second))).(*Ent)
				first.data.Age, second.data.Age = time.Second, 2*time.Second
				if got := first.Behaviour().(*ItemBehaviour).merge(first, second, tx); got != test.merged {
					t.Errorf("merge=%v, want %v", got, test.merged)
				}
				for n, row := range []struct {
					e        *Ent
					count    int
					pos, vel mgl64.Vec3
				}{
					{first, test.wantFirst, mgl64.Vec3{4, 10, 4}, mgl64.Vec3{0.1, 0.2, 0}},
					{second, test.wantSecond, mgl64.Vec3{4.25, 10, 4}, mgl64.Vec3{-0.1, -0.2, 0}},
				} {
					if row.e.H().Closed() != (row.count == 0) {
						t.Errorf("actor %d: closed=%v, want count %d", n, row.e.H().Closed(), row.count)
					}
					if row.count > 0 && (row.e.Behaviour().(*ItemBehaviour).Item().Count() != row.count || row.e.Position() != row.pos || row.e.Velocity() != row.vel) {
						t.Errorf("actor %d changed identity, count or motion: count=%d position=%v velocity=%v", n, row.e.Behaviour().(*ItemBehaviour).Item().Count(), row.e.Position(), row.e.Velocity())
					}
					if row.count > 0 && test.merged && row.e.Age() != time.Second {
						t.Errorf("actor %d age=%v, want 1s", n, row.e.Age())
					}
				}
				actors := 0
				for range tx.Entities() {
					actors++
				}
				wantActors := 0
				if test.wantFirst > 0 {
					wantActors++
				}
				if test.wantSecond > 0 {
					wantActors++
				}
				if actors != wantActors {
					t.Errorf("actors=%d, want %d", actors, wantActors)
				}
			}).Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPartialItemPickupKeepsActor(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Do(func(tx *world.Tx) {
		pos, vel := mgl64.Vec3{4, 10, 4}, mgl64.Vec3{0.1, 0.2, 0}
		e := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos, Velocity: vel}, item.NewStack(item.Diamond{}, 5))).(*Ent)
		e.data.Age = time.Second
		e.Behaviour().(*ItemBehaviour).collect(e, partialItemCollector{Entity: e}, tx)
		if e.H().Closed() || e.Behaviour().(*ItemBehaviour).Item().Count() != 3 || e.Position() != pos || e.Velocity() != vel || e.Age() != time.Second {
			t.Errorf("partial pickup replaced or reset the remaining item: closed=%v count=%d position=%v velocity=%v age=%v", e.H().Closed(), e.Behaviour().(*ItemBehaviour).Item().Count(), e.Position(), e.Velocity(), e.Age())
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type partialItemCollector struct{ world.Entity }

func (partialItemCollector) Collect(item.Stack) (int, bool) { return 2, true }

func TestPartialHopperCollectionKeepsItemActor(t *testing.T) {
	settings := (world.NopProvider{}).Settings()
	settings.CurrentTick = 1 // World.New reaches the even transfer step at 2.
	w := world.Config{Synchronous: true, Provider: world.NopProvider{Set: settings}}.New()
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Do(func(tx *world.Tx) {
		pos := cube.Pos{4, 0, 4}
		hopper := block.NewHopper()
		inv := hopper.Inventory(tx, pos)
		for slot := range 4 {
			_ = inv.SetItem(slot, item.NewStack(block.Stone{}, 64))
		}
		_ = inv.SetItem(4, item.NewStack(item.Diamond{}, 62))
		tx.SetBlock(pos, hopper, nil)
		e := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{4.25, 1.1, 4.3}}, item.NewStack(item.Diamond{}, 5))).(*Ent)
		control := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: e.Position()}, item.NewStack(item.Stick{}, 1))).(*Ent)
		if tx.CurrentTick() != 2 {
			t.Fatal("expected an even hopper transfer step")
		}
		// Pickup belongs to the block actor, before the item movement phase.
		hopper.Tick(tx.CurrentTick(), pos, tx)
		e.Tick(tx, 0)
		control.Tick(tx, 0)
		if e.H().Closed() || e.Behaviour().(*ItemBehaviour).Item().Count() != 3 || e.Position() != control.Position() || e.Velocity() != control.Velocity() {
			t.Errorf("partial hopper pickup replaced or moved the item: closed=%v count=%d position=%v", e.H().Closed(), e.Behaviour().(*ItemBehaviour).Item().Count(), e.Position())
		}
		if got, _ := inv.Item(4); got.Count() != 64 {
			t.Errorf("hopper diamond count=%d, want 64", got.Count())
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
