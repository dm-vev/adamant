package world_test

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

func TestFluidTickSourceIdentitySurvivesScheduleOnlySave(t *testing.T) {
	dir := t.TempDir()
	db, err := mcdb.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	db.Settings().Spawn = cube.Pos{0, 64, 0}
	w := world.Config{Synchronous: true, Provider: db, RandomTickSpeed: -1}.New()
	waterPos, lavaPos := cube.Pos{8, 64, 8}, cube.Pos{10, 64, 8}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetLiquid(waterPos, block.Water{Depth: 7, Still: true})
		tx.SetLiquid(lavaPos, block.Lava{Depth: 7, Still: true})
	})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = mcdb.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w = world.Config{Synchronous: true, Provider: db, RandomTickSpeed: -1}.New()
	<-w.Exec(func(tx *world.Tx) {
		tx.Block(waterPos)
		for i, liquid := range []world.Liquid{
			block.Water{Depth: 8, Still: false}, block.Water{Depth: 8, Still: true},
			block.Water{Depth: 7, Still: true}, block.Water{Depth: 7, Still: false}, block.Water{Depth: 8, Still: true, Falling: true},
		} {
			delay := time.Second
			if i >= 2 {
				delay /= 2
			}
			tx.ScheduleBlockUpdate(waterPos, liquid, delay)
		}
		for i, liquid := range []world.Liquid{
			block.Lava{Depth: 8, Still: false}, block.Lava{Depth: 8, Still: true},
			block.Lava{Depth: 7, Still: true}, block.Lava{Depth: 7, Still: false}, block.Lava{Depth: 8, Still: true, Falling: true},
		} {
			delay := time.Second
			if i >= 2 {
				delay /= 2
			}
			tx.ScheduleBlockUpdate(lavaPos, liquid, delay)
		}
	})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = mcdb.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	col, err := db.LoadColumn(world.ChunkPos{}, world.Overworld)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]cube.Pos{"minecraft:water": waterPos, "minecraft:flowing_water": waterPos, "minecraft:lava": lavaPos, "minecraft:flowing_lava": lavaPos}
	for _, tick := range col.ScheduledTicks {
		pos, exists := want[tick.Type]
		delay := int32(20)
		if tick.Type == "minecraft:flowing_water" || tick.Type == "minecraft:flowing_lava" {
			delay = 10
		}
		if !exists || tick.Pos != pos || tick.Delay != delay {
			t.Fatalf("unexpected saved fluid tick: %#v; want delay %d", tick, delay)
		}
		delete(want, tick.Type)
	}
	if len(want) != 0 {
		t.Fatalf("schedule-only save lost fluid identities: %v", want)
	}
}

func TestFluidTickDispatchMatchesFamilyNotNativeBlockName(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	pos := cube.Pos{8, 64, 8}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetLiquid(pos.Side(cube.FaceUp), block.Water{Depth: 8, Still: true})
		tx.SetLiquid(pos, block.Water{Depth: 7, Still: true})
		tx.ScheduleBlockUpdate(pos, block.Lava{Depth: 8, Still: true}, time.Second/20)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Liquid(pos.Side(cube.FaceDown)); ok {
			t.Fatal("lava tick dispatched the water fluid")
		}
		tx.ScheduleBlockUpdate(pos, block.Water{Depth: 8, Still: false}, time.Second/20)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		liquid, ok := tx.Liquid(pos.Side(cube.FaceDown))
		if !ok || liquid.LiquidType() != "water" || !liquid.LiquidFalling() {
			t.Fatal("source-water tick failed to advance a flowing-water state")
		}
	})
}

type fluidSchedulingTestBlock struct {
	block.Stone
	target cube.Pos
}

func (fluidSchedulingTestBlock) EncodeBlock() (string, map[string]any) {
	return "test:fluid_scheduler", nil
}
func (fluidSchedulingTestBlock) Hash() (uint64, uint64) { return 1 << 60, 0 }
func (b fluidSchedulingTestBlock) ScheduledTick(_ cube.Pos, tx *world.Tx, _ *rand.Rand) {
	tx.ScheduleBlockUpdate(b.target, block.Water{Depth: 8, Still: true}, time.Second/20)
}

func TestBlockToFluidSchedulingUsesCurrentSharedTickClock(t *testing.T) {
	blockPos, fluidPos := cube.Pos{8, 64, 8}, cube.Pos{9, 64, 8}
	scheduler := fluidSchedulingTestBlock{target: fluidPos}
	registry := world.NewBlockRegistry()
	name, properties := scheduler.EncodeBlock()
	registry.RegisterBlockState(world.BlockState{Name: name, Properties: properties})
	registry.RegisterBlock(scheduler)
	w := world.Config{Synchronous: true, Blocks: registry, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(blockPos, scheduler, nil)
		tx.SetLiquid(fluidPos, block.Water{Depth: 8, Still: true})
		tx.ScheduleBlockUpdate(blockPos, scheduler, time.Second/20)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Liquid(fluidPos.Side(cube.FaceDown)); ok {
			t.Fatal("block callback's one-tick fluid update ran in the same tick")
		}
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Liquid(fluidPos.Side(cube.FaceDown)); !ok {
			t.Fatal("block callback's fluid update missed the following tick")
		}
	})
}

