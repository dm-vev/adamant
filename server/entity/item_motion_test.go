package entity

import (
	"context"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestItemMovementMatchesNativeAirSteps(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	// Scalar reference for ItemActor::postNormalTick: subtract 0.04 from Y,
	// move with that velocity, then multiply all axes by 0.98. These expected
	// states are independent of MovementComputer's implementation.
	steps := []struct{ pos, vel mgl64.Vec3 }{
		{mgl64.Vec3{4.1, 10.16, 3.9}, mgl64.Vec3{0.098, 0.1568, -0.098}},
		{mgl64.Vec3{4.198, 10.2768, 3.802}, mgl64.Vec3{0.09604, 0.114464, -0.09604}},
		{mgl64.Vec3{4.29404, 10.351264, 3.70596}, mgl64.Vec3{0.0941192, 0.07297472, -0.0941192}},
	}
	if err := w.Do(func(tx *world.Tx) {
		e := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{4, 10, 4}, Velocity: mgl64.Vec3{0.1, 0.2, -0.1}}, item.NewStack(item.Diamond{}, 1))).(*Ent)
		for tick, want := range steps {
			e.Tick(tx, int64(tick))
			if !e.Position().ApproxEqualThreshold(want.pos, 1e-9) || !e.Velocity().ApproxEqualThreshold(want.vel, 1e-9) {
				t.Errorf("tick %d: position=%v velocity=%v, want %v %v", tick+1, e.Position(), e.Velocity(), want.pos, want.vel)
			}
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestItemLandingAppliesFrictionAfterCollision(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Do(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{4, 0, 4}, block.Stone{}, nil)
		e := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{4.5, 1.05, 4.5}, Velocity: mgl64.Vec3{0.1, -0.1, 0}}, item.NewStack(item.Diamond{}, 1))).(*Ent)
		e.Tick(tx, 0)
		if !e.OnGround() || !e.Position().ApproxEqualThreshold(mgl64.Vec3{4.6, 1, 4.5}, 1e-9) || !e.Velocity().ApproxEqualThreshold(mgl64.Vec3{0.0588, 0, 0}, 1e-9) {
			t.Errorf("landing: position=%v velocity=%v onGround=%v", e.Position(), e.Velocity(), e.OnGround())
		}
		e.Tick(tx, 1)
		if !e.Position().ApproxEqualThreshold(mgl64.Vec3{4.6588, 1, 4.5}, 1e-9) || !e.Velocity().ApproxEqualThreshold(mgl64.Vec3{0.0345744, 0, 0}, 1e-9) {
			t.Errorf("ground slide: position=%v velocity=%v", e.Position(), e.Velocity())
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
	w := world.Config{Synchronous: true}.New()
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
		e.Tick(tx, 0)
		if e.H().Closed() || e.Behaviour().(*ItemBehaviour).Item().Count() != 3 || !e.Position().ApproxEqualThreshold(mgl64.Vec3{4.25, 1.06, 4.3}, 1e-9) {
			t.Errorf("partial hopper pickup replaced or moved the item: closed=%v count=%d position=%v", e.H().Closed(), e.Behaviour().(*ItemBehaviour).Item().Count(), e.Position())
		}
		if got, _ := inv.Item(4); got.Count() != 64 {
			t.Errorf("hopper diamond count=%d, want 64", got.Count())
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
