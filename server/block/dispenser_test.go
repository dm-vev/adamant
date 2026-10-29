package block

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Exercise fallback factories with independent executed-client velocity and
// origin controls. The first two words select the sole stack and construct
// the actor; the fixture supplies the subsequent speed/Gaussian stream.
func newDispenserFallbackTestWorld(t *testing.T, entities world.EntityRegistry) (*world.World, world.EntitySpawnOpts) {
	t.Helper()
	data, err := os.ReadFile("testdata/bedrock_1_26_51_ejection_velocities.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Seed   uint32
			Face   int
			Frames []struct {
				Words    []uint32
				Velocity [3]float32
			}
		}
		Origins []struct {
			Position    cube.Pos
			Orientation int
			Origin      [3]float32
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete {
		t.Fatal("incomplete native ejection corpus")
	}
	source := &ejectionNativeWords{active: true}
	var want world.EntitySpawnOpts
	velocityFound, originFound := false, false
	for _, c := range native.Scenarios {
		if c.Seed == 1 && c.Face == int(cube.FaceEast) {
			if len(c.Frames) != 40 {
				t.Fatal("missing native fallback velocity")
			}
			source.words = append([]uint32{0, 0}, c.Frames[0].Words...)
			for i, x := range c.Frames[0].Velocity {
				want.Velocity[i] = float64(x)
			}
			velocityFound = true
		}
	}
	for _, c := range native.Origins {
		if c.Position == (cube.Pos{0, 70, 0}) && c.Orientation == 9 {
			for i, x := range c.Origin {
				want.Position[i] = float64(x)
			}
			originFound = true
		}
	}
	if !velocityFound || !originFound {
		t.Fatal("missing native eastward fallback controls")
	}
	t.Cleanup(func() {
		if source.used != len(source.words) {
			t.Errorf("fallback consumed %d native words, want %d", source.used, len(source.words))
		}
	})
	return world.Config{Synchronous: true, Entities: entities, RandSource: source}.New(), want
}

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

func TestDispenserShootsFireChargeAndKeepsPoweredState(t *testing.T) {
	typ := dispenserTestEntityType{}
	entities := world.EntityRegistryConfig{SmallFireball: func(opts world.EntitySpawnOpts, _ world.Entity) *world.EntityHandle {
		return opts.New(typ, dispenserTestEntityConfig{})
	}}.New([]world.EntityType{typ})
	d := NewDispenser()
	d.Facing, d.Triggered = cube.FaceEast, true
	_ = d.inventory.SetItem(0, item.NewStack(item.FireCharge{}, 2))
	w := world.Config{Synchronous: true, Entities: entities}.New()
	defer w.Close()
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, d, nil)
		d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		got := tx.Block(pos).(Dispenser)
		stack, _ := got.inventory.Item(0)
		spawned := 0
		for e := range tx.Entities() {
			if e.H().Type() == typ {
				spawned++
			}
		}
		if !got.Triggered || stack.Count() != 1 || spawned != 1 {
			t.Fatalf("triggered=%v stack=%v small fireballs=%d", got.Triggered, stack, spawned)
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
		after, changed := d.RedstonePowerUpdate(pos, tx, 0)
		if changed {
			tx.SetBlock(pos, after, nil)
		}
		got := tx.Block(pos).(Dispenser)
		stack, _ := got.inventory.Item(0)
		if got.Triggered || stack.Count() != 2 {
			t.Fatalf("triggered=%v stack=%v", got.Triggered, stack)
		}
	})
}

func TestDispenserFiresOncePerPoweredEdge(t *testing.T) {
	typ := dispenserTestEntityType{}
	entities := world.EntityRegistryConfig{SmallFireball: func(opts world.EntitySpawnOpts, _ world.Entity) *world.EntityHandle {
		return opts.New(typ, dispenserTestEntityConfig{})
	}}.New([]world.EntityType{typ})
	d := NewDispenser()
	d.Facing = cube.FaceEast
	_ = d.inventory.SetItem(0, item.NewStack(item.FireCharge{}, 2))
	w := world.Config{Synchronous: true, Entities: entities}.New()
	defer w.Close()
	pos := cube.Pos{0, 1, 0}
	sourcePos := pos.Side(cube.FaceWest)

	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, d, nil)
		tx.SetBlock(pos.Side(d.Facing).Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(sourcePos, RedstoneBlock{}, nil)
	})
	for range 12 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		got := tx.Block(pos).(Dispenser)
		stack, _ := got.inventory.Item(0)
		if !got.Triggered || stack.Count() != 1 {
			t.Fatalf("continuous power triggered more than once: triggered=%v stack=%v", got.Triggered, stack)
		}
		tx.SetBlock(sourcePos, nil, nil)
	})
	// The native scene samples the falling edge and publishes it in the next
	// circuit pass, rather than clearing the flag in the neighbour callback.
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if !tx.Block(pos).(Dispenser).Triggered {
			t.Fatal("falling edge bypassed the scene publication delay")
		}
	})
	for range 2 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		got := tx.Block(pos).(Dispenser)
		stack, _ := got.inventory.Item(0)
		if got.Triggered || stack.Count() != 1 {
			t.Fatalf("falling edge: triggered=%t stack=%v", got.Triggered, stack)
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
	w, want := newDispenserFallbackTestWorld(t, entities)
	defer w.Close()
	pos := cube.Pos{0, 70, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, d, nil)
		d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		count := 0
		for entity := range tx.Entities() {
			count++
			if velocity := entity.(*dispenserTestEntity).data.Vel; velocity != want.Velocity || entity.Position() != want.Position {
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
		Item: func(opts world.EntitySpawnOpts, it any) *world.EntityHandle {
			return opts.New(typ, dispenserTestEntityConfig{variant: -it.(item.Stack).Count()})
		},
		Boat: func(opts world.EntitySpawnOpts, variant int) *world.EntityHandle {
			return opts.New(typ, dispenserTestEntityConfig{variant: variant})
		},
	}.New([]world.EntityType{typ})
	d := NewDispenser()
	d.Facing, d.Triggered = cube.FaceEast, true
	_ = d.inventory.SetItem(0, item.NewStack(item.Boat{Variant: item.BoatVariantOak}, 1))
	w, want := newDispenserFallbackTestWorld(t, entities)
	defer w.Close()
	pos := cube.Pos{0, 70, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, d, nil)
		d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		for spawned := range tx.Entities() {
			e := spawned.(*dispenserTestEntity)
			if e.data.Data != -1 || e.data.Vel != want.Velocity || e.Position() != want.Position {
				t.Fatalf("boat did not use item fallback: variant=%v velocity=%v", e.data.Data, e.data.Vel)
			}
			return
		}
		t.Fatal("expected ejected boat item")
	})
}

