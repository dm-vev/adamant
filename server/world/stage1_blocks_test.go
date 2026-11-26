package world_test

import (
	"testing"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/block/model"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestStage1BlockFlags(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()
	reg := world.DefaultBlockRegistry

	assertFlags(t, reg, block.Air{}, map[string]bool{
		"empty": true, "fullCube": false, "liquid": false, "nbt": false, "randomTick": false, "redstone": false,
	})
	assertFlags(t, reg, block.Stone{}, map[string]bool{
		"empty": false, "fullCube": true, "liquid": false, "slow": false, "nbt": false,
	})
	assertFlags(t, reg, block.Leaves{Type: block.OakLeaves()}, map[string]bool{
		"fullCube": true, "randomTick": true, "displacing": true,
	})
	assertFlags(t, reg, block.WeepingVines{Age: 0}, map[string]bool{
		"climbable": true, "randomTick": true,
	})
	assertFlags(t, reg, block.WeepingVines{Age: 25}, map[string]bool{
		"climbable": true, "randomTick": false,
	})
	assertFlags(t, reg, block.SoulSand{}, map[string]bool{
		"fullCube": false, "empty": false, "slow": true, "honey": false,
	})
	if got := (model.Honey{}).StaticCollisionKind(); got != world.CollisionDynamic {
		t.Fatalf("model.Honey{}.StaticCollisionKind() = %v, want CollisionDynamic", got)
	}
	assertFlags(t, reg, block.HoneyBlock{}, map[string]bool{
		"fullCube": false, "empty": false, "honey": true, "slow": true, "powderSnow": false, "stepper": true,
	})
	assertFlags(t, reg, block.PowderSnow{}, map[string]bool{
		"powderSnow": true, "insider": true, "empty": true,
	})
	assertFlags(t, reg, block.Cobweb{}, map[string]bool{
		"empty": true, "insider": true,
	})
	assertFlags(t, reg, block.Magma{}, map[string]bool{
		"fullCube": true, "stepper": true,
	})
	assertFlags(t, reg, block.Chest{}, map[string]bool{
		"nbt": true, "displacing": true, "fullCube": false,
	})
	assertFlags(t, reg, block.RedstoneWire{}, map[string]bool{
		"redstone": true, "empty": true,
	})
}

func assertFlags(t *testing.T, reg *world.BasicBlockRegistry, b world.Block, want map[string]bool) {
	t.Helper()
	flags := reg.BlockFlags(reg.BlockRuntimeID(b))
	checks := map[string]bool{
		"empty":      flags.EmptyCollision(),
		"fullCube":   flags.FullCubeCollision(),
		"insider":    flags.HasEntityInsider(),
		"stepper":    flags.HasEntityStepper(),
		"liquid":     flags.IsLiquid(),
		"redstone":   flags.IsRedstoneRelevant(),
		"powderSnow": flags.IsPowderSnow(),
		"climbable":  flags.IsClimbable(),
		"slow":       flags.IsSlowBlock(),
		"honey":      flags.IsHoneyBlock(),
		"nbt":        flags.IsNBT(),
		"randomTick": flags.RandomTicks(),
		"displacing": flags.LiquidDisplacing(),
	}
	for k, expected := range want {
		if checks[k] != expected {
			t.Errorf("%T flag %q = %v, want %v", b, k, checks[k], expected)
		}
	}
}

