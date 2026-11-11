package advanced

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Seed 42 holds a wide ocean around chunk (24, 28), with the kelp forests of
// its ocean and deep ocean biomes and seagrass meadows between them.
const (
	oceanPlantsSeed   = 42
	oceanPlantsRadius = 1
)

var oceanPlantsCentre = world.ChunkPos{24, 28}

// oceanPlantCells returns the kelp and seagrass blocks within radius chunks
// of centre, by position.
func oceanPlantCells(tx *world.Tx, centre world.ChunkPos, radius int) map[cube.Pos]world.Block {
	cells := map[cube.Pos]world.Block{}
	for cx := int(centre[0]) - radius; cx <= int(centre[0])+radius; cx++ {
		for cz := int(centre[1]) - radius; cz <= int(centre[1])+radius; cz++ {
			for x := range 16 {
				for z := range 16 {
					// The floor of this ocean lies between 30 and 62.
					for y := 20; y <= 64; y++ {
						pos := cube.Pos{cx*16 + x, y, cz*16 + z}
						switch b := tx.Block(pos).(type) {
						case block.Kelp, block.SeaGrass:
							cells[pos] = b
						}
					}
				}
			}
		}
	}
	return cells
}

// TestOceanPlantsSurviveGeneration generates an ocean floor with kelp and
// seagrass and runs the world over it. Java does not update the neighbours of
// anything it generates, and ticks only the fluids of the positions it marks,
// which find nothing to flow into around a plant: the plants stay, and no item
// of a broken plant, nor a block fallen from under one, floats up. Random
// ticks run fast, so that kelp grows: a kelp head that grows changes nothing
// under it, and the gravel floor of a column, which an aquifer may leave over
// water, stays.
func TestOceanPlantsSurviveGeneration(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	// A synchronous world generates the chunks it is asked for and those
	// around them, and a tick promotes them as Java promotes a chunk whose
	// neighbours are generated.
	w := world.Config{
		Generator:       NewOverworld(oceanPlantsSeed),
		Entities:        entity.DefaultRegistry,
		RandomTickSpeed: 300,
		Synchronous:     true,
	}.New()
	t.Cleanup(func() { _ = w.Close() })

	var before map[cube.Pos]world.Block
	<-w.Exec(func(tx *world.Tx) {
		for dx := -oceanPlantsRadius - 1; dx <= oceanPlantsRadius+1; dx++ {
			for dz := -oceanPlantsRadius - 1; dz <= oceanPlantsRadius+1; dz++ {
				tx.Block(cube.Pos{(int(oceanPlantsCentre[0]) + dx) * 16, 0, (int(oceanPlantsCentre[1]) + dz) * 16})
			}
		}
		before = oceanPlantCells(tx, oceanPlantsCentre, oceanPlantsRadius)
	})
	kelp, seagrass := 0, 0
	for _, b := range before {
		if _, ok := b.(block.Kelp); ok {
			kelp++
		} else {
			seagrass++
		}
	}
	if kelp < 200 || seagrass < 200 {
		t.Fatalf("generation left %d kelp and %d seagrass, want a forest and a meadow of at least 200 each", kelp, seagrass)
	}

	// Fluid ticks wait five ticks, and the blocks they disturb fall within
	// a few more.
	for range 100 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		after := oceanPlantCells(tx, oceanPlantsCentre, oceanPlantsRadius)
		lost, example := 0, ""
		for pos, b := range before {
			if _, ok := after[pos]; !ok {
				if lost++; example == "" {
					example = fmt.Sprintf("%T at %v", b, pos)
				}
			}
		}
		if lost != 0 {
			t.Errorf("%d of %d plants broke in 100 ticks, such as %s", lost, len(before), example)
		}
		entities := map[string]int{}
		for e := range tx.Entities() {
			entities[e.H().Type().EncodeEntity()]++
		}
		for name, n := range entities {
			if name == "minecraft:item" || name == "minecraft:falling_block" {
				t.Errorf("%d %s entities in the area, want none: something broke or fell", n, name)
			}
		}
	})
}

// TestKelpColumnsStandOnTheirFloor generates chunks where the seagrass of one
// origin chunk and the kelp of another meet. Java places a kelp column on a
// sturdy floor or not at all, so the cell under the lowest kelp of a column
// holds a floor, whichever feature took the cell it would have started in.
func TestKelpColumnsStandOnTheirFloor(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	g := NewOverworld(oceanPlantsSeed)
	registry := world.DefaultBlockRegistry
	kelp := map[uint32]struct{}{}
	for age := range 26 {
		kelp[world.BlockRuntimeID(block.Kelp{Age: age})] = struct{}{}
	}
	columns := 0
	// These hold the columns that stood on seagrass before the fix.
	for _, pos := range []world.ChunkPos{{20, 26}, {26, 30}} {
		c := chunk.New(registry, world.Overworld.Range())
		g.GenerateChunk(pos, c, world.GenerationContext{})
		for x := range uint8(16) {
			for z := range uint8(16) {
				for y := int16(world.Overworld.Range().Min()) + 1; y <= 70; y++ {
					if _, isKelp := kelp[c.Block(x, y, z, 0)]; !isKelp {
						continue
					}
					below := world.NativeStateAt(c, x, y-1, z)
					if _, onKelp := kelp[below.Block]; onKelp {
						continue
					}
					columns++
					shape, err := gamedata.BlockGenerationShapeID(below.CanonicalID(registry))
					if err != nil || shape.Sturdy&(1<<uint(cube.FaceUp)) == 0 {
						name, _ := world.CanonicalState(registry, below)
						t.Errorf("kelp at chunk %v (%d, %d, %d) stands on %s, which is no floor", pos, x, y, z, name.Name)
					}
				}
			}
		}
	}
	if columns < 10 {
		t.Fatalf("found %d kelp columns, want a forest", columns)
	}
}
