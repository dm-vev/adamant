package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMerchantTradeFollowsTheTradeScreen plays the requests the Bedrock
// trade screen sends: the player puts the price into the Trade2Ingredient1
// slot, then crafts the offer by its network ID, the offer index plus one,
// consumes the input and takes the created output.
func TestMerchantTradeFollowsTheTradeScreen(t *testing.T) {
	t.Parallel()
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		h, err := entity.NewFromStructureNBT(map[string]any{"id": "minecraft:villager",
			"VillagerData": map[string]any{"type": "minecraft:plains", "profession": "minecraft:farmer", "level": int32(1)}},
			world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}, SpawnFinalized: true})
		if err != nil {
			t.Fatal(err)
		}
		v := tx.AddEntity(h).(*entity.Villager)
		offers, err := v.MerchantOffers()
		if err != nil || len(offers) == 0 {
			t.Fatalf("farmer has no offers: %v", err)
		}
		offer := offers[0]

		s := craftingTestSession()
		handler := craftingTestHandler()
		user := merchantTestUser{handle: entity.NewItem(world.EntitySpawnOpts{}, item.NewStack(item.Emerald{}, 1))}
		player := batchController{tx: tx, s: s, owner: user}
		m, ok := menu.OpenMerchant(v, v, user.handle)
		if !ok {
			t.Fatal("the farmer does not trade")
		}
		s.menu.Store(m)
		s.openedContainerID.Store(uint32(mc.ContainerTypeTrade))

		price := offer.CostA()
		_ = s.inv.SetItem(0, price)
		place := &mc.PlaceStackRequestAction{}
		place.Count = byte(price.Count())
		place.Source = mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: mc.ContainerCombinedHotBarAndInventory}, Slot: 0, StackNetworkID: item_id(price)}
		place.Destination = mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: mc.ContainerTradeTwoIngredientOne}, Slot: menu.UITradeInputA}
		if err := handler.handleRequest(mc.ItemStackRequest{RequestID: -1, Actions: []mc.StackRequestAction{place}}, s, tx, player); err != nil {
			t.Fatalf("placing the price in the trade input: %v", err)
		}

		input, _ := s.ui.Item(menu.UITradeInputA)
		consume := &mc.ConsumeStackRequestAction{}
		consume.Count = byte(price.Count())
		consume.Source = mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: mc.ContainerTradeTwoIngredientOne}, Slot: menu.UITradeInputA, StackNetworkID: item_id(input)}
		take := &mc.PlaceStackRequestAction{}
		take.Count = byte(offer.Output.Count())
		take.Source = mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: mc.ContainerCreatedOutput}, Slot: menu.UIResult, StackNetworkID: -3}
		take.Destination = mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: mc.ContainerCombinedHotBarAndInventory}, Slot: 1}
		if err := handler.handleRequest(mc.ItemStackRequest{RequestID: -3, Actions: []mc.StackRequestAction{
			&mc.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
			consume,
			take,
		}}, s, tx, player); err != nil {
			t.Fatalf("trading: %v", err)
		}
		if left, _ := s.ui.Item(menu.UITradeInputA); !left.Empty() {
			t.Fatalf("trade input keeps %v", left)
		}
		if got, _ := s.inv.Item(1); !got.Comparable(offer.Output) || got.Count() != offer.Output.Count() {
			t.Fatalf("bought %v, want %v", got, offer.Output)
		}
		if after, _ := v.MerchantOffers(); after[0].Uses != offer.Uses+1 {
			t.Fatalf("offer uses %v, want %v", after[0].Uses, offer.Uses+1)
		}
	})
}

// merchantTestUser is a survival player as far as trading asks: the owner of
// the batches of its requests.
type merchantTestUser struct {
	craftOwner
	handle *world.EntityHandle
}

func (u merchantTestUser) H() *world.EntityHandle { return u.handle }

// TestMerchantOffersNameTheirNetworkIDs checks the offers the trade screen
// receives carry the netId its trade requests name and the levels' experience.
func TestMerchantOffersNameTheirNetworkIDs(t *testing.T) {
	t.Parallel()
	data, err := serializeMerchantOffers([]entity.MerchantOffer{{Input: item.NewStack(item.Emerald{}, 1), Output: item.NewStack(item.Bread{}, 6), MaxUses: 16}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := nbt.UnmarshalEncoding(data, &decoded, nbt.NetworkLittleEndian); err != nil {
		t.Fatal(err)
	}
	recipes, _ := decoded["Recipes"].([]any)
	if len(recipes) != 1 || recipes[0].(map[string]any)["netId"] != int32(1) || len(decoded["TierExpRequirements"].([]any)) != 5 {
		t.Fatalf("serialised offers %v lack netId or TierExpRequirements", decoded)
	}
}
