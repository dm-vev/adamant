package chunk

import (
	"sync"
	"testing"

	"github.com/dm-vev/adamant/server/block/cube"
)

func TestLightQueueWrapGrowAndPoolRoundTrip(t *testing.T) {
	t.Parallel()

	q := newLightQueue(64)
	for i := range 40 {
		q.push(lightNode{x: int32(i), level: uint8(i & 15)})
	}
	for i := range 25 {
		got, ok := q.pop()
		if !ok || got.x != int32(i) {
			t.Fatalf("pop #%d = (%+v, %v), want x=%d", i, got, ok, i)
		}
	}
	// Wrap tail past head and trigger grow() across two doublings.
	for i := 40; i < 200; i++ {
		q.push(lightNode{x: int32(i), level: uint8(i & 15)})
	}
	for i := 25; i < 200; i++ {
		got, ok := q.pop()
		if !ok || got.x != int32(i) {
			t.Fatalf("pop after grow #%d = (%+v, %v), want x=%d", i, got, ok, i)
		}
	}
	if _, ok := q.pop(); ok {
		t.Fatal("expected empty queue after draining all items")
	}

	// Oversized buffer is shrunk back cleanly on putLightQueue and recycled without error.
	large := newLightQueue(32768)
	putLightQueue(large)
	if len(large.nodes) != initialLightQueueCapacity {
		t.Fatalf("putLightQueue kept %d nodes, want reset to %d", len(large.nodes), initialLightQueueCapacity)
	}
}

func TestLightNodePoolsConcurrentSafety(t *testing.T) {
	t.Parallel()

	const workers = 8
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for iter := range 20 {
				q := relightLevelQueues.Get().(*levelQueue)
				for i := range 300 {
					q.push(lightNode{x: int32(id), y: int32(i), z: int32(iter), level: uint8((id + i) & 15)})
				}
				count := 0
				for !q.empty() {
					if _, ok := q.pop(); ok {
						count++
					}
				}
				if count != 300 {
					t.Errorf("popped %d nodes, want 300", count)
				}
				putLevelQueue(q)
			}
		}(w)
	}
	wg.Wait()
}

type poolTestRegistry struct{}

func (poolTestRegistry) BlockCount() int                   { return 1 }
func (poolTestRegistry) AirRuntimeID() uint32              { return 0 }
func (poolTestRegistry) FilteringBlock(uint32) uint8       { return 0 }
func (poolTestRegistry) LightBlock(uint32) uint8           { return 0 }
func (poolTestRegistry) RandomTickBlock(uint32) bool       { return false }
func (poolTestRegistry) NBTBlock(uint32) bool              { return false }
func (poolTestRegistry) LiquidDisplacingBlock(uint32) bool { return false }
func (poolTestRegistry) LiquidBlock(uint32) bool           { return false }
func (poolTestRegistry) RuntimeIDToState(rid uint32) (string, map[string]any, bool) {
	if rid == 0 {
		return "minecraft:air", nil, true
	}
	return "", nil, false
}
func (poolTestRegistry) StateToRuntimeID(name string, _ map[string]any) (uint32, bool) {
	return 0, name == "minecraft:air"
}

func TestLightAreaConcurrentFillSpreadAndRelight(t *testing.T) {
	t.Parallel()

	const workers = 4
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := New(poolTestRegistry{}, cube.Range{0, 63})
			area := LightArea([]*Chunk{c}, 0, 0)
			area.Fill()
			area.Spread()
			area.relight([]cube.Pos{{4, 10, 4}}, BlockLight, func(cube.Pos) uint8 { return 12 })
			if got := c.Sub()[0].BlockLight(4, 10, 4); got != 12 {
				t.Errorf("block light at source = %d, want 12", got)
			}
			if got := c.Sub()[0].BlockLight(5, 10, 4); got != 11 {
				t.Errorf("propagated block light = %d, want 11", got)
			}
		}()
	}
	wg.Wait()
}