func TestDispenserMinecarts(t *testing.T) {
	typ := dispenserTestEntityType{}
	entities := world.EntityRegistryConfig{
		Item: func(opts world.EntitySpawnOpts, _ any) *world.EntityHandle {
			return opts.New(typ, dispenserTestEntityConfig{variant: -1})
		},
		Minecart: func(opts world.EntitySpawnOpts) *world.EntityHandle {
			return opts.New(typ, dispenserTestEntityConfig{variant: 0})
		},
		MinecartChest: func(opts world.EntitySpawnOpts) *world.EntityHandle {
			return opts.New(typ, dispenserTestEntityConfig{variant: 1})
		},
		MinecartHopper: func(opts world.EntitySpawnOpts) *world.EntityHandle {
			return opts.New(typ, dispenserTestEntityConfig{variant: 2})
		},
		MinecartTNT: func(opts world.EntitySpawnOpts) *world.EntityHandle {
			return opts.New(typ, dispenserTestEntityConfig{variant: 3})
		},
	}.New([]world.EntityType{typ})
	tests := []struct {
		name string
		item world.Item
		want int
	}{
		{"minecart", item.Minecart{}, 0},
		{"chest", item.MinecartChest{}, 1},
		{"hopper", item.MinecartHopper{}, 2},
		{"tnt", item.MinecartTNT{}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, rail := range []world.Block{Rail{Direction: RailEastWest}, PoweredRail{Direction: RailAscendingEast}} {
				d := NewDispenser()
				d.Facing, d.Triggered = cube.FaceEast, true
				_ = d.inventory.SetItem(0, item.NewStack(tt.item, 1))
				w := world.Config{Synchronous: true, Entities: entities}.New()
				pos := cube.Pos{0, 1, 0}
				<-w.Exec(func(tx *world.Tx) {
					tx.SetBlock(pos, d, nil)
					tx.SetBlock(pos.Side(d.Facing), rail, nil)
					d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
					count := 0
					for spawned := range tx.Entities() {
						count++
						e := spawned.(*dispenserTestEntity)
						// This test type has no origin offset; its position records
						// the native actor-centre request for both rail shapes.
						wantPos := mgl64.Vec3{1.625, 1.5, 0.5}
						if e.data.Data != tt.want || e.Position() != wantPos || e.data.Vel != (mgl64.Vec3{}) {
							t.Fatalf("minecart=%v pos=%v velocity=%v", e.data.Data, e.Position(), e.data.Vel)
						}
					}
					stack, _ := tx.Block(pos).(Dispenser).inventory.Item(0)
					if count != 1 || !stack.Empty() {
						t.Fatalf("entities=%d stack=%v", count, stack)
					}
				})
				_ = w.Close()
			}
		})
	}
}

func TestDispenserMinecartsFallbackWithoutRail(t *testing.T) {
	typ := dispenserTestEntityType{}
	entities := world.EntityRegistryConfig{Item: func(opts world.EntitySpawnOpts, it any) *world.EntityHandle {
		return opts.New(typ, dispenserTestEntityConfig{variant: -it.(item.Stack).Count()})
	}}.New([]world.EntityType{typ})
	for _, it := range []world.Item{item.Minecart{}, item.MinecartChest{}, item.MinecartHopper{}, item.MinecartTNT{}} {
		d := NewDispenser()
		d.Facing, d.Triggered = cube.FaceEast, true
		_ = d.inventory.SetItem(0, item.NewStack(it, 1))
		w, want := newDispenserFallbackTestWorld(t, entities)
		<-w.Exec(func(tx *world.Tx) {
			pos := cube.Pos{0, 70, 0}
			tx.SetBlock(pos, d, nil)
			d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
			count := 0
			for spawned := range tx.Entities() {
				count++
				e := spawned.(*dispenserTestEntity)
				if e.data.Data != -1 || e.data.Vel != want.Velocity || e.Position() != want.Position {
					t.Fatalf("minecart did not use item fallback: item=%T variant=%v velocity=%v", it, e.data.Data, e.data.Vel)
				}
			}
			stack, _ := tx.Block(pos).(Dispenser).inventory.Item(0)
			if count != 1 || !stack.Empty() {
				t.Fatalf("entities=%d stack=%v", count, stack)
			}
		})
		_ = w.Close()
	}
}
