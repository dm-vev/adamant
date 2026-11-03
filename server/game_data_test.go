package server

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

func TestDefaultGameDataIncludesSharedFallDamageRule(t *testing.T) {
	settings := &world.Settings{DefaultGameMode: world.GameModeSurvival, FallDamage: true}
	provider := world.NopProvider{Set: settings}
	overworld := world.Config{Synchronous: true, Provider: provider}.New()
	nether := world.Config{Synchronous: true, Provider: provider, Dim: world.Nether}.New()
	defer overworld.Close()
	defer nether.Close()

	nether.SetFallDamage(false)
	dimensions := []v2193.DimensionDefinition{{Name: "adamant:test"}}
	srv := &Server{world: overworld, customDimensions: dimensions}
	data := srv.defaultGameData()

	found := false
	for _, rule := range data.GameRules {
		if rule.Name == "falldamage" {
			found = true
			if enabled, ok := rule.Value.(bool); !ok || enabled {
				t.Fatalf("falldamage value = %#v, want false", rule.Value)
			}
		}
	}
	if !found {
		t.Fatal("falldamage missing from initial game rules")
	}
	if len(data.Dimensions) != 1 || data.Dimensions[0].Name != dimensions[0].Name {
		t.Fatalf("dimensions = %#v, want %#v", data.Dimensions, dimensions)
	}
}

// TestDefaultGameDataDefinesBlocksOfData checks that the client learns the
// vanilla blocks the game defines by data from the server, as from BDS: it
// draws none of them otherwise, and the tents of abandoned camps, of white
// wool stairs, stood invisible over their fences.
func TestDefaultGameDataDefinesBlocksOfData(t *testing.T) {
	srv := newSessionTestServer(t, player.NopProvider{})
	srv.makeBlockEntries()
	blocks := map[string]map[string]any{}
	for _, b := range srv.defaultGameData().Blocks {
		blocks[b.Name] = b.Properties
	}
	for _, colour := range item.Colours() {
		for _, kind := range []string{"wool_stairs", "wool_slab", "wool_double_slab", "concrete_stairs", "concrete_slab", "concrete_double_slab"} {
			name := "minecraft:" + colour.String() + "_" + kind
			if _, ok := blocks[name]; !ok {
				t.Errorf("%s is not defined", name)
			}
		}
	}
	data, _ := blocks["minecraft:white_wool_stairs"]["vanilla_block_data"].(map[string]any)
	archetype, _ := data["block_archetype"].(map[string]any)
	if archetype["base_block"] != "minecraft:white_wool" || archetype["type"] != "stair_block" {
		t.Errorf("white wool stairs are built on %v, want stairs of white wool", archetype)
	}
}

// TestBlocksOfDataAreBlocksOfTheServer checks that the server has a block
// for each vanilla block it defines to the client.
func TestBlocksOfDataAreBlocksOfTheServer(t *testing.T) {
	if len(vanillaBlocks) == 0 {
		t.Fatal("no vanilla blocks are defined")
	}
	for name := range vanillaBlocks {
		if _, ok := world.DefaultBlockRegistry.StateToRuntimeID(name, nil); !ok {
			t.Errorf("%s is no block of the server", name)
		}
	}
}
