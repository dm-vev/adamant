package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSpawnedMobsTakeTheirBiomeVariant follows the finalizeSpawn variant
// selection of Java 26.
func TestSpawnedMobsTakeTheirBiomeVariant(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()
	spawn := func(tx *world.Tx, id, biomeName string, x float64) world.Entity {
		t.Helper()
		b, ok := world.BiomeByName(biomeName)
		if !ok {
			t.Fatalf("unknown biome %s", biomeName)
		}
		pos := mgl64.Vec3{x, 100, 0.5}
		tx.SetBiome(cube.PosFromVec3(pos), b)
		h := spawnMob(id, world.EntitySpawnOpts{Position: pos, SpawnReason: "natural", SpawnSeed: 1})
		return tx.AddEntity(h)
	}
	<-w.Exec(func(tx *world.Tx) {
		if v := spawn(tx, "minecraft:pig", "desert", 0.5).(*Pig).behaviour().ClimateVariant(); v != "warm" {
			t.Errorf("desert pig is %s, want warm", v)
		}
		if v := spawn(tx, "minecraft:cow", "ice_plains", 2.5).(*Cow).behaviour().ClimateVariant(); v != "cold" {
			t.Errorf("snowy cow is %s, want cold", v)
		}
		if v := spawn(tx, "minecraft:chicken", "ice_plains", 4.5).(*Chicken).ClimateVariant(); v != "cold" {
			t.Errorf("snowy chicken is %s, want cold", v)
		}
		if v := spawn(tx, "minecraft:frog", "mangrove_swamp", 6.5).(*Frog).Variant(); v != 2 {
			t.Errorf("mangrove frog has Bedrock variant %d, want warm 2", v)
		}
		if v := spawn(tx, "minecraft:frog", "ice_plains", 8.5).(*Frog).Variant(); v != 1 {
			t.Errorf("snowy frog has Bedrock variant %d, want cold 1", v)
		}
		if v := spawn(tx, "minecraft:wolf", "forest", 10.5).(*Wolf).Variant(); wolfVariantNames[v] != "woods" {
			t.Errorf("forest wolf is %s, want woods", wolfVariantNames[v])
		}
		if v := spawn(tx, "minecraft:wolf", "mesa_plateau_stone", 12.5).(*Wolf).Variant(); wolfVariantNames[v] != "striped" {
			t.Errorf("badlands wolf is %s, want striped", wolfVariantNames[v])
		}
		if v := spawn(tx, "minecraft:rabbit", "desert", 14.5).(*Rabbit).Variant(); v != 4 {
			t.Errorf("desert rabbit has variant %d, want gold 4", v)
		}
	})
}
