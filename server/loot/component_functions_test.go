package loot

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
)

// TestPoolFunctionsApplyToTheirItems checks a pool's functions decorate the
// items its entries yield, as LootPool does: buried treasure's potion is
// one of water breathing, and pillager outposts' goat horns play one of the
// regular goat horn instruments, not always the first.
func TestPoolFunctionsApplyToTheirItems(t *testing.T) {
	potions := 0
	for seed := int64(1); seed <= 20; seed++ {
		stacks, err := Generate("minecraft:chests/buried_treasure", seed, Context{})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range stacks {
			if p, ok := s.Item().(item.Potion); ok {
				potions++
				if p.Type != potion.WaterBreathing() {
					t.Fatalf("buried treasure potion is %v, want water breathing", p.Type)
				}
			}
		}
	}
	if potions == 0 {
		t.Fatal("no buried treasure held a potion")
	}

	instruments := map[uint8]bool{}
	for seed := int64(1); seed <= 100; seed++ {
		stacks, err := Generate("minecraft:chests/pillager_outpost", seed, Context{})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range stacks {
			if horn, ok := s.Item().(item.GoatHorn); ok {
				if horn.Type.Uint8() > 3 {
					t.Fatalf("outpost goat horn plays %v, not a regular goat horn", horn.Type.Name())
				}
				instruments[horn.Type.Uint8()] = true
			}
		}
	}
	if len(instruments) < 2 {
		t.Fatalf("outpost goat horns all play the same instrument: %v", instruments)
	}
}

// TestCopyStateCopiesBlockProperties checks copy_state puts the listed
// properties of the broken block into the item's block_state component, as
// a bee nest keeps its honey level.
func TestCopyStateCopiesBlockProperties(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	nest, ok := world.BlockByName("minecraft:bee_nest", map[string]any{"direction": int32(0), "honey_level": int32(5)})
	if !ok {
		t.Fatal("no bee nest state")
	}
	stack, err := makeStack(entry{Type: "minecraft:item", Name: "minecraft:bee_nest", Modifier: modifierList{{
		Type: "minecraft:copy_state", Raw: map[string]any{"block": "minecraft:bee_nest", "properties": []any{"honey_level"}},
	}}}, &Context{BlockState: nest})
	if err != nil {
		t.Fatal(err)
	}
	data, err := item.StackToJavaNBT(stack)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := data["components"].(map[string]any)["minecraft:block_state"].(map[string]any)
	if state["honey_level"] != "5" {
		t.Fatalf("bee nest item block_state = %v, want honey_level 5", state)
	}
}
