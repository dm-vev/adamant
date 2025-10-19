package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	worldFinaliseBlockRegistry()
}

func TestEnsureEndPortalSpawnBuildsPlatform(t *testing.T) {
	w := world.Config{Dim: world.End, Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		tx.World().SetSpawn(cube.Pos{10, tx.Range()[1] + 20, -5})
		spawn := ensureEndPortalSpawn(tx)

		if spawn != (cube.Pos{10, tx.Range()[0] + 1, -5}) {
			t.Fatalf("unexpected spawn position: got %v want %v", spawn, cube.Pos{10, tx.Range()[0] + 1, -5})
		}

		baseY := spawn.Y() - 1
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				pos := cube.Pos{spawn.X() + x, baseY, spawn.Z() + z}
				if _, ok := tx.Block(pos).(Obsidian); !ok {
					t.Fatalf("platform block at %v is not obsidian: %T", pos, tx.Block(pos))
				}
			}
		}

		for y := 0; y < 2; y++ {
			pos := cube.Pos{spawn.X(), spawn.Y() + y, spawn.Z()}
			if b := tx.Block(pos); b != nil {
				t.Fatalf("spawn column not clear at %v: %T", pos, b)
			}
		}
	})
}
