package world

import (
	"bytes"
	"testing"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world/chunk"
)

type stage1Block struct {
	name string
}

func (b stage1Block) Hash() (uint64, uint64)                { return 0, ^uint64(0) }
func (b stage1Block) Model() BlockModel                     { return nil }
func (b stage1Block) EncodeBlock() (string, map[string]any) { return b.name, nil }

type stage1EmptyModel struct{}

func (stage1EmptyModel) BBox(cube.Pos, BlockSource) []cube.BBox          { return nil }
func (stage1EmptyModel) FaceSolid(cube.Pos, cube.Face, BlockSource) bool { return false }
func (stage1EmptyModel) StaticCollisionKind() CollisionKind              { return CollisionEmpty }

type stage1PanicModelBlock struct{}

func (stage1PanicModelBlock) Hash() (uint64, uint64) { return 0, ^uint64(0) }
func (stage1PanicModelBlock) Model() BlockModel      { panic("uninitialized model state") }
func (stage1PanicModelBlock) EncodeBlock() (string, map[string]any) {
	return "test:panic_model", nil
}

type stage1WaterBlock struct{ stage1Block }

func (stage1WaterBlock) LiquidType() string                     { return "water" }
func (stage1WaterBlock) LiquidDepth() int                       { return 5 }
func (stage1WaterBlock) LiquidFalling() bool                    { return true }
func (stage1WaterBlock) SpreadDecay() int                       { return 1 }
func (stage1WaterBlock) BlastResistance() float64               { return 100 }
func (stage1WaterBlock) Harden(cube.Pos, *Tx, *cube.Pos) bool   { return false }
func (stage1WaterBlock) LiquidRemoveBlock(cube.Pos, *Tx, Block) {}
func (b stage1WaterBlock) WithDepth(depth int, falling bool) Liquid {
	return stage1WaterBlock{stage1Block: b.stage1Block}
}

type stage1TestLiquidHolder struct{ stage1Block }

func (stage1TestLiquidHolder) Model() BlockModel { return stage1EmptyModel{} }

func registerTestBlock(r *BasicBlockRegistry, b Block) {
	name, props := b.EncodeBlock()
	r.RegisterBlockState(BlockState{Name: name, Properties: props})
	r.RegisterBlock(b)
}

func TestStage1RegistryCloneAndDeterministicFinalize(t *testing.T) {
	t.Parallel()

	build := func(names []string) *BasicBlockRegistry {
		r := &BasicBlockRegistry{
			blockProperties: make(map[string]map[string]any),
			stateRuntimeIDs: make(map[stateHash]uint32),
			customBlocks:    make(map[string]CustomBlock),
		}
		registerTestBlock(r, stage1TestLiquidHolder{stage1Block{name: "minecraft:air"}})
		registerTestBlock(r, stage1WaterBlock{stage1Block{name: "minecraft:water"}})
		registerTestBlock(r, stage1PanicModelBlock{})
		for _, n := range names {
			registerTestBlock(r, stage1Block{name: n})
		}
		r.Finalize()
		return r
	}

	r1 := build([]string{"test:z", "test:a", "test:m"})
	r2 := build([]string{"test:m", "test:z", "test:a"})
	for _, n := range []string{"minecraft:air", "minecraft:water", "test:a", "test:m", "test:z", "test:panic_model"} {
		id1, ok1 := r1.StateToRuntimeID(n, nil)
		id2, ok2 := r2.StateToRuntimeID(n, nil)
		if !ok1 || !ok2 || id1 != id2 {
			t.Fatalf("StateToRuntimeID(%q) = (%d,%v) vs (%d,%v)", n, id1, ok1, id2, ok2)
		}
	}

	cloned := r1.clone(false)
	waterRID, _ := cloned.StateToRuntimeID("minecraft:water", nil)
	if !cloned.BlockFlags(waterRID).IsLiquid() {
		t.Fatal("cloned registry missing IsLiquid() flag")
	}
	if k, d, f := cloned.LiquidInfo(waterRID); k != 1 || d != 5 || !f {
		t.Fatalf("cloned LiquidInfo = (%d, %d, %v), want (1, 5, true)", k, d, f)
	}
	airRID := cloned.AirRuntimeID()
	if !cloned.BlockFlags(airRID).EmptyCollision() {
		t.Fatal("cloned air missing EmptyCollision()")
	}
}

