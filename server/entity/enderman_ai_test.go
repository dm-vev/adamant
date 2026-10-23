package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestEndermanSavesCarriedBlockAsBDS checks the carried block is saved under
// carriedBlock as a block state compound, as BDS saves it, and read back.
func TestEndermanSavesCarriedBlockAsBDS(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	data := &world.EntityData{}
	EndermanType.DecodeNBT(map[string]any{"carriedBlock": map[string]any{"name": "minecraft:grass_block", "states": map[string]any{}}}, data)
	if _, ok := data.Data.(*speciesState).carried.(block.Grass); !ok {
		t.Fatalf("decoded carried block %#v, want grass", data.Data.(*speciesState).carried)
	}
	saved := EndermanType.EncodeNBT(data)
	carried, _ := saved["carriedBlock"].(map[string]any)
	if carried["name"] != "minecraft:grass_block" || carried["version"] == nil {
		t.Fatalf("saved carriedBlock %v", carried)
	}
	if j := ReadNativeMobJavaState(saved); j["CarriedBlock"] != nil {
		t.Fatalf("carried block also saved in the Java state: %v", j)
	}

	// Saves of earlier versions kept the block in the Java state.
	legacy := &world.EntityData{}
	EndermanType.DecodeNBT(MergeNativeMobJavaState(map[string]any{}, map[string]any{"CarriedBlock": map[string]any{"Name": "minecraft:sand", "Properties": map[string]any{}}}), legacy)
	if _, ok := legacy.Data.(*speciesState).carried.(block.Sand); !ok {
		t.Fatalf("decoded legacy carried block %#v, want sand", legacy.Data.(*speciesState).carried)
	}
}

// TestEndermanHandlesEveryHoldableBlock checks every block an enderman may
// take resolves to a default state and may be placed back without the Java
// shape rules failing.
func TestEndermanHandlesEveryHoldableBlock(t *testing.T) {
	names, err := gamedata.BlockTagMembers("minecraft:enderman_holdable")
	if err != nil || len(names) == 0 {
		t.Fatalf("holdable blocks %v, error %v", names, err)
	}
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 65, 0}
		tx.SetBlock(pos.Side(cube.FaceDown), block.Grass{}, nil)
		for _, name := range names {
			id, err := gamedata.BlockStateID(name, gamedata.BlockPropertiesWithDefaults(name, nil))
			if err != nil {
				t.Fatalf("%v: %v", name, err)
			}
			b, ok := world.BlockByRuntimeID(world.ResolveStateID(world.DefaultBlockRegistry, id).Block)
			if !ok {
				t.Fatalf("%v has no block", name)
			}
			if !blockInTag(defaultBlockState(b), "minecraft:enderman_holdable") {
				t.Fatalf("default state of %v left the tag", name)
			}
			tx.JavaPlacement(id, pos)
		}
	})
}

// TestEndermanLeavesBlocks checks an enderman puts the block it carries
// down beside it on solid ground.
func TestEndermanLeavesBlocks(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	h := NewEnderman(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}})
	<-w.Exec(func(tx *world.Tx) {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
		e := tx.AddEntity(h).(*Enderman)
		e.CarryBlock(block.Melon{})
		m := e.s().ai.Mob(e.Ent, e)
		g := &endermanLeaveBlockGoal{}
		for range 100 {
			g.Tick(m)
		}
		if _, ok := e.CarriedBlock(); ok {
			t.Fatal("enderman still carries its block")
		}
		placed := 0
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				if _, ok := tx.Block(cube.Pos{x, 65, z}).(block.Melon); ok {
					placed++
				}
			}
		}
		if placed != 1 {
			t.Fatalf("%d melons placed beside the enderman", placed)
		}
	})
}
