package block

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type dispenserTestEntityType struct{}
type dispenserTestEntityConfig struct{ variant int }
type dispenserTestEntity struct {
	h    *world.EntityHandle
	data *world.EntityData
}

func (dispenserTestEntityType) Open(_ *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	return &dispenserTestEntity{h: h, data: data}
}
func (dispenserTestEntityType) EncodeEntity() string { return "test:item" }
func (dispenserTestEntityType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.1, 0, -0.1, 0.1, 0.2, 0.1)
}
func (dispenserTestEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (dispenserTestEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }
func (c dispenserTestEntityConfig) Apply(data *world.EntityData)            { data.Data = c.variant }
func (e *dispenserTestEntity) Close() error                                 { return nil }
func (e *dispenserTestEntity) H() *world.EntityHandle                       { return e.h }
func (e *dispenserTestEntity) Position() mgl64.Vec3                         { return e.data.Pos }
func (e *dispenserTestEntity) Rotation() cube.Rotation                      { return e.data.Rot }

func TestDispenserNBTRoundTrip(t *testing.T) {
	d := NewDispenser()
	d.Facing, d.Triggered, d.CustomName = cube.FaceEast, true, "Supplies"
	_ = d.inventory.SetItem(4, item.NewStack(item.Diamond{}, 3))
	nbt := d.EncodeNBT()
	decoded := (Dispenser{Facing: d.Facing, Triggered: d.Triggered}).DecodeNBT(nbt).(Dispenser)
	stack, _ := decoded.inventory.Item(4)
	if nbt["id"] != "Dispenser" || decoded.Facing != d.Facing || !decoded.Triggered || decoded.CustomName != d.CustomName || stack.Count() != 3 {
		t.Fatalf("round trip failed: nbt=%v decoded=%+v stack=%v", nbt, decoded, stack)
	}
}

func TestAllDispenserStates(t *testing.T) {
	states := allDispensers()
	seen := map[[2]int32]bool{}
	for _, state := range states {
		_, properties := state.(Dispenser).EncodeBlock()
		seen[[2]int32{properties["facing_direction"].(int32), int32(properties["triggered_bit"].(byte))}] = true
	}
	if len(states) != 12 || len(seen) != 12 {
		t.Fatalf("got %d states and %d encodings, want 12", len(states), len(seen))
	}
}

func TestDispenserConsumesOneAndClearsTrigger(t *testing.T) {
	d := NewDispenser()
	d.Facing, d.Triggered = cube.FaceEast, true
	_ = d.inventory.SetItem(0, item.NewStack(item.FireCharge{}, 2))
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, d, nil)
		tx.SetBlock(pos.Side(d.Facing).Side(cube.FaceDown), Stone{}, nil)
		d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		got := tx.Block(pos).(Dispenser)
		stack, _ := got.inventory.Item(0)
		if _, ok := tx.Block(pos.Side(d.Facing)).(Fire); got.Triggered || stack.Count() != 1 || !ok {
			t.Fatalf("triggered=%v stack=%v front=%T", got.Triggered, stack, tx.Block(pos.Side(d.Facing)))
		}
	})
}

func TestDispenserFallingEdgeDoesNotConsume(t *testing.T) {
	d := NewDispenser()
	d.Triggered = true
	_ = d.inventory.SetItem(0, item.NewStack(item.Diamond{}, 2))
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, d, nil)
		d.RedstoneUpdate(pos, tx)
		got := tx.Block(pos).(Dispenser)
		stack, _ := got.inventory.Item(0)
		if got.Triggered || stack.Count() != 2 {
			t.Fatalf("triggered=%v stack=%v", got.Triggered, stack)
		}
	})
}