func TestStage1LiquidInfoAndClimbables(t *testing.T) {
	t.Parallel()
	world.DefaultBlockRegistry.Finalize()
	reg := world.DefaultBlockRegistry

	assertLiquid := func(b world.Block, wantKind, wantDepth uint8, wantFalling bool) {
		t.Helper()
		rid := reg.BlockRuntimeID(b)
		if !reg.BlockFlags(rid).IsLiquid() {
			t.Fatalf("%T IsLiquid() = false", b)
		}
		k, d, f := reg.LiquidInfo(rid)
		if k != wantKind || d != wantDepth || f != wantFalling {
			t.Errorf("LiquidInfo(%T) = (%d, %d, %v), want (%d, %d, %v)", b, k, d, f, wantKind, wantDepth, wantFalling)
		}
	}
	assertLiquid(block.Water{Still: true, Depth: 8}, 1, 8, false)
	assertLiquid(block.Water{Still: false, Depth: 3, Falling: true}, 1, 3, true)
	assertLiquid(block.Lava{Still: true, Depth: 8}, 2, 8, false)
	if k, d, f := reg.LiquidInfo(reg.BlockRuntimeID(block.Stone{})); k != 0 || d != 0 || f {
		t.Fatalf("non-liquid Stone LiquidInfo = (%d, %d, %v), want (0, 0, false)", k, d, f)
	}

	climbNames := map[string]bool{
		"minecraft:ladder": true, "minecraft:vine": true, "minecraft:scaffolding": true,
		"minecraft:weeping_vines": true, "minecraft:twisting_vines": true,
		"minecraft:cave_vines": true, "minecraft:cave_vines_head_with_berries": true,
		"minecraft:cave_vines_body_with_berries": true,
	}
	seen := make(map[string]bool, len(climbNames))
	for rid := uint32(0); ; rid++ {
		name, _, ok := reg.RuntimeIDToStateView(rid)
		if !ok {
			break
		}
		if climbNames[name] {
			seen[name] = true
			if !reg.BlockFlags(rid).IsClimbable() {
				t.Errorf("%s (rid %d) missing IsClimbable()", name, rid)
			}
		}
	}
	if len(seen) != len(climbNames) {
		t.Fatalf("saw %d climbable block names in registry, want %d", len(seen), len(climbNames))
	}
}

func TestStage1BlockAndLiquidRIDLookups(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()
	w.Freeze()

	stonePos := cube.Pos{2, 10, 2}
	waterPos := cube.Pos{3, 10, 2}
	loggedPos := cube.Pos{4, 10, 2}
	stoneRID := world.DefaultBlockRegistry.BlockRuntimeID(block.Stone{})
	waterRID := world.DefaultBlockRegistry.BlockRuntimeID(block.Water{Still: true, Depth: 8})
	ladderRID := world.DefaultBlockRegistry.BlockRuntimeID(block.Ladder{Facing: cube.North})

	<-w.Exec(func(tx *world.Tx) {
		l := world.NewLoader(1, w, world.NopViewer{})
		l.Move(tx, mgl64.Vec3{2, 10, 2})
		l.Load(tx, 1)
		tx.SetBlock(stonePos, block.Stone{}, nil)
		tx.SetBlock(waterPos, block.Water{Still: true, Depth: 8}, nil)
		tx.SetBlock(loggedPos, block.Ladder{Facing: cube.North}, nil)
		tx.SetLiquid(loggedPos, block.Water{Still: true, Depth: 8})
	})

	<-w.Exec(func(tx *world.Tx) {
		rv := tx.NewReadView()
		if r, ok := tx.BlockRID(stonePos); !ok || r != stoneRID {
			t.Errorf("tx.BlockRID(stone) = (%d, %v), want (%d, true)", r, ok, stoneRID)
		}
		if r, ok := tx.LiquidRID(stonePos); ok || r != 0 {
			t.Errorf("tx.LiquidRID(stone) = (%d, %v), want (0, false)", r, ok)
		}
		if r, ok := tx.LiquidRID(waterPos); !ok || r != waterRID {
			t.Errorf("tx.LiquidRID(water) = (%d, %v), want (%d, true)", r, ok, waterRID)
		}
		if r, ok := tx.BlockRID(loggedPos); !ok || r != ladderRID {
			t.Errorf("tx.BlockRID(waterlogged) = (%d, %v), want (%d, true)", r, ok, ladderRID)
		}
		if r, ok := tx.LiquidRID(loggedPos); !ok || r != waterRID {
			t.Errorf("tx.LiquidRID(waterlogged) = (%d, %v), want (%d, true)", r, ok, waterRID)
		}
		if r, ok := rv.BlockRID(loggedPos); !ok || r != ladderRID {
			t.Errorf("rv.BlockRID(waterlogged) = (%d, %v), want (%d, true)", r, ok, ladderRID)
		}
		if r, ok := rv.LiquidRID(loggedPos); !ok || r != waterRID {
			t.Errorf("rv.LiquidRID(waterlogged) = (%d, %v), want (%d, true)", r, ok, waterRID)
		}
		if _, ok := tx.BlockRID(cube.Pos{0, 1000, 0}); ok {
			t.Error("out-of-bounds BlockRID succeeded")
		}
		if _, ok := rv.BlockRID(cube.Pos{1024, 10, 1024}); ok || !rv.Missed() {
			t.Errorf("unloaded ReadView.BlockRID should fail and set Missed(); missed=%v", rv.Missed())
		}
	})
}

