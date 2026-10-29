package block_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

func bellTestWorld(t *testing.T) (*world.World, cube.Pos) {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceDown), block.Stone{}, nil)
		tx.SetBlock(pos, block.Bell{Attach: block.StandingBellAttachment(), Facing: cube.South}, nil)
	})
	return w, pos
}

func bellNBT(w *world.World, pos cube.Pos) (nbt map[string]any) {
	<-w.Exec(func(tx *world.Tx) { nbt = tx.Block(pos).(block.Bell).EncodeNBT() })
	return nbt
}

// TestBellTicksCountLikeBDS checks the Ticks count BDS keeps: a ring resets
// it, it counts two a tick while ringing until the bell stops at 50 and one a
// tick while the bell is still, wrapping at 50.
func TestBellTicksCountLikeBDS(t *testing.T) {
	w, pos := bellTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		if !tx.Block(pos).(block.Bell).Ring(pos, tx, cube.FaceSouth) {
			t.Fatal("standing bell did not ring along its north/south axis")
		}
	})
	for tick := 1; tick < 25; tick++ {
		w.AdvanceTick()
		if nbt := bellNBT(w, pos); nbt["Ringing"] != uint8(1) || nbt["Ticks"] != int32(2*tick) {
			t.Fatalf("tick %d: Ringing %v Ticks %v, want 1 and %d", tick, nbt["Ringing"], nbt["Ticks"], 2*tick)
		}
	}
	w.AdvanceTick()
	if nbt := bellNBT(w, pos); nbt["Ringing"] != uint8(0) || nbt["Ticks"] != int32(0) {
		t.Fatalf("after 25 ticks: Ringing %v Ticks %v, want 0 and 0", nbt["Ringing"], nbt["Ticks"])
	}
	for range 49 {
		w.AdvanceTick()
	}
	if nbt := bellNBT(w, pos); nbt["Ticks"] != int32(49) {
		t.Fatalf("still bell Ticks %v after 49 ticks, want 49", nbt["Ticks"])
	}
	w.AdvanceTick()
	if nbt := bellNBT(w, pos); nbt["Ticks"] != int32(0) {
		t.Fatalf("still bell Ticks %v after 50 ticks, want it wrapped to 0", nbt["Ticks"])
	}
}

// TestBellRingsWhenPowered checks actor-sampled rising edges and the native
// toggle state, which is retained across a falling edge and flipped on retrigger.
func TestBellRingsWhenPowered(t *testing.T) {
	w, pos := bellTestWorld(t)
	source := pos.Side(cube.FaceEast)
	<-w.Exec(func(tx *world.Tx) { tx.SetBlock(source, block.RedstoneBlock{}, nil) })
	for range 4 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		bell := tx.Block(pos).(block.Bell)
		nbt := bell.EncodeNBT()
		if !bell.Powered || nbt["Ringing"] != uint8(1) || nbt["Direction"] != int32(0) {
			t.Fatalf("powered bell: Powered %v Ringing %v Direction %v, want true, 1 and 0 (south)", bell.Powered, nbt["Ringing"], nbt["Direction"])
		}
	})
	for range 30 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) { tx.SetBlock(source, block.Air{}, nil) })
	for range 4 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		bell := tx.Block(pos).(block.Bell)
		if !bell.Powered || bell.EncodeNBT()["Ringing"] != uint8(0) {
			t.Fatalf("released bell: toggle %v Ringing %v, want true and 0", bell.Powered, bell.EncodeNBT()["Ringing"])
		}
	})
	<-w.Exec(func(tx *world.Tx) { tx.SetBlock(source, block.RedstoneBlock{}, nil) })
	for range 4 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		bell := tx.Block(pos).(block.Bell)
		if bell.Powered || bell.EncodeNBT()["Ringing"] != uint8(1) {
			t.Fatal("retrigger did not flip toggle and restart ringing")
		}
	})
}
