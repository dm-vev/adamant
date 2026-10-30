package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// TestItemComponents checks imported Java components with Bedrock's cake
// stack size, which the native container transfer fixtures exercise.
func TestItemComponents(t *testing.T) {
	for _, c := range []struct {
		id    string
		stack int
	}{{"minecraft:cake", 64}, {"minecraft:name_tag", 64}} {
		if got := javaStack(t, c.id, 1).MaxCount(); got != c.stack {
			t.Errorf("%v stacks to %v, want %v", c.id, got, c.stack)
		}
	}
	for _, c := range []struct {
		id         string
		durability int
	}{{"minecraft:bow", 384}, {"minecraft:crossbow", 465}, {"minecraft:fishing_rod", 64}, {"minecraft:flint_and_steel", 64}, {"minecraft:elytra", 432}, {"minecraft:turtle_helmet", 275}} {
		if got := javaStack(t, c.id, 1).Item().(item.Durable).DurabilityInfo().MaxDurability; got != c.durability {
			t.Errorf("%v has durability %v, want %v", c.id, got, c.durability)
		}
	}
	for _, id := range []string{"minecraft:netherite_ingot", "minecraft:netherite_scrap", "minecraft:netherite_block", "minecraft:ancient_debris"} {
		if f, ok := javaStack(t, id, 1).Item().(item.FireProof); !ok || !f.FireProof() {
			t.Errorf("%v burns", id)
		}
	}
	for _, c := range []struct {
		id     string
		chance int
		ok     bool
	}{{"minecraft:oak_sapling", 30, true}, {"minecraft:sugar_cane", 50, true}, {"minecraft:fern", 65, true}, {"minecraft:short_grass", 30, true}, {"minecraft:cake", 100, true}, {"minecraft:grass_block", 0, false}, {"minecraft:chorus_flower", 0, false}} {
		if got, ok := item.CompostChance(javaStack(t, c.id, 1).Item()); got != c.chance || ok != c.ok {
			t.Errorf("%v composts with %v%% (%v), want %v%% (%v)", c.id, got, ok, c.chance, c.ok)
		}
	}
}

// TestEmptyComposterAlwaysTakesALayer follows ContextIntProviders.compostable:
// an empty composter takes a layer from any compostable item.
func TestEmptyComposterAlwaysTakesALayer(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		for range 20 {
			c := Composter{}
			tx.SetBlock(pos, c, nil)
			if !c.fill(javaStack(t, "minecraft:oak_sapling", 1), pos, tx) {
				t.Fatal("composter refused a sapling")
			}
			if got := tx.Block(pos).(Composter).Level; got != 1 {
				t.Fatalf("empty composter level after a sapling = %v, want 1", got)
			}
		}
	})
}
