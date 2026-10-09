package nether

import (
	"os"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func init() {
	world.DefaultBlockRegistry.Finalize()
}

func TestComplexStartsMatchJava(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_complex_starts.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := compareNetherStartOracle(data); err != nil {
		t.Fatal(err)
	}
}

func TestFortressActorsSurviveRepeatedRendering(t *testing.T) {
	g := NewNether(1)
	// Java start (-14,-54): two MonsterThrones and one chest-bearing corridor.
	expected := []struct {
		pos  cube.Pos
		name string
	}{
		{cube.Pos{-160, 56, -853}, "minecraft:spawner"},
		{cube.Pos{-193, 74, -780}, "minecraft:spawner"},
		{cube.Pos{-207, 65, -796}, "minecraft:chest"},
	}
	first := make([]map[string]any, len(expected))
	for pass := 0; pass < 2; pass++ {
		for n := range expected {
			index := n
			if pass != 0 {
				index = len(expected) - 1 - n
			}
			want := expected[index]
			pos := world.ChunkPos{int32(want.pos[0] >> 4), int32(want.pos[2] >> 4)}
			column := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
			random := g.featureSource.structureStepRandom(pos, 1, 7)
			g.applyNetherBridge(int(pos[0]), int(pos[1]), column, random)
			state, err := world.JavaState(column.BlockRegistry(), world.NativeStateAt(column, uint8(want.pos[0]&15), int16(want.pos[1]), uint8(want.pos[2]&15)))
			if err != nil {
				t.Fatal(err)
			}
			if state.Name != want.name {
				t.Fatalf("pass=%d position=%v block=%s, want %s", pass, want.pos, state.Name, want.name)
			}
			var data map[string]any
			for _, actor := range g.takeBlockEntityPayloads(pos) {
				if actor.Pos == want.pos {
					data = actor.Data
				}
			}
			if want.name == "minecraft:chest" {
				if data["LootTable"] != "minecraft:chests/nether_bridge" {
					t.Fatalf("pass=%d missing fortress chest loot: %v", pass, data)
				}
			} else {
				spawn, _ := data["SpawnData"].(map[string]any)
				entity, _ := spawn["entity"].(map[string]any)
				if entity["id"] != "minecraft:blaze" {
					t.Fatalf("pass=%d missing blaze spawner: %v", pass, data)
				}
			}
			if pass == 0 {
				first[index] = data
			} else if !reflect.DeepEqual(data, first[index]) {
				t.Fatalf("re-rendering changed actor at %v: first=%v next=%v", want.pos, first[index], data)
			}
		}
	}
}
