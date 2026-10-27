package world

import (
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// litColumns returns a provider holding columns of stone with caves and
// glowstone from -4 to 4, whose light crosses chunk borders.
func litColumns() *entityLoadProvider {
	stone, _ := chunk.StateToRuntimeID("minecraft:stone", nil)
	glow, _ := chunk.StateToRuntimeID("minecraft:glowstone", nil)
	p := &entityLoadProvider{columns: map[ChunkPos]*chunk.Column{}}
	for cx := int32(-4); cx <= 4; cx++ {
		for cz := int32(-4); cz <= 4; cz++ {
			c := chunk.New(DefaultBlockRegistry, Overworld.Range())
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(Overworld.Range().Min()); y < 60; y++ {
						if y > 20 && y < 30 && (int(x)+int(z))%5 < 2 {
							continue
						}
						c.SetBlock(x, y, z, 0, stone)
					}
				}
			}
			c.SetBlock(uint8(cx&7), 25, 15, 0, glow)
			c.SetBlock(0, 59, uint8(cz&7), 0, glow)
			p.columns[ChunkPos{cx, cz}] = &chunk.Column{Chunk: c}
		}
	}
	return p
}

// lightAround returns the block and sky light of the blocks of the chunks
// within one of the origin, read once the chunks within two of it, whose
// light reaches them, are ready.
func lightAround(t *testing.T, w *World, load func(tx *Tx)) map[cube.Pos][2]uint8 {
	out := map[cube.Pos][2]uint8{}
	<-w.Exec(func(tx *Tx) { load(tx) })
	deadline := time.Now().Add(time.Minute)
	for {
		ready := true
		<-w.Exec(func(tx *Tx) {
			for x := int32(-2); x <= 2; x++ {
				for z := int32(-2); z <= 2; z++ {
					if c, ok := w.chunks[ChunkPos{x, z}]; !ok || !c.Ready() {
						ready = false
					}
				}
			}
		})
		if ready || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	<-w.Exec(func(tx *Tx) {
		for x := -16; x < 32; x++ {
			for z := -16; z < 32; z++ {
				for y := 15; y < 66; y++ {
					pos := cube.Pos{x, y, z}
					out[pos] = [2]uint8{tx.BlockLight(pos), tx.SkyLight(pos)}
				}
			}
		}
	})
	return out
}

// TestLoadedColumnsLightMatchesSynchronousLoading checks columns read from
// the provider of a world ticking on its own goroutine, whose light is
// filled on other goroutines, are lit as a synchronous world lights them,
// whether a block read loads them or a loader does.
func TestLoadedColumnsLightMatchesSynchronousLoading(t *testing.T) {
	DefaultBlockRegistry.Finalize()
	readAll := func(tx *Tx) {
		for x := int32(-3); x <= 3; x++ {
			for z := int32(-3); z <= 3; z++ {
				tx.chunk(ChunkPos{x, z})
			}
		}
	}
	sync := Config{Synchronous: true, Provider: litColumns(), RandomTickSpeed: -1}.New()
	want := lightAround(t, sync, readAll)
	_ = sync.Close()

	async := Config{Provider: litColumns(), RandomTickSpeed: -1}.New()
	got := lightAround(t, async, readAll)
	_ = async.Close()

	loaded := Config{Provider: litColumns(), RandomTickSpeed: -1}.New()
	loader := NewLoader(3, loaded, NopViewer{})
	viaLoader := lightAround(t, loaded, func(tx *Tx) {
		loader.Move(tx, mgl64.Vec3{8, 64, 8})
		loader.Load(tx, 100)
	})
	_ = loaded.Close()

	for name, light := range map[string]map[cube.Pos][2]uint8{"block reads": got, "a loader": viaLoader} {
		differ := 0
		for pos, w := range want {
			if g := light[pos]; g != w {
				if differ++; differ <= 3 {
					t.Errorf("loaded by %s: light at %v is %v, want %v", name, pos, g, w)
				}
			}
		}
		if differ > 0 {
			t.Errorf("loaded by %s: %d light levels differ", name, differ)
		}
	}
}

// storingProvider remembers the columns stored through it.
type storingProvider struct {
	*entityLoadProvider
	mu     sync.Mutex
	stored map[ChunkPos]*chunk.Column
}

func (p *storingProvider) StoreColumn(pos ChunkPos, _ Dimension, col *chunk.Column) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stored[pos] = col
	return nil
}

// TestClosingColumnWhileItsLightFillsSavesItsEntities checks a column read
// from the provider and closed before its light was filled, as when a
// player teleports away from chunks just loading, saves the entities read
// with it rather than dropping them as those of a column still generating.
func TestClosingColumnWhileItsLightFillsSavesItsEntities(t *testing.T) {
	pos := ChunkPos{0, 0}
	provider := &storingProvider{
		entityLoadProvider: &entityLoadProvider{NopProvider: NopProvider{WorldData: &WorldDataMemory{}}, columns: map[ChunkPos]*chunk.Column{}},
		stored:             map[ChunkPos]*chunk.Column{},
	}
	w := Config{
		Provider:        provider,
		RandomTickSpeed: -1,
		Entities:        (EntityRegistryConfig{}).New([]EntityType{testEntityType{}}),
	}.New()
	provider.columns[pos] = loadedTestEntityColumn(w, 1, mgl64.Vec3{1, 4, 1})

	// Keep every light filler busy until the column closed.
	for range cap(w.lightFills) {
		w.lightFills <- struct{}{}
	}
	<-w.Exec(func(tx *Tx) {
		col, err := w.loadChunk(pos)
		if err != nil {
			t.Fatalf("load chunk: %v", err)
		}
		if col.Ready() {
			t.Fatal("column was ready before its light was filled")
		}
		time.AfterFunc(20*time.Millisecond, func() {
			for range cap(w.lightFills) {
				<-w.lightFills
			}
		})
		w.closeChunk(tx, pos, col)
	})
	_ = w.Close()

	provider.mu.Lock()
	defer provider.mu.Unlock()
	col, ok := provider.stored[pos]
	if !ok {
		t.Fatal("column closed while its light filled was not saved")
	}
	if len(col.Entities) != 1 {
		t.Fatalf("saved column has %d entities, want 1", len(col.Entities))
	}
}
