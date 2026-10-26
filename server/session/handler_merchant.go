package session

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

const (
	// merchantInputSlotA and merchantInputSlotB are the UI inventory slots of
	// the trade screen's two inputs, Trade2Ingredient1 and Trade2Ingredient2.
	merchantInputSlotA = 4
	merchantInputSlotB = 5
)

// merchantTradeOpen reports if the session has a trade screen open.
func (s *Session) merchantTradeOpen() bool {
	return s.containerOpened.Load() && byte(s.openedContainerID.Load()) == protocol.ContainerTypeTrade
}

// merchantCostA returns the first input of an offer with its price applied,
// as MerchantOffer.getCostA: the base count, plus the demand times the price
// multiplier rounded down if positive, plus the special price, kept between
// 1 and the item's stack size.
func merchantCostA(offer entity.MerchantOffer) item.Stack {
	base := offer.Input.Count()
	demand := int(math.Floor(float64(float32(int32(base)*int32(offer.Demand)) * offer.PriceMultiplier)))
	price := min(max(base+max(0, demand)+offer.SpecialPrice, 1), offer.Input.MaxCount())
	return offer.Input.Grow(price - base)
}

// openedMerchant returns the merchant the session's trade screen is open for.
func (s *Session) openedMerchant(tx *world.Tx) (world.Entity, entity.Merchant, error) {
	if !s.merchantTradeOpen() {
		return nil, nil, fmt.Errorf("no merchant window is open")
	}
	handle := s.openedEntity.Load()
	if handle == nil {
		return nil, nil, fmt.Errorf("merchant window has no entity")
	}
	target, ok := handle.Entity(tx)
	if !ok {
		return nil, nil, fmt.Errorf("merchant is no longer in the world")
	}
	merchant, ok := target.(entity.Merchant)
	if !ok {
		return nil, nil, fmt.Errorf("opened entity is not a merchant")
	}
	return target, merchant, nil
}

// handleMerchantCraft handles the trade request the Bedrock trade screen
// sends: a CraftRecipe action naming the offer by its network ID, which is
// its index plus one, followed by Consume actions for the inputs the player
// put in the two input slots and the taking of the created output. The
// inputs must satisfy the offer as MerchantOffer.satisfiedBy requires, the
// first input slot paying the priced first cost and the second the second.
func (h *ItemStackRequestHandler) handleMerchantCraft(a *protocol.CraftRecipeStackRequestAction, s *Session, tx *world.Tx, c Controllable) error {
	target, merchant, err := s.openedMerchant(tx)
	if err != nil {
		return err
	}
	times := max(int(a.NumberOfCrafts), 1)
	index := int(a.RecipeNetworkID) - 1

	inputA, _ := s.ui.Item(merchantInputSlotA)
	inputB, _ := s.ui.Item(merchantInputSlotB)
	var output item.Stack
	for range times {
		offers, err := merchantOffersFor(merchant, c)
		if err != nil {
			s.notifyMerchantError(c, "failed to load merchant offers for trade", err)
			return err
		}
		if index < 0 || index >= len(offers) {
			return fmt.Errorf("merchant offer %d is out of range", index)
		}
		offer := offers[index]
		if offer.Uses >= offer.MaxUses || offer.Input.Empty() || offer.Output.Empty() {
			return fmt.Errorf("merchant offer %d is unavailable", index)
		}
		costA, costB := merchantCostA(offer), offer.SecondaryInput
		if !merchantPays(inputA, costA) || !merchantPays(inputB, costB) {
			return fmt.Errorf("merchant offer %d: inputs %v and %v do not pay %v and %v", index, inputA, inputB, costA, costB)
		}
		if !output.Empty() && (!output.Comparable(offer.Output) || output.Count()+offer.Output.Count() > output.MaxCount()) {
			return fmt.Errorf("merchant offer %d: repeated results do not stack", index)
		}
		if err := ensureUnlockedForCrafting(inputA); err != nil {
			return err
		}
		completed, err := merchant.CompleteMerchantTrade(tx, c, index)
		if err != nil {
			s.notifyMerchantError(c, "failed to complete merchant trade", err)
			return err
		}
		if !completed {
			return fmt.Errorf("merchant rejected offer %d", index)
		}
		inputA = inputA.Grow(-costA.Count())
		if !costB.Empty() {
			inputB = inputB.Grow(-costB.Count())
		}
		if output.Empty() {
			output = offer.Output
		} else {
			output = output.Grow(offer.Output.Count())
		}
	}
	for slot, stack := range map[byte]item.Stack{merchantInputSlotA: inputA, merchantInputSlotB: inputB} {
		if err := h.setItemInSlot(protocol.StackRequestSlotInfo{
			Container: protocol.FullContainerName{ContainerID: protocol.ContainerTradeTwoIngredientOne},
			Slot:      slot,
		}, stack, s, tx); err != nil {
			return err
		}
	}
	if updated, err := merchantOffersFor(merchant, c); err == nil {
		if serialized, err := serializeMerchantOffers(updated); err == nil {
			s.sendMerchantOffers(target, merchant, c, updated, serialized)
		}
	}
	return h.createResults(s, tx, output)
}

// merchantPays reports if the stack in an input slot pays a cost: an empty
// cost needs nothing, otherwise the same item at least as many times.
func merchantPays(input, cost item.Stack) bool {
	if cost.Empty() {
		return true
	}
	return !input.Empty() && input.Comparable(cost) && input.Count() >= cost.Count()
}
