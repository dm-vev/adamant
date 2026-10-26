package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestLeashAttachBreakAndFenceKnotLifecycle(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	mobHandle := NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}})
	holderHandle := NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 65, .5}})
	<-w.Exec(func(tx *world.Tx) {
		mob := tx.AddEntity(mobHandle).(*Cow)
		holder := tx.AddEntity(holderHandle)
		if !mob.AttachLeash(holder, tx) || mob.HolderLeashID() != holderHandle.UniqueID() {
			t.Fatal("lead attachment did not persist the holder ID")
		}
		encoded := CowType.EncodeNBT(mob.data)
		if encoded[leashHolderKey] != holderHandle.UniqueID() {
			t.Fatalf("encoded leash holder = %v, want %d", encoded[leashHolderKey], holderHandle.UniqueID())
		}
	})

	<-w.Exec(func(tx *world.Tx) {
		holder, _ := holderHandle.Entity(tx)
		holder.(*Cow).Teleport(mgl64.Vec3{11.5, 65, .5})
		mob, _ := mobHandle.Entity(tx)
		tickLeash(mob.(*Cow).Ent, tx, nil)
		if got := mob.(*Cow).HolderLeashID(); got != 0 {
			t.Fatalf("leash holder after moving 11 blocks = %d, want detached", got)
		}
		if got := countLeashDrops(tx); got != 1 {
			t.Fatalf("lead drops after leash break = %d, want 1", got)
		}
	})

	fencePos := cube.Pos{0, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(fencePos, block.WoodFence{}, nil)
		mob, _ := mobHandle.Entity(tx)
		holder, _ := holderHandle.Entity(tx)
		if !mob.(*Cow).AttachLeash(holder, tx) {
			t.Fatal("could not reattach cow")
		}
		if !TieLeashedEntities(fencePos, tx, holder) {
			t.Fatal("tying a held cow to a fence did not create a knot")
		}
		id := mob.(*Cow).HolderLeashID()
		if id == 0 || id == holderHandle.UniqueID() {
			t.Fatalf("cow holder after tying = %d, want knot ID", id)
		}
		var knot *LeashKnot
		for candidate := range tx.Entities() {
			if k, ok := candidate.(*LeashKnot); ok && k.H().UniqueID() == id {
				knot = k
				break
			}
		}
		if knot == nil {
			t.Fatal("fence knot entity was not registered in the world")
		}
		tx.SetBlock(fencePos, block.Air{}, nil)
		knot.Tick(tx, tx.CurrentTick())
		if got := mob.(*Cow).HolderLeashID(); got != 0 {
			t.Fatalf("holder after fence removal = %d, want detached", got)
		}
		if got := countLeashDrops(tx); got != 2 {
			t.Fatalf("lead drops after fence removal = %d, want 2 total", got)
		}
	})
}

func countLeashDrops(tx *world.Tx) int {
	count := 0
	for e := range tx.Entities() {
		if e.H() != nil && e.H().Type().EncodeEntity() == "minecraft:item" {
			count++
		}
	}
	return count
}

// TestTyingLeadsReusesTheKnot follows LeadItem.bindPlayerMobs: tying more
// mobs to a fence uses the knot already there, and mobs led from farther
// than 16 blocks are not gathered.
func TestTyingLeadsReusesTheKnot(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		fencePos := cube.Pos{0, 64, 0}
		tx.SetBlock(fencePos, block.WoodFence{}, nil)
		holder := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, .5}}))
		for i, x := range []float64{4.5, 6.5, 30.5} {
			cow := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{x, 64, .5}})).(*Cow)
			cow.AttachLeash(holder, tx)
			if i < 2 && !TieLeashedEntities(fencePos, tx, holder) {
				t.Fatalf("could not tie cow %d", i)
			}
		}
		knots := 0
		for e := range tx.Entities() {
			if _, ok := e.(*LeashKnot); ok {
				knots++
			}
		}
		if knots != 1 {
			t.Fatalf("%d knots on one fence, want 1", knots)
		}
		if TieLeashedEntities(fencePos, tx, holder) {
			t.Fatal("tied a cow led from 30 blocks away")
		}
	})
}