func TestStage1CurrentTickClosedAndBroadcastEntityState(t *testing.T) {
	t.Parallel()
	shared := &world.Settings{CurrentTick: 100}
	provider := world.NopProvider{Set: shared}
	w1 := world.Config{Provider: provider, Synchronous: true, RandomTickSpeed: -1}.New()
	w2 := world.Config{Provider: provider, Dim: world.Nether, Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w2.Close() }()
	w1.Freeze()
	w2.Freeze()
	<-w1.Exec(func(*world.Tx) {})
	baseTick := w1.CurrentTick()

	if err := w1.StepTicks(2); err != nil {
		t.Fatal(err)
	}
	w1.AdvanceTick()
	w1.AdvanceTick()
	var closedTx *world.Tx
	<-w1.Exec(func(tx *world.Tx) {
		closedTx = tx
		if tx.Closed() {
			t.Fatal("active tx reported Closed() == true")
		}
		if got := tx.CurrentTick(); got != baseTick+2 {
			t.Fatalf("tx.CurrentTick() = %d, want %d", got, baseTick+2)
		}
	})
	if !closedTx.Closed() || !(*world.Tx)(nil).Closed() {
		t.Fatal("Closed() check failed for finished/nil tx")
	}
	_ = w1.Close()
	if got := w2.CurrentTick(); got != baseTick+2 {
		t.Fatalf("w2.CurrentTick() after owner handoff = %d, want %d", got, baseTick+2)
	}

	s1, s2 := &stateSharedViewer{}, &stateSharedViewer{}
	plain := &statePlainViewer{}
	ent := stage1TestEntity{pos: mgl64.Vec3{4, 64, 4}}
	<-w2.Exec(func(tx *world.Tx) {
		for _, v := range []world.Viewer{s1, s2, plain} {
			l := world.NewLoader(1, w2, v)
			l.Move(tx, ent.pos)
			l.Load(tx, 1)
		}
	})
	<-w2.Exec(func(tx *world.Tx) {
		world.BroadcastEntityState(tx, ent)
		world.BroadcastEntityState(nil, ent)
		world.BroadcastEntityState(closedTx, ent)
		world.BroadcastEntityState(tx, nil)
	})
	if s1.calls != 1 || s2.calls != 1 || plain.calls != 1 {
		t.Fatalf("viewer calls = (%d, %d, %d), want (1, 1, 1)", s1.calls, s2.calls, plain.calls)
	}
	if s1.encodes+s2.encodes != 1 || s1.last != "shared" || s2.last != "shared" {
		t.Fatalf("shared broadcast encodes=%d s1=%q s2=%q, want 1 encode and 'shared'", s1.encodes+s2.encodes, s1.last, s2.last)
	}
}

type stateSharedViewer struct {
	world.NopViewer
	calls, encodes int
	last           string
}

func (v *stateSharedViewer) ViewEntityStateShared(_ world.Entity, b *world.Broadcast) {
	v.calls++
	if s, ok := b.Load().(string); ok {
		v.last = s
		return
	}
	v.encodes++
	b.Store("shared")
	v.last = "shared"
}

type statePlainViewer struct {
	world.NopViewer
	calls int
}

func (v *statePlainViewer) ViewEntityState(world.Entity) { v.calls++ }

type stage1TestEntity struct{ pos mgl64.Vec3 }

func (stage1TestEntity) Close() error            { return nil }
func (stage1TestEntity) H() *world.EntityHandle  { return nil }
func (e stage1TestEntity) Position() mgl64.Vec3  { return e.pos }
func (stage1TestEntity) Rotation() cube.Rotation { return cube.Rotation{} }
