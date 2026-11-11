package player

import (
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// mapProvider is a provider that keeps map data in memory.
type mapProvider struct {
	world.NopProvider
	mu   sync.Mutex
	maps map[int64]world.MapData
}

func (m *mapProvider) LoadMap(id int64) (world.MapData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.maps[id]; ok {
		return d, nil
	}
	return world.MapData{}, errors.New("no such map")
}

func (m *mapProvider) StoreMap(d world.MapData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maps[d.ID] = d
	return nil
}

func (m *mapProvider) AllocateMapID() (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.maps) + 1), nil
}

func (m *mapProvider) JavaMapID(id int64) (int32, error)   { return int32(id), nil }
func (m *mapProvider) NativeMapID(id int32) (int64, error) { return int64(id), nil }

func TestPlayerExploreMap(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry, Provider: &mapProvider{maps: map[int64]world.MapData{}}}.New()
	t.Cleanup(func() { _ = w.Close() })
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		created, err := tx.Maps().Create(cube.Pos{0, 100, 0}, 0)
		if err != nil {
			t.Fatalf("CreateMap: %v", err)
		}
		data, err := p.ExploreMap(created.ID)
		if err != nil {
			t.Fatalf("ExploreMap: %v", err)
		}
		if data.ID != created.ID || data.CenterX != created.CenterX || data.CenterZ != created.CenterZ {
			t.Fatalf("ExploreMap returned map %d at %d, %d, want map %d at %d, %d", data.ID, data.CenterX, data.CenterZ, created.ID, created.CenterX, created.CenterZ)
		}
		if _, err := p.ExploreMap(created.ID + 1000); err == nil {
			t.Fatal("ExploreMap of a map that does not exist succeeded")
		}
	})
}
