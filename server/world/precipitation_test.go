package world_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
)

func TestPrecipitationFreezesWaterEdgesAndCapsSnow(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		// Western half: a frozen-ocean pool; eastern half: snowy plains stone.
		for x := range 16 {
			for z := range 16 {
				for y := -1; y <= 70; y++ {
					if x < 8 {
						tx.SetBiome(cube.Pos{x, y, z}, biome.FrozenOcean{})
					} else {
						tx.SetBiome(cube.Pos{x, y, z}, biome.SnowyPlains{})
					}
				}
				tx.SetBlock(cube.Pos{x, 62, z}, block.Stone{}, nil)
				if x < 8 {
					tx.SetBlock(cube.Pos{x, 63, z}, block.Water{Still: true, Depth: 8}, nil)
				}
			}
		}
		for range 4000 {
			w.TickPrecipitation(tx, world.ChunkPos{0, 0})
		}
		edgeIce, innerIce := 0, 0
		for x := range 8 {
			for z := range 16 {
				if _, ok := tx.Block(cube.Pos{x, 63, z}).(block.Ice); ok {
					if x == 0 || x == 7 || z == 0 || z == 15 {
						edgeIce++
					} else {
						innerIce++
					}
				}
			}
		}
		if edgeIce == 0 {
			t.Fatal("no exposed water froze")
		}
		if innerIce != 0 && edgeIce < 4 {
			t.Fatalf("water surrounded by water froze before the edges: edge=%d inner=%d", edgeIce, innerIce)
		}
		if _, ok := tx.Block(cube.Pos{12, 63, 8}).(block.SnowLayer); ok {
			t.Fatal("snow accumulated without rain")
		}

		w.StartRaining(time.Hour)
		if err := w.SetGameRule("max_snow_accumulation_height", 3); err != nil {
			t.Fatal(err)
		}
		for range 40000 {
			w.TickPrecipitation(tx, world.ChunkPos{0, 0})
		}
		snowy := 0
		for x := 8; x < 16; x++ {
			for z := range 16 {
				if s, ok := tx.Block(cube.Pos{x, 63, z}).(block.SnowLayer); ok {
					snowy++
					if _, props := s.EncodeBlock(); props["height"].(int32) > 2 {
						t.Fatalf("snow at %d,%d grew past max_snow_accumulation_height 3: %v", x, z, props)
					}
				}
			}
		}
		if snowy == 0 {
			t.Fatal("no snow accumulated while raining in snowy plains")
		}
	})
}
