package session_test

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// merchantRequestsUUID is the UUID of the villager of the merchant scenario.
var merchantRequestsUUID = uuid.MustParse("00000000-0000-4000-8000-000000000f13")

// The offers of the villager of the merchant scenario, by index.
const (
	// offerBread sells six bread for an emerald, priced up to two by its
	// demand.
	offerBread = iota
	// offerSword sells an iron sword for an emerald and a stick.
	offerSword
	// offerUsedUp sells apples, but is used up.
	offerUsedUp
	// offerLastUse sells a cookie for an emerald, once more.
	offerLastUse
)

// merchantRequestsScenario trades with a villager as TestMenuRequestScenarios
// runs it: through the inputs of the trade screen, once, twice at once
// and with a second cost, and by taking the result of an offer, which pays
// from the inventory, into an empty slot, onto bread, into the off hand,
// paid from two slots, and into the slot of the stick that pays for it; and
// the trades the villager refuses, some after actions of the same request
// that the refusal undoes: an offer out of range, used up, or used up by
// the second of two trades, which keeps the first use; inputs that do not
// pay, a locked input, results that do not stack, a result of the wrong
// count, a destination it does not fit, out of range or of no container;
// and the villager gone under the open menu. The price of bread falls as
// the player trades: it is paid by the scan of the inventory as priced.
func (s *scenario) merchantRequestsScenario() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	inputA := func() v2193.StackRequestSlotInfo {
		return s.slot(v2193.ContainerTradeTwoIngredientOne, uiTradeInput)
	}
	inputB := func() v2193.StackRequestSlotInfo {
		return s.slot(v2193.ContainerTradeTwoIngredientTwo, uiTradeInput+1)
	}
	trade := func(heading string, offer, times, count, to int, accept bool) {
		s.t.Helper()
		actions := []v2193.StackRequestAction{
			&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: uint32(offer + 1), NumberOfCrafts: uint8(times)},
			&v2193.ConsumeStackRequestAction{Count: 1, Source: inputA()},
			&v2193.PlaceStackRequestAction{Count: uint8(count), Source: s.created(), Destination: s.slot(inv, to)},
		}
		if accept {
			s.request(heading, nil, actions...)
		} else {
			s.refuse(heading, actions...)
		}
	}
	preview := func(offer int) v2193.StackRequestSlotInfo {
		return v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: v2193.ContainerTradeResultPreview}, Slot: uint8(offer)}
	}
	take := func(heading string, offer, count int, to v2193.StackRequestSlotInfo, accept bool) {
		s.t.Helper()
		action := &v2193.TakeStackRequestAction{Count: uint8(count), Source: preview(offer), Destination: to}
		if accept {
			s.request(heading, nil, action)
		} else {
			s.refuse(heading, action)
		}
	}
	offer := func(buy, buyB, sell item.Stack, uses, maxUses, demand int) map[string]any {
		o := map[string]any{
			"buy":  item.WriteNBT(buy),
			"sell": item.WriteNBT(sell),
			// No experience: the villager neither rewards orbs, which fly
			// off at random, nor levels up to offers drawn at random.
			"uses": int32(uses), "maxUses": int32(maxUses), "xp": int32(0), "rewardExp": byte(0),
			"demand": int32(demand), "priceMultiplier": float32(0.05),
		}
		if !buyB.Empty() {
			o["buyB"] = item.WriteNBT(buyB)
		}
		return o
	}
	emeralds := func(n int) item.Stack { return item.NewStack(item.Emerald{}, n) }

	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})
	s.give("emeralds, sticks, bread and apples given",
		slotStack{0, emeralds(30)},
		slotStack{1, item.NewStack(item.Stick{}, 4)},
		slotStack{2, item.NewStack(item.Bread{}, 60)},
		slotStack{3, item.NewStack(item.Apple{}, 4)},
		slotStack{4, emeralds(1)},
	)
	var villager *world.EntityHandle
	s.step("a villager stands by", func(tx *world.Tx, p *player.Player) {
		h, err := entity.NewFromStructureNBT(map[string]any{
			"id":           "minecraft:villager",
			"NoAI":         byte(1),
			"VillagerData": map[string]any{"type": "minecraft:plains", "profession": "minecraft:farmer", "level": int32(1)},
			"Offers": map[string]any{"Recipes": []any{
				offerBread:   offer(emeralds(1), item.Stack{}, item.NewStack(item.Bread{}, 6), 0, 16, 20),
				offerSword:   offer(emeralds(1), item.NewStack(item.Stick{}, 1), item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), 0, 16, 0),
				offerUsedUp:  offer(emeralds(1), item.Stack{}, item.NewStack(item.Apple{}, 1), 16, 16, 0),
				offerLastUse: offer(emeralds(1), item.Stack{}, item.NewStack(item.Cookie{}, 1), 3, 4, 0),
			}},
		}, world.EntitySpawnOpts{Position: p.Position().Add(mgl64.Vec3{0, 0, -3}), ID: merchantRequestsUUID, SpawnFinalized: true})
		if err != nil {
			s.t.Fatal(err)
		}
		villager = h
		tx.AddEntity(h)
	})
	openTrades := func(heading string) {
		s.client(heading, &v2193.InventoryTransactionPacket{TransactionData: &v2193.UseItemOnEntityTransactionData{
			TargetEntityRuntimeID: s.runtimeID(villager),
			ActionType:            v2193.UseItemOnEntityActionInteract,
			HotBarSlot:            emptyHand,
			Position:              mgl32.Vec3{0.5, -58.38, 0.5},
			ClickedPosition:       mgl32.Vec3{0, 1, 0},
		}})
	}
	openTrades("the client opens the trade screen")

	// The trade screen: the inputs put in pay.
	s.request("nine emeralds put in", nil,
		&v2193.PlaceStackRequestAction{Count: 9, Source: s.slot(inv, 0), Destination: inputA()})
	trade("bread bought for two emeralds, its demand", offerBread, 1, 6, 10, true)
	trade("bread bought twice at once", offerBread, 2, 12, 11, true)
	trade("refused: an offer out of range", 9, 1, 1, 12, false)
	trade("refused: an offer of network ID 0", -1, 1, 1, 12, false)
	trade("refused: the used-up offer", offerUsedUp, 1, 1, 12, false)
	trade("refused: a sword without its stick", offerSword, 1, 1, 12, false)
	s.request("a stick put in", nil,
		&v2193.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 1), Destination: inputB()})
	trade("refused: two swords, which do not stack", offerSword, 2, 1, 12, false)
	trade("a sword bought for an emerald and a stick", offerSword, 1, 1, 12, true)
	// One of the two emeralds left taken out first: one does not pay.
	s.refuse("refused: an emerald taken out, then bread",
		&v2193.TakeStackRequestAction{Count: 1, Source: inputA(), Destination: s.slot(inv, 0)},
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: offerBread + 1, NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.again(v2193.ContainerTradeTwoIngredientOne, uiTradeInput)})
	s.request("two emeralds more put in", nil,
		&v2193.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 0), Destination: inputA()})
	// The first of the two trades uses the offer up: the second fails, and
	// the use of the first stays.
	trade("refused: two cookies of the last use", offerLastUse, 2, 2, 13, false)
	trade("refused: a cookie of the offer used up", offerLastUse, 1, 1, 13, false)
	s.step("a locked emerald in the input", func(_ *world.Tx, _ *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		_ = ui.SetItem(uiTradeInput, emeralds(3).WithLockMode(item.LockInInventory))
	})
	trade("refused: a sword for a locked emerald", offerSword, 1, 1, 14, false)
	s.step("the locked emeralds taken away", func(_ *world.Tx, _ *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		_ = ui.SetItem(uiTradeInput, item.Stack{})
	})

	// The preview: the inventory pays.
	take("bread taken from the preview into an empty slot", offerBread, 6, s.slot(inv, 15), true)
	take("bread taken from the preview onto bread", offerBread, 6, s.slot(inv, 15), true)
	take("bread taken from the preview into the off hand", offerBread, 6, s.slot(v2193.ContainerOffhand, 1), true)
	take("refused: bread of the wrong count", offerBread, 5, s.slot(inv, 16), false)
	take("refused: the used-up offer from the preview", offerUsedUp, 1, s.slot(inv, 16), false)
	take("refused: an offer out of range from the preview", 9, 1, s.slot(inv, 16), false)
	take("refused: bread onto apples", offerBread, 6, s.slot(inv, 3), false)
	take("refused: bread onto 60 bread", offerBread, 6, s.slot(inv, 2), false)
	take("refused: bread into a slot out of range", offerBread, 6, s.slot(inv, 40), false)
	take("refused: bread into no container", offerBread, 6, s.slot(v2193.ContainerAnvilInput, 1), false)
	// Bread paid with the emerald of slot 0 and that of slot 4.
	s.step("emeralds taken but one", func(_ *world.Tx, p *player.Player) {
		_ = p.Inventory().SetItem(0, emeralds(1))
	})
	take("bread taken from the preview, paid from two slots", offerBread, 6, s.slot(inv, 17), true)
	// The stick that pays leaves its slot empty for the sword.
	s.step("an emerald and one stick left", func(_ *world.Tx, p *player.Player) {
		_ = p.Inventory().SetItem(0, emeralds(1))
		_ = p.Inventory().SetItem(1, item.NewStack(item.Stick{}, 1))
	})
	take("a sword taken from the preview into the slot of the stick paid", offerSword, 1, s.slot(inv, 1), true)
	take("refused: a sword with no emerald left", offerSword, 1, s.slot(inv, 18), false)
	// The apples put away first: the refusal puts them back.
	s.refuse("refused: the apples moved, then a sword with no emerald",
		&v2193.PlaceStackRequestAction{Count: 4, Source: s.slot(inv, 3), Destination: s.slot(inv, 19)},
		&v2193.TakeStackRequestAction{Count: 1, Source: preview(offerSword), Destination: s.slot(inv, 18)})
	s.step("a locked emerald given", func(_ *world.Tx, p *player.Player) {
		_ = p.Inventory().SetItem(5, emeralds(2).WithLockMode(item.LockInSlot))
	})
	take("refused: bread paid with a locked emerald", offerBread, 6, s.slot(inv, 18), false)
	s.step("emeralds given", func(_ *world.Tx, p *player.Player) {
		_ = p.Inventory().SetItem(5, emeralds(8))
	})

	// The villager gone under the open menu.
	s.request("emeralds put in again", nil,
		&v2193.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 5), Destination: inputA()})
	s.step("the villager taken away", func(tx *world.Tx, _ *player.Player) {
		if e, ok := villager.Entity(tx); ok {
			tx.RemoveEntity(e)
		}
	})
	trade("refused: bread of a villager gone", offerBread, 1, 6, 20, false)
	take("refused: bread from the preview of a villager gone", offerBread, 6, s.slot(inv, 20), false)
	s.closeContainer("the client closes the trade screen")
	trade("refused: bread with no trade screen", offerBread, 1, 6, 20, false)
}