func TestStage1ColumnInvalidateSubChunkPreservesSiblings(t *testing.T) {
	t.Parallel()

	reg := NewBlockRegistry().(*BasicBlockRegistry)
	reg.Finalize()

	col := newColumn(chunk.New(reg, Overworld.Range()))
	col.CacheFullChunkPayload([]byte("full"))
	key := col.Cache(NewSlotKey("test"))
	key.CacheFullChunkPayload([]byte("keyfull"))
	for i := int16(0); i < 3; i++ {
		col.CacheNetworkSubChunkPayload(i, []byte{byte(i), 1, 2})
		key.CacheNetworkSubChunkPayload(i, []byte{byte(i), 3, 4})
		col.CacheSubChunkBlockEntityPayload(i, []byte{byte(i), 5})
		col.CacheSubChunkHeightMap(i, 1, []int8{int8(i)})
	}

	col.invalidateSubChunk(1)

	if _, ok := col.CachedFullChunkPayload(); ok {
		t.Fatal("own full chunk payload not invalidated")
	}
	if _, ok := key.CachedFullChunkPayload(); ok {
		t.Fatal("keyed full chunk payload not invalidated")
	}
	if _, ok := col.CachedNetworkSubChunkPayload(1); ok {
		t.Fatal("subChunk 1 network payload not invalidated")
	}
	if _, ok := key.CachedNetworkSubChunkPayload(1); ok {
		t.Fatal("keyed subChunk 1 network payload not invalidated")
	}
	for _, sibling := range []int16{0, 2} {
		if p, ok := col.CachedNetworkSubChunkPayload(sibling); !ok || !bytes.Equal(p, []byte{byte(sibling), 1, 2}) {
			t.Errorf("sibling subChunk %d network payload = (%v, %v)", sibling, p, ok)
		}
		if p, ok := key.CachedNetworkSubChunkPayload(sibling); !ok || !bytes.Equal(p, []byte{byte(sibling), 3, 4}) {
			t.Errorf("keyed sibling subChunk %d network payload = (%v, %v)", sibling, p, ok)
		}
	}
}

func TestStage1SetBlockSkipsBlockEntityPayloadInvalidationWhenNonNBT(t *testing.T) {
	t.Parallel()

	reg := NewBlockRegistry().(*BasicBlockRegistry)
	solid := stage1Block{name: "test:stone"}
	registerTestBlock(reg, solid)
	reg.Finalize()

	w := Config{Synchronous: true, RandomTickSpeed: -1, Blocks: reg}.New()
	defer func() { _ = w.Close() }()
	w.Freeze()

	pos := cube.Pos{1, 10, 1}
	<-w.Exec(func(tx *Tx) {
		l := NewLoader(1, w, NopViewer{})
		l.Move(tx, pos.Vec3Centre())
		l.Load(tx, 1)
		c := tx.chunk(chunkPosFromBlockPos(pos))
		sub := c.SubIndex(int16(pos[1]))
		for i := range int16(3) {
			c.CacheNetworkSubChunkPayload(i, []byte{byte(i), 9})
		}
		c.CacheSubChunkBlockEntityPayload(sub, []byte("be-keep"))
		c.CacheChunkBlockEntityPayload(false, []byte("be-full-keep"))
		tx.SetBlock(pos, solid, nil)

		if p, ok := c.CachedSubChunkBlockEntityPayload(sub); !ok || string(p) != "be-keep" {
			t.Errorf("non-NBT setBlock cleared subchunk block entity payload: (%q, %v)", p, ok)
		}
		if p, ok := c.CachedChunkBlockEntityPayload(false); !ok || string(p) != "be-full-keep" {
			t.Errorf("non-NBT setBlock cleared chunk block entity payload: (%q, %v)", p, ok)
		}
		if _, ok := c.CachedNetworkSubChunkPayload(sub); ok {
			t.Errorf("setBlock did not invalidate target subchunk %d", sub)
		}
		for _, sibling := range []int16{0, 1, 2} {
			if sibling != sub {
				if _, ok := c.CachedNetworkSubChunkPayload(sibling); !ok {
					t.Errorf("setBlock unnecessarily invalidated sibling subchunk %d", sibling)
				}
			}
		}
	})
}
