package block

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	world.FinaliseBlockRegistry()
}

func TestEnsureEndPortalSpawnBuildsPlatform(t *testing.T) {
	w := world.Config{Dim: world.End, Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()

	var err error
	<-w.Exec(func(tx *world.Tx) {
		tx.World().SetSpawn(cube.Pos{10, tx.Range()[1] + 20, -5})
		spawn := ensureEndPortalSpawn(tx)

		if spawn != (cube.Pos{10, tx.Range()[0] + 1, -5}) {
			err = fmt.Errorf("unexpected spawn position: got %v want %v", spawn, cube.Pos{10, tx.Range()[0] + 1, -5})
			return
		}

		baseY := spawn.Y() - 1
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				pos := cube.Pos{spawn.X() + x, baseY, spawn.Z() + z}
				if b := tx.Block(pos); b != nil {
					if _, ok := b.(Obsidian); ok {
						continue
					}
					err = fmt.Errorf("platform block at %v is not obsidian: %T", pos, b)
					return
				}
			}
		}

		for y := 0; y < 2; y++ {
			pos := cube.Pos{spawn.X(), spawn.Y() + y, spawn.Z()}
			if b := tx.Block(pos); b != nil {
				if _, ok := b.(Air); ok {
					continue
				}
				err = fmt.Errorf("spawn column not clear at %v: %T", pos, b)
				return
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}
