package item_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestWaterBottlesTurnDirtIntoMud checks PotionItem.useOn: a water bottle,
// also a splash one, used on dirt from above or the side turns it into mud
// and leaves an empty bottle; other potions, other blocks and the bottom face
// do nothing.
func TestWaterBottlesTurnDirtIntoMud(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		cases := []struct {
			used   item.UsableOnBlock
			ground world.Block
			face   cube.Face
			mud    bool
		}{
			{item.Potion{Type: potion.Water()}, block.Dirt{}, cube.FaceUp, true},
			{item.SplashPotion{Type: potion.Water()}, block.Dirt{Coarse: true}, cube.FaceNorth, true},
			{item.Potion{Type: potion.Water()}, block.Dirt{}, cube.FaceDown, false},
			{item.Potion{Type: potion.Awkward()}, block.Dirt{}, cube.FaceUp, false},
			{item.Potion{Type: potion.Water()}, block.Stone{}, cube.FaceUp, false},
		}
		for i, c := range cases {
			tx.SetBlock(pos, c.ground, nil)
			ctx := &item.UseContext{}
			used := c.used.UseOnBlock(pos, c.face, mgl64.Vec3{}, tx, nil, ctx)
			_, mud := tx.Block(pos).(block.Mud)
			if used != c.mud || mud != c.mud {
				t.Fatalf("case %d: used %v, mud %v, want %v", i, used, mud, c.mud)
			}
			if c.mud {
				if _, bottle := ctx.NewItem.Item().(item.GlassBottle); !bottle || ctx.CountSub != 1 {
					t.Fatalf("case %d: left %v and took %d, want one bottle for the potion", i, ctx.NewItem, ctx.CountSub)
				}
			}
		}
	})
}

// TestJavaItemsResolveToBedrockItems checks Java item identifiers that name
// Bedrock items differently, and explorer maps, which Java keeps as their own
// items and Bedrock as filled maps with a variant.
func TestJavaItemsResolveToBedrockItems(t *testing.T) {
	for id, want := range map[string]string{
		"minecraft:item_frame":                 "minecraft:frame",
		"minecraft:glow_item_frame":            "minecraft:glow_frame",
		"minecraft:frogspawn":                  "minecraft:frog_spawn",
		"minecraft:zombified_piglin_spawn_egg": "minecraft:zombie_pigman_spawn_egg",
		"minecraft:knowledge_book":             "adamant:knowledge_book",
	} {
		it, ok := item.JavaItem(id)
		if !ok {
			t.Errorf("%s resolves to no item", id)
			continue
		}
		if name, _ := it.EncodeItem(); name != want {
			t.Errorf("%s resolves to %s, want %s", id, name, want)
		}
	}
	it, ok := item.JavaItem("minecraft:buried_treasure_map")
	if m, isMap := it.(item.FilledMap); !ok || !isMap || m.Variant.JavaItemID() != "minecraft:buried_treasure_map" {
		t.Fatalf("buried_treasure_map resolves to %#v", it)
	}
}

// TestChorusFruitHasAUseCooldown checks the use_cooldown component of chorus
// fruit.
func TestChorusFruitHasAUseCooldown(t *testing.T) {
	if cd := (item.ChorusFruit{}).Cooldown(); cd.Seconds() != 1 {
		t.Fatalf("chorus fruit cooldown %v, want 1s", cd)
	}
}