var nativeFluidHalfHash = block.NextHash()
var nativeFluidUpperHalf = [...]cube.BBox{cube.Box(0, .5, 0, 1, 1, 1)}
var nativeFluidLowerHalf = [...]cube.BBox{cube.Box(0, 0, 0, 1, .5, 1)}

type nativeFluidHalfBlock struct {
	block.Stone
	closed bool
}

func (b nativeFluidHalfBlock) EncodeBlock() (string, map[string]any) {
	return "test:positional_fluid_half", map[string]any{"closed": b.closed}
}
func (b nativeFluidHalfBlock) Hash() (uint64, uint64) {
	if b.closed {
		return nativeFluidHalfHash, 1
	}
	return nativeFluidHalfHash, 0
}
func (nativeFluidHalfBlock) Model() world.BlockModel { return nativeFluidHalfModel{} }
func (nativeFluidHalfBlock) CanDisplace(liquid world.Liquid) bool {
	return liquid != nil && liquid.LiquidType() == "water"
}
func (b nativeFluidHalfBlock) SideClosed(pos, side cube.Pos, _ *world.Tx) bool {
	return b.closed && side == pos.Side(cube.FaceWest)
}

type nativeFluidHalfModel struct{}

func (nativeFluidHalfModel) BBox(pos cube.Pos, _ world.BlockSource) []cube.BBox {
	if pos[2]&1 == 0 {
		return nativeFluidUpperHalf[:]
	}
	return nativeFluidLowerHalf[:]
}
func (nativeFluidHalfModel) FaceSolid(pos cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return pos[2]&1 == 0 && face == cube.FaceUp || pos[2]&1 != 0 && face == cube.FaceDown
}

func TestFluidMixedSourceNativeBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name              string
		z                 int
		closed, wantFluid bool
	}{
		{"matching halves leave a gap", 8, false, true},
		{"complementary halves close the boundary", 9, false, false},
		{"native aperture closes a geometric gap", 8, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := world.NewBlockRegistry()
			custom := nativeFluidHalfBlock{closed: tc.closed}
			name, properties := custom.EncodeBlock()
			registry.RegisterBlockState(world.BlockState{Name: name, Properties: properties})
			registry.RegisterBlock(custom)
			w := world.Config{Blocks: registry, Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			source := cube.Pos{8, 64, tc.z}
			target := source.Side(cube.FaceEast)
			native, err := world.ResolveState(registry, world.BlockState{
				Name: "minecraft:oak_slab", Properties: map[string]any{"type": "top", "waterlogged": true},
			})
			if err != nil {
				t.Fatal(err)
			}
			slab, ok := registry.BlockByRuntimeID(native.Block)
			if !ok {
				t.Fatal("missing native slab")
			}
			liquid, ok := registry.BlockByRuntimeID(native.Liquid)
			if !ok {
				t.Fatal("missing native water")
			}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(source, slab, nil)
				tx.SetLiquid(source, liquid.(world.Liquid))
				tx.SetBlock(target, custom, nil)
				tx.SetBlock(source.Side(cube.FaceDown), block.Stone{}, nil)
				tx.SetBlock(target.Side(cube.FaceDown), block.Stone{}, nil)
				for _, face := range [...]cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest} {
					tx.SetBlock(source.Side(face), block.Stone{}, nil)
				}
				for _, face := range [...]cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceEast} {
					tx.SetBlock(target.Side(face), block.Stone{}, nil)
				}
				tx.ScheduleBlockUpdate(source, block.Water{Depth: 8, Still: true}, time.Second/20)
			})
			w.AdvanceTick()
			<-w.Exec(func(tx *world.Tx) {
				got, present := tx.Liquid(target)
				if present != tc.wantFluid {
					t.Fatalf("target fluid present = %v, want %v", present, tc.wantFluid)
				}
				if present && (got.LiquidDepth() != 7 || got.LiquidFalling()) {
					t.Errorf("target fluid = %#v, want non-falling depth 7", got)
				}
				if actual, ok := tx.Block(target).(nativeFluidHalfBlock); !ok || actual != custom {
					t.Error("flow replaced native foreground block")
				}
				if _, err := tx.BlockStateID(target); err == nil {
					t.Error("native-only block received a fabricated source identity")
				}
			})
		})
	}
}
