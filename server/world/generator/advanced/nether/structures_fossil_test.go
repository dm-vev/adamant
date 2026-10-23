package nether

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func TestFossilPostProcessExpandsChunkClipAndRepeatsExactly(t *testing.T) {
	// Mapped Java 26.3 start fixture: seed 1, chunk (1026,40), fossil_2,
	// bounds [16428,50,637]-[16432,50,641]. Its source postProcess emits the
	// following ten bone blocks for either intersecting chunk's expanded clip.
	want := map[[3]int32]string{
		{16428, 50, 639}: "minecraft:bone_block",
		{16429, 50, 639}: "minecraft:bone_block",
		{16430, 50, 639}: "minecraft:bone_block",
		{16430, 50, 640}: "minecraft:bone_block",
		{16431, 50, 639}: "minecraft:bone_block",
		{16432, 50, 637}: "minecraft:bone_block",
		{16432, 50, 638}: "minecraft:bone_block",
		{16432, 50, 639}: "minecraft:bone_block",
		{16432, 50, 640}: "minecraft:bone_block",
		{16432, 50, 641}: "minecraft:bone_block",
	}
	g := NewNether(1)
	air, ok := world.BlockByName("minecraft:air", nil)
	if !ok {
		t.Fatal("air block is not registered")
	}
	for _, pos := range []world.ChunkPos{{1026, 39}, {1027, 39}} {
		render := func() []structure.Placement {
			var placements []structure.Placement
			g.featureSource.placeNetherFossilStep(pos, structure.ProcessorEnvironment{
				BaseBlock: func(int32, int32, int32) world.Block { return air },
				Random:    g.featureSource.structureStepRandom(pos, 2, 7),
			}, func(p structure.Placement) { placements = append(placements, p) })
			return placements
		}
		first, second := render(), render()
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("fossil placements changed on repeat for chunk %v", pos)
		}
		actual := make(map[[3]int32]string)
		for _, placement := range first {
			for _, block := range placement.Blocks {
				actual[block.Pos] = block.State.Name
			}
		}
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("expanded fossil writes for chunk %v = %v, want %v", pos, actual, want)
		}
	}
}

func TestFossilDriedGhastPlacementAndTerrainBlocking(t *testing.T) {
	// Mapped Java 26.3 seed 1 fossil_8: postProcess places an east-facing
	// dried ghast at [9,42,-633] when that base-terrain position is air.
	ghastPos := [3]int32{9, 42, -633}
	g := NewNether(1)
	air, ok := world.BlockByName("minecraft:air", nil)
	if !ok {
		t.Fatal("air block is not registered")
	}
	netherrack, ok := world.BlockByName("minecraft:netherrack", nil)
	if !ok {
		t.Fatal("netherrack block is not registered")
	}
	render := func(blocked bool) []structure.Placement {
		var placements []structure.Placement
		g.featureSource.placeNetherFossilStep(world.ChunkPos{0, -40}, structure.ProcessorEnvironment{
			BaseBlock: func(x, y, z int32) world.Block {
				if blocked && [3]int32{x, y, z} == ghastPos {
					return netherrack
				}
				return air
			},
			Random: g.featureSource.structureStepRandom(world.ChunkPos{0, -40}, 2, 7),
		}, func(p structure.Placement) { placements = append(placements, p) })
		return placements
	}
	var placed bool
	for _, placement := range render(false) {
		for _, block := range placement.Blocks {
			if block.Pos != ghastPos || block.State.Name != "minecraft:dried_ghast" {
				continue
			}
			placed = true
			if block.State.Properties["facing"] != "east" || block.State.Properties["hydration"] != int32(0) || block.State.Properties["waterlogged"] != false {
				t.Fatalf("dried ghast at %v has unexpected state %v", ghastPos, block.State)
			}
		}
	}
	if !placed {
		t.Fatalf("expected source-proven dried ghast at %v", ghastPos)
	}
	for _, placement := range render(true) {
		for _, block := range placement.Blocks {
			if block.Pos == ghastPos && block.State.Name == "minecraft:dried_ghast" {
				t.Fatalf("dried ghast placed over non-air base terrain at %v", ghastPos)
			}
		}
	}
}