func TestDispenserEjectsUnsupportedItem(t *testing.T) {
	typ := dispenserTestEntityType{}
	entities := world.EntityRegistryConfig{Item: func(opts world.EntitySpawnOpts, _ any) *world.EntityHandle {
		return opts.New(typ, dispenserTestEntityConfig{})
	}}.New([]world.EntityType{typ})
	d := NewDispenser()
	d.Facing, d.Triggered = cube.FaceEast, true
	_ = d.inventory.SetItem(0, item.NewStack(item.Diamond{}, 1))
	w := world.Config{Synchronous: true, Entities: entities}.New()
	defer w.Close()
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, d, nil)
		d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		count := 0
		for entity := range tx.Entities() {
			count++
			if velocity := entity.(*dispenserTestEntity).data.Vel; velocity[0] <= 0 || velocity[1] != 0 || velocity[2] != 0 {
				t.Fatalf("unexpected velocity %v", velocity)
			}
		}
		stack, _ := tx.Block(pos).(Dispenser).inventory.Item(0)
		if count != 1 || !stack.Empty() {
			t.Fatalf("entities=%d stack=%v", count, stack)
		}
	})
}

func TestDispenserSpawnsBoatsOnWater(t *testing.T) {
	typ := dispenserTestEntityType{}
	entities := world.EntityRegistryConfig{
		Boat: func(opts world.EntitySpawnOpts, variant int) *world.EntityHandle {
			return opts.New(typ, dispenserTestEntityConfig{variant: variant})
		},
		ChestBoat: func(opts world.EntitySpawnOpts, variant int) *world.EntityHandle {
			return opts.New(typ, dispenserTestEntityConfig{variant: variant})
		},
	}.New([]world.EntityType{typ})

	tests := []struct {
		name    string
		stack   item.Stack
		variant item.BoatVariant
		below   bool
	}{
		{"boat facing water", item.NewStack(item.Boat{Variant: item.BoatVariantCherry}, 1), item.BoatVariantCherry, false},
		{"chest boat above water", item.NewStack(item.ChestBoat{Variant: item.BoatVariantBamboo}, 1), item.BoatVariantBamboo, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDispenser()
			d.Facing, d.Triggered = cube.FaceEast, true
			_ = d.inventory.SetItem(0, tt.stack)
			w := world.Config{Synchronous: true, Entities: entities}.New()
			defer w.Close()
			pos := cube.Pos{0, 1, 0}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos, d, nil)
				waterPos := pos.Side(d.Facing)
				if tt.below {
					waterPos = waterPos.Side(cube.FaceDown)
				}
				tx.SetLiquid(waterPos, Water{Depth: 8, Still: true})
				d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))

				count := 0
				for spawned := range tx.Entities() {
					count++
					e := spawned.(*dispenserTestEntity)
					if e.data.Data != tt.variant.Int() || e.Position() != (mgl64.Vec3{1.5, 1.375, 0.5}) || e.Rotation() != (cube.Rotation{90, 0}) || e.data.Vel != (mgl64.Vec3{0.1, 0, 0}) {
						t.Fatalf("unexpected boat: variant=%v pos=%v rot=%v vel=%v", e.data.Data, e.Position(), e.Rotation(), e.data.Vel)
					}
				}
				stack, _ := tx.Block(pos).(Dispenser).inventory.Item(0)
				if count != 1 || !stack.Empty() {
					t.Fatalf("entities=%d stack=%v", count, stack)
				}
			})
		})
	}
}

func TestDispenserEjectsBoatWithoutWater(t *testing.T) {
	typ := dispenserTestEntityType{}
	entities := world.EntityRegistryConfig{
		Item: func(opts world.EntitySpawnOpts, _ any) *world.EntityHandle {
			return opts.New(typ, dispenserTestEntityConfig{variant: -1})
		},
		Boat: func(opts world.EntitySpawnOpts, variant int) *world.EntityHandle {
			return opts.New(typ, dispenserTestEntityConfig{variant: variant})
		},
	}.New([]world.EntityType{typ})
	d := NewDispenser()
	d.Facing, d.Triggered = cube.FaceEast, true
	_ = d.inventory.SetItem(0, item.NewStack(item.Boat{Variant: item.BoatVariantOak}, 1))
	w := world.Config{Synchronous: true, Entities: entities}.New()
	defer w.Close()
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, d, nil)
		d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		for spawned := range tx.Entities() {
			e := spawned.(*dispenserTestEntity)
			if e.data.Data != -1 || e.data.Vel != (mgl64.Vec3{0.2, 0, 0}) {
				t.Fatalf("boat did not use item fallback: variant=%v velocity=%v", e.data.Data, e.data.Vel)
			}
			return
		}
		t.Fatal("expected ejected boat item")
	})
}
