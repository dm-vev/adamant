package playerdb

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func TestRespawnConfigPersistsAcrossProviderRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "players")
	provider, err := NewProvider(path)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	respawnPosition := cube.Pos{12, 64, -5}
	config := player.Config{
		UUID:                id,
		Inventory:           inventory.New(36, nil),
		OffHand:             inventory.New(1, nil),
		Armour:              inventory.NewArmour(nil),
		EnderChestInventory: inventory.New(27, nil),
		RespawnDimension:    world.Nether,
		RespawnPosition:     respawnPosition,
		RespawnRotation:     cube.Rotation{35, -12},
		HasRespawnPoint:     true,
	}
	var end, nether *world.World
	end = world.Config{
		Dim: world.End,
		PortalDestination: func(dim world.Dimension) *world.World {
			if dim == world.Nether {
				return nether
			}
			return nil
		},
		DefaultWorld: func() *world.World { return nether },
	}.New()
	nether = world.Config{
		Dim: world.Nether,
		PortalDestination: func(dim world.Dimension) *world.World {
			if dim == world.End {
				return end
			}
			return nil
		},
	}.New()
	t.Cleanup(func() {
		_ = end.Close()
		_ = nether.Close()
	})
	if err := provider.Save(id, config, end); err != nil {
		t.Fatal(err)
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}

	provider, err = NewProvider(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	loaded, loadedWorld, err := provider.Load(id, func(dim world.Dimension) *world.World {
		if dim == world.End {
			return end
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if loadedWorld != end {
		t.Fatalf("loaded player world = %p, want End world %p", loadedWorld, end)
	}
	if !loaded.HasRespawnPoint || loaded.RespawnDimension != world.Nether || loaded.RespawnPosition != respawnPosition || loaded.RespawnRotation != (cube.Rotation{35, -12}) {
		t.Fatalf("respawn config did not survive provider restart: %+v", loaded)
	}
	if err := nether.Do(func(tx *world.Tx) {
		tx.SetBlock(respawnPosition, block.RespawnAnchor{Charge: 2}, nil)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	origin := mgl64.Vec3{5.5, 64, 5.5}
	handle := world.EntitySpawnOpts{Position: origin, ID: id}.New(player.Type, loaded)
	if err := end.Do(func(tx *world.Tx) {
		tx.SetBlock(cube.PosFromVec3(origin), block.EndPortal{}, nil)
		e := tx.AddEntity(handle)
		(block.EndPortal{}).EntityInside(cube.PosFromVec3(origin), tx, e)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for handle.World() != nether && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if handle.World() != nether {
		t.Fatalf("End exit selected world %p, want persisted respawn dimension Nether", handle.World())
	}
	if err := nether.Do(func(tx *world.Tx) {
		e, ok := handle.Entity(tx)
		if !ok || !e.Position().ApproxEqual(respawnPosition.Vec3Middle()) {
			t.Fatalf("reloaded player End return entity=%v ok=%t; want anchor position %v", e, ok, respawnPosition.Vec3Middle())
		}
		if anchor, ok := tx.Block(respawnPosition).(block.RespawnAnchor); !ok || anchor.Charge != 2 {
			t.Fatalf("End exit consumed the charged anchor: %T %+v", tx.Block(respawnPosition), anchor)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
