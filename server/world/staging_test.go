package world_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/google/uuid"
)

// sidecarProvider keeps the namespaced data, maps and spawn points of a
// world in memory, and records their writes. Writes of world data fail
// while failData is set.
type sidecarProvider struct {
	world.NopProvider

	mu       sync.Mutex
	data     map[string][]byte
	maps     map[int64]world.MapData
	spawns   map[uuid.UUID]cube.Pos
	nextID   int64
	writes   []string
	failData bool
}

func newSidecarProvider() *sidecarProvider {
	return &sidecarProvider{data: map[string][]byte{}, maps: map[int64]world.MapData{}, spawns: map[uuid.UUID]cube.Pos{}, nextID: -1}
}

func (p *sidecarProvider) record(w string) { p.writes = append(p.writes, w) }

// takeWrites returns the writes recorded since the last call, sorted.
func (p *sidecarProvider) takeWrites() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	w := p.writes
	p.writes = nil
	slices.Sort(w)
	return w
}

func (p *sidecarProvider) LoadWorldData(namespace string) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if b, ok := p.data[namespace]; ok {
		return slices.Clone(b), nil
	}
	return nil, leveldb.ErrNotFound
}

func (p *sidecarProvider) SaveWorldData(namespace string, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failData && namespace == "test:data" {
		return errors.New("injected: disk full")
	}
	p.data[namespace] = slices.Clone(data)
	p.record("data " + namespace)
	return nil
}

func (p *sidecarProvider) LoadMap(id int64) (world.MapData, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if m, ok := p.maps[id]; ok {
		return m, nil
	}
	return world.MapData{}, leveldb.ErrNotFound
}

func (p *sidecarProvider) StoreMap(data world.MapData) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.maps[data.ID] = data
	p.record(fmt.Sprintf("map %d", data.ID))
	return nil
}

func (p *sidecarProvider) AllocateMapID() (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := p.nextID
	p.nextID--
	p.record("map ID")
	return id, nil
}

func (p *sidecarProvider) JavaMapID(nativeID int64) (int32, error) { return int32(-nativeID), nil }
func (p *sidecarProvider) NativeMapID(javaID int32) (int64, error) { return -int64(javaID), nil }

func (p *sidecarProvider) LoadPlayerSpawnPosition(id uuid.UUID) (cube.Pos, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pos, ok := p.spawns[id]
	return pos, ok, nil
}

func (p *sidecarProvider) SavePlayerSpawnPosition(id uuid.UUID, pos cube.Pos) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.spawns[id] = pos
	p.record("spawn " + id.String())
	return nil
}

// TestSidecarWritesWaitForTheNextSave checks that namespaced world data and
// maps are written to the provider by the next save of the world, not as
// they are set, that the world reads them back meanwhile, that the ID of a
// map is reserved and the spawn point of a player written at once, and that
// a write that fails is written by the save after.
func TestSidecarWritesWaitForTheNextSave(t *testing.T) {
	for _, synchronous := range []bool{true, false} {
		t.Run(fmt.Sprintf("synchronous=%v", synchronous), func(t *testing.T) {
			t.Parallel()
			p := newSidecarProvider()
			w := world.Config{Provider: p, Generator: world.NopGenerator{}, RandomTickSpeed: -1, Synchronous: synchronous}.New()
			t.Cleanup(func() { _ = w.Close() })
			p.takeWrites()

			steve := uuid.MustParse("00000000-0000-4000-8000-00000000057e")
			spawn := cube.Pos{10, 64, 10}
			var m world.MapData
			<-w.Exec(func(tx *world.Tx) {
				if err := w.SaveWorldData("test:data", []byte("one")); err != nil {
					t.Errorf("save world data: %v", err)
				}
				w.SetPlayerSpawn(steve, spawn)
				var err error
				if m, err = tx.Maps().Create(cube.Pos{}, 0); err != nil {
					t.Errorf("create a map: %v", err)
				}
			})
			if got, want := p.takeWrites(), []string{"map ID", "spawn " + steve.String()}; !slices.Equal(got, want) {
				t.Errorf("before the save the provider was written %q, want %q: the reservation of the map's ID and the spawn point", got, want)
			}
			if b, err := w.LoadWorldData("test:data"); err != nil || string(b) != "one" {
				t.Errorf("world data read back before the save = %q, %v, want \"one\"", b, err)
			}
			if pos := w.PlayerSpawn(steve); pos != spawn {
				t.Errorf("spawn point read back before the save = %v, want %v", pos, spawn)
			}
			if got, err := w.MapData(m.ID); err != nil || got.ID != m.ID || got.JavaID != m.JavaID || !got.HasJavaID {
				t.Errorf("map read back before the save = %+v, %v, want map %d", got.ID, err, m.ID)
			}

			w.Save()
			got := p.takeWrites()
			for _, want := range []string{fmt.Sprintf("map %d", m.ID), "data test:data"} {
				if !slices.Contains(got, want) {
					t.Errorf("the save wrote %q, without %q", got, want)
				}
			}

			// A write that fails waits for the next save.
			p.mu.Lock()
			p.failData = true
			p.mu.Unlock()
			if err := w.SaveWorldData("test:data", []byte("two")); err != nil {
				t.Errorf("save world data: %v", err)
			}
			w.Save()
			p.mu.Lock()
			p.failData = false
			p.mu.Unlock()
			if b, err := w.LoadWorldData("test:data"); err != nil || string(b) != "two" {
				t.Errorf("world data read back after its write failed = %q, %v, want \"two\"", b, err)
			}
			w.Save()
			if b, _ := p.LoadWorldData("test:data"); string(b) != "two" {
				t.Errorf("the provider holds %q after the save after the failed write, want \"two\"", b)
			}
		})
	}
}
