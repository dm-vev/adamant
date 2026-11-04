package menu

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
)

// Trade makes offer index of the merchant of the menu times times, paid with
// the two trade inputs, and shows the client the offers as they are after it.
//
// Each trade needs the offer to have uses left and the inputs to pay the
// priced first cost and the second cost, the first input slot paying the
// first; the results of repeated trades must stack. The first input may not
// be locked.
func (t *Tx) Trade(index, times int) error {
	tx, owner := t.o.Tx, t.o.Owner
	_, merchant, err := t.o.Menu.Merchant(tx)
	if err != nil {
		return err
	}

	inputA, _ := t.o.Inv.UI.Item(UITradeInputA)
	inputB, _ := t.o.Inv.UI.Item(UITradeInputB)
	var output item.Stack
	for range times {
		offers, err := merchant.MerchantOffersFor(owner)
		if err != nil {
			t.notifyMerchantError("failed to load merchant offers for trade", err)
			return err
		}
		if index < 0 || index >= len(offers) {
			return fmt.Errorf("merchant offer %d is out of range", index)
		}
		offer := offers[index]
		if offer.Uses >= offer.MaxUses || offer.Input.Empty() || offer.Output.Empty() {
			return fmt.Errorf("merchant offer %d is unavailable", index)
		}
		costA, costB := offer.CostA(), offer.SecondaryInput
		if !merchantPays(inputA, costA) || !merchantPays(inputB, costB) {
			return fmt.Errorf("merchant offer %d: inputs %v and %v do not pay %v and %v", index, inputA, inputB, costA, costB)
		}
		if !output.Empty() && (!output.Comparable(offer.Output) || output.Count()+offer.Output.Count() > output.MaxCount()) {
			return fmt.Errorf("merchant offer %d: repeated results do not stack", index)
		}
		if err := ensureUnlockedForCrafting(inputA); err != nil {
			return err
		}
		completed, err := merchant.CompleteMerchantTrade(tx, owner, index)
		if err != nil {
			t.notifyMerchantError("failed to complete merchant trade", err)
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
	for _, in := range [...]struct {
		slot  int
		stack item.Stack
	}{{UITradeInputA, inputA}, {UITradeInputB, inputB}} {
		if err := t.set(t.named(Slot{Inv: t.o.Inv.UI, Index: in.slot}, RoleTradeInput), in.stack); err != nil {
			return err
		}
	}
	if updated, err := merchant.MerchantOffersFor(owner); err == nil {
		t.o.Viewer.ViewTrades(t.o.Menu, merchant, updated)
	}
	return t.createResults(output)
}

// TakeTrade makes offer index once, paid from the main inventory, and puts
// count items of its result in to.
//
// count must be the count of the result of the offer, which must have uses
// left, and the result must fit in to and be taken there
// (ErrSlotRefusesItem); both are checked before anything is paid. The items
// paid may not be locked, and the HandleTake handler of the main inventory
// for each slot paid and the HandlePlace handler of the inventory of to may
// cancel the trade. A failure to load the offers after the trade closes the
// menu.
func (t *Tx) TakeTrade(index, count int, to Ref) error {
	tx, c := t.o.Tx, t.o.Owner
	_, merchant, err := t.o.Menu.Merchant(tx)
	if err != nil {
		return err
	}
	offers, err := merchant.MerchantOffersFor(c)
	if err != nil {
		t.notifyMerchantError("failed to load merchant offers for trade", err)
		return err
	}
	if index < 0 || index >= len(offers) {
		return fmt.Errorf("merchant offer %d is out of range", index)
	}
	offer := offers[index]
	if offer.Uses >= offer.MaxUses || offer.Input.Empty() || offer.Output.Empty() || count != offer.Output.Count() {
		return fmt.Errorf("merchant offer %d is unavailable", index)
	}

	main := t.o.Inv.Main
	originals := main.Slots()
	remaining := append([]item.Stack(nil), originals...)
	inputA := offer.CostA()
	for _, cost := range [2]item.Stack{inputA, offer.SecondaryInput} {
		if cost.Empty() {
			continue
		}
		needed := cost.Count()
		for slot, stack := range remaining {
			if stack.Empty() || !stack.Comparable(cost) {
				continue
			}
			removed := min(needed, stack.Count())
			remaining[slot] = stack.Grow(-removed)
			needed -= removed
			if needed == 0 {
				break
			}
		}
		if needed != 0 {
			return fmt.Errorf("merchant trade inputs are missing")
		}
	}

	destInventory := to.Inv
	destination, err := t.Item(to.Slot)
	if err != nil {
		return err
	}
	if destInventory == main {
		slot := to.Index
		if slot < 0 || slot >= len(remaining) {
			return fmt.Errorf("trade destination slot is out of range")
		}
		destination = remaining[slot]
	}
	if !destination.Empty() && (!destination.Comparable(offer.Output) || destination.Count()+offer.Output.Count() > destination.MaxCount()) {
		return fmt.Errorf("merchant result does not fit in destination")
	}
	result := offer.Output
	if !destination.Empty() {
		result = destination.Grow(offer.Output.Count())
	}
	if err := ensureAccepts(to, result); err != nil {
		return err
	}

	context := event.C(inventory.Holder(c))
	for slot, after := range remaining {
		before := originals[slot]
		if before.Equal(after) {
			continue
		}
		taken := before.Grow(-after.Count())
		if err := ensureUnlockedForCrafting(taken); err != nil {
			return err
		}
		if err := call(context, slot, taken, main.Handler().HandleTake); err != nil {
			return err
		}
	}
	if err := call(context, to.Index, offer.Output, destInventory.Handler().HandlePlace); err != nil {
		return err
	}
	completed, err := merchant.CompleteMerchantTrade(tx, c, index)
	if err != nil {
		t.notifyMerchantError("failed to complete merchant trade", err)
		return err
	}
	if !completed {
		return fmt.Errorf("merchant rejected offer %d", index)
	}
	for slot, after := range remaining {
		if originals[slot].Equal(after) {
			continue
		}
		if err := t.set(t.named(Slot{Inv: main, Index: slot}, RoleInventory), after); err != nil {
			return err
		}
	}
	if err := t.set(to, result); err != nil {
		return err
	}
	updatedOffers, err := merchant.MerchantOffersFor(c)
	if err != nil {
		t.notifyMerchantError("failed to reload merchant offers", err)
		c.CloseMenu(false)
		return nil
	}
	t.o.Viewer.ViewTrades(t.o.Menu, merchant, updatedOffers)
	return nil
}

// merchantPays reports if the stack in an input slot pays a cost: an empty
// cost needs nothing, otherwise the same item at least as many times.
func merchantPays(input, cost item.Stack) bool {
	if cost.Empty() {
		return true
	}
	return !input.Empty() && input.Comparable(cost) && input.Count() >= cost.Count()
}

// notifyMerchantError logs an error of the merchant of a trade and tells the
// player that its offers are unavailable.
func (t *Tx) notifyMerchantError(message string, err error) {
	if t.o.Log != nil {
		t.o.Log.Error(message, "err", err)
	}
	if t.o.Owner != nil {
		t.o.Owner.Message("Merchant offers are temporarily unavailable.")
	}
}
