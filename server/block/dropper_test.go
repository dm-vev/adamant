package block

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestDropperNBTRoundTrip(t *testing.T) {
	d := NewDropper()
	d.Facing, d.Triggered, d.CustomName = cube.FaceEast, true, "Supplies"
	if err := d.inventory.SetItem(4, item.NewStack(item.Diamond{}, 3)); err != nil {
		t.Fatal(err)
	}
	nbt := d.EncodeNBT()
	decoded := (Dropper{Facing: d.Facing, Triggered: d.Triggered}).DecodeNBT(nbt).(Dropper)
	stack, _ := decoded.inventory.Item(4)
	if nbt["id"] != "Dropper" || decoded.Facing != d.Facing || !decoded.Triggered || decoded.CustomName != d.CustomName || stack.Count() != 3 {
		t.Fatalf("round trip failed: nbt=%v decoded=%+v stack=%v", nbt, decoded, stack)
	}
}

func TestAllDropperStates(t *testing.T) {
	states := allDroppers()
	if len(states) != 12 {
		t.Fatalf("got %d states, want 12", len(states))
	}
	seen := map[[2]int32]bool{}
	for _, state := range states {
		d := state.(Dropper)
		name, properties := d.EncodeBlock()
		if name != "minecraft:dropper" {
			t.Fatalf("unexpected block name %q", name)
		}
		seen[[2]int32{properties["facing_direction"].(int32), int32(properties["triggered_bit"].(byte))}] = true
	}
	if len(seen) != 12 {
		t.Fatalf("got %d unique encoded states, want 12", len(seen))
	}
}

func TestDropperDispensesOneRandomStack(t *testing.T) {
	d := NewDropper()
	d.Triggered = true
	d.Facing = cube.FaceEast
	_ = d.inventory.SetItem(1, item.NewStack(item.Coal{}, 2))
	_ = d.inventory.SetItem(7, item.NewStack(item.Diamond{}, 2))
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, d, nil)
		tx.SetBlock(pos.Side(d.Facing), NewBarrel(), nil)
		d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		got := tx.Block(pos).(Dropper)
		coal, _ := got.inventory.Item(1)
		diamond, _ := got.inventory.Item(7)
		if coal.Count()+diamond.Count() != 3 || !got.Triggered {
			t.Fatalf("counts = %d + %d, triggered=%v", coal.Count(), diamond.Count(), got.Triggered)
		}
	})
}

func TestDropperInsertsIntoFrontContainer(t *testing.T) {
	d := NewDropper()
	d.Facing, d.Triggered = cube.FaceEast, true
	_ = d.inventory.SetItem(0, item.NewStack(item.Diamond{}, 2))
	b := NewBarrel()
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, d, nil)
		tx.SetBlock(pos.Side(d.Facing), b, nil)
		d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		source, _ := tx.Block(pos).(Dropper).inventory.Item(0)
		destination := tx.Block(pos.Side(d.Facing)).(Barrel).inventory.Items()
		if source.Count() != 1 || len(destination) != 1 || destination[0].Count() != 1 {
			t.Fatalf("source=%v destination=%v", source, destination)
		}
	})
}

func TestDropperFallingEdgeDoesNotDispense(t *testing.T) {
	d := NewDropper()
	d.Triggered = true
	_ = d.inventory.SetItem(0, item.NewStack(item.Diamond{}, 2))
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, d, nil)
		after, changed := d.RedstonePowerUpdate(pos, tx, 0)
		if changed {
			tx.SetBlock(pos, after, nil)
		}
		got := tx.Block(pos).(Dropper)
		stack, _ := got.inventory.Item(0)
		if got.Triggered || stack.Count() != 2 {
			t.Fatalf("falling edge: triggered=%v stack=%v", got.Triggered, stack)
		}
	})
}
