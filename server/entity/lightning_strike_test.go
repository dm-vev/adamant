package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

func TestLightningOnRodPowersItAndCleansCopper(t *testing.T) {
	w := newDeathTestWorld(t)
	rod := cube.Pos{0, 65, 0}
	oxidised := block.Copper{Oxidation: block.OxidisedOxidation()}
	<-w.Exec(func(tx *world.Tx) {
		// Surround the rod with oxidised copper so every random cleaning step
		// lands on copper.
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				for y := 64; y <= 65; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, oxidised, nil)
				}
			}
		}
		tx.SetBlock(rod, block.LightningRod{Facing: cube.FaceUp, Oxidation: block.OxidisedOxidation()}, nil)
		tx.AddEntity(entity.NewLightning(world.EntitySpawnOpts{Position: rod.Vec3()}))
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		r := tx.Block(rod).(block.LightningRod)
		if !r.Powered || r.Oxidation == block.OxidisedOxidation() {
			t.Fatalf("struck rod = %#v, want powered and less oxidised", r)
		}
		cleaned := 0
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				for y := 64; y <= 65; y++ {
					if c, ok := tx.Block(cube.Pos{x, y, z}).(block.Copper); ok && c.Oxidation != block.OxidisedOxidation() {
						cleaned++
					}
				}
			}
		}
		if cleaned == 0 {
			t.Fatal("the strike cleaned no surrounding copper")
		}
	})
}
