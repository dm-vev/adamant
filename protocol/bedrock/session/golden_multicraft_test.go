package session_test

import (
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/google/uuid"
)

// TestGoldenMultiCraft records what a client is sent for a multi-recipe
// craft: two damaged wooden swords repaired into one in the grid of a
// crafting table. Each write of the UI inventory sends all of it, so the
// order the two inputs are consumed in shows; the client names the second
// first.
func TestGoldenMultiCraft(t *testing.T) {
	if isolated(t) {
		return
	}
	s := newScenario(t, "multicraft", scenarioConfig{})
	s.multiCraftScenario()
	s.snapshot()
}

// multiCraftScenario runs the steps of TestGoldenMultiCraft.
func (s *scenario) multiCraftScenario() {
	const inv = mc.ContainerCombinedHotBarAndInventory

	sword := item.Sword{Tier: item.ToolTierWood}
	s.step("two damaged swords given", func(_ *world.Tx, p *player.Player) {
		_ = p.Inventory().SetItem(0, item.NewStack(sword, 1).Damage(40))
		_ = p.Inventory().SetItem(1, item.NewStack(sword, 1).Damage(40))
	})
	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})
	table := cube.Pos{2, -60, 0}
	s.place("a crafting table is placed", table, block.CraftingTable{})
	s.clickBlock("the client opens the crafting table", table)
	s.request("the swords put in the grid", nil,
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(mc.ContainerCraftingInput, uiCraftingGrid)},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 1), Destination: s.slot(mc.ContainerCraftingInput, uiCraftingGrid+1)})

	// Each sword has 19 of 59 durability left; the repair adds a twentieth.
	max := item.NewStack(sword, 1).MaxDurability()
	result := item.NewStack(sword, 1).WithDurability(min(max, 19+19+max/20))
	name, meta := result.Item().EncodeItem()
	s.request("the swords repaired", nil,
		&mc.CraftRecipeStackRequestAction{RecipeNetworkID: s.multiRecipe("00000000-0000-0000-0000-000000000001"), NumberOfCrafts: 1},
		&mc.CraftResultsDeprecatedStackRequestAction{TimesCrafted: 1, ResultItems: []mc.StackRequestItem{{
			Identifier: name, MetadataValue: uint32(meta), Count: 1, NBTData: session.NetworkItemNBT(result),
		}}},
		&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerCraftingInput, uiCraftingGrid+1)},
		&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerCraftingInput, uiCraftingGrid)})
	// A multi recipe takes no other action: its result is taken after.
	s.request("the repaired sword taken", nil,
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(mc.ContainerCreatedOutput, uiCreatedOutput), Destination: s.slot(inv, 2)})
	s.closeContainer("the client closes the crafting table")
}

// multiRecipe returns the network ID of the multi recipe of the UUID passed
// that the client was sent.
func (s *scenario) multiRecipe(id string) uint32 {
	s.t.Helper()
	want := uuid.MustParse(id)
	for _, r := range s.crafting.MultiRecipes {
		if r.UUID == [16]byte(want) {
			return r.RecipeNetworkID
		}
	}
	s.t.Fatalf("no multi recipe %s", id)
	return 0
}
