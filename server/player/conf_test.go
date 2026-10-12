package player

import (
	"context"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

func TestConfigPortalTravelCooldown(t *testing.T) {
	data := &world.EntityData{}
	Config{}.Apply(data)
	if got := data.Data.(*playerData).portalTravel.Cooldown; got != 15*time.Second {
		t.Fatalf("unexpected portal cooldown: %v", got)
	}
}

func TestEndReturnUsesRespawnBlockInDestinationDimension(t *testing.T) {
	for _, test := range []struct {
		name      string
		dimension world.Dimension
		respawn   world.Block
		wantSpawn bool
	}{
		{name: "charged Nether anchor", dimension: world.Nether, respawn: block.RespawnAnchor{Charge: 2}},
		{name: "uncharged Nether anchor falls back", dimension: world.Nether, respawn: block.RespawnAnchor{}, wantSpawn: true},
		{name: "anchor is invalid outside Nether", dimension: world.Overworld, respawn: block.RespawnAnchor{Charge: 2}, wantSpawn: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			pos := cube.Pos{12, 64, -5}
			provider := &playerSpawnPositionProvider{spawn: pos}
			w := world.Config{Dim: test.dimension, Provider: provider, Synchronous: true}.New()
			t.Cleanup(func() { _ = w.Close() })
			id := uuid.New()
			w.SetPlayerSpawn(id, pos)
			if err := w.Do(func(tx *world.Tx) { tx.SetBlock(pos, test.respawn, nil) }).Wait(context.Background()); err != nil {
				t.Fatal(err)
			}

			data := &world.EntityData{}
			Config{
				UUID: id, RespawnDimension: test.dimension, RespawnPosition: pos, HasRespawnPoint: true,
			}.Apply(data)
			spawnPoint := data.Data.(*playerData).portalTravel.SpawnPoint
			var got cube.Pos
			if err := w.Do(func(tx *world.Tx) {
				got = cube.PosFromVec3(spawnPoint(tx))
				if !test.wantSpawn {
					if anchor, ok := tx.Block(pos).(block.RespawnAnchor); !ok || anchor.Charge != 2 {
						t.Fatalf("charged anchor after End return = %T %+v; want unchanged charge 2", tx.Block(pos), anchor)
					}
				}
			}).Wait(context.Background()); err != nil {
				t.Fatal(err)
			}

			want := pos
			if test.wantSpawn {
				want = w.Spawn()
			}
			if got != want {
				t.Fatalf("End return spawn = %v, want %v", got, want)
			}
		})
	}
}

type playerSpawnPositionProvider struct {
	world.NopProvider
	spawn cube.Pos
}

func (p *playerSpawnPositionProvider) LoadPlayerSpawnPosition(uuid.UUID) (cube.Pos, bool, error) {
	return p.spawn, true, nil
}

func (p *playerSpawnPositionProvider) SavePlayerSpawnPosition(_ uuid.UUID, spawn cube.Pos) error {
	p.spawn = spawn
	return nil
}
