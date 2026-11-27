package session

import (
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/world"
)

// handleAction dispatches an item stack request action directly by concrete
// pointer type without per-request context allocation or reflection lookup.
//
//go:generate go run ./internal/gentables -kind stack
func (h *ItemStackRequestHandler) handleAction(action mc.StackRequestAction, req mc.ItemStackRequest, s *Session, tx *world.Tx, c Controllable) (error, bool) {
	switch a := action.(type) {
	case *mc.TakeStackRequestAction:
		return h.handleTake(a, s, tx), true
	case *mc.PlaceStackRequestAction:
		return h.handlePlace(a, s, tx), true
	case *mc.SwapStackRequestAction:
		return h.handleSwap(a, s, tx), true
	case *mc.DestroyStackRequestAction:
		return h.handleDestroy(a, s, tx, c), true
	case *mc.DropStackRequestAction:
		return h.handleDrop(a, s, tx), true
	case *mc.BeaconPaymentStackRequestAction:
		return h.handleBeaconPayment(a), true
	case *mc.CraftRecipeStackRequestAction:
		return h.handleCraftRecipe(a, s, tx), true
	case *mc.AutoCraftRecipeStackRequestAction:
		return h.handleAutoCraft(a, s), true
	case *mc.CraftRecipeOptionalStackRequestAction:
		return h.handleCraftRecipeOptional(a, req.FilterStrings), true
	case *mc.CraftLoomRecipeStackRequestAction:
		return h.handleLoomCraft(a), true
	case *mc.CraftGrindstoneRecipeStackRequestAction:
		from := len(h.batch.Changes())
		err := h.batch.Grind()
		h.recordBatchChanges(from)
		return err, true
	case *mc.CraftCreativeStackRequestAction:
		return h.handleCreativeCraft(a, c), true
	case *mc.MineBlockStackRequestAction:
		return h.handleMineBlock(a, s, tx), true
	case *mc.CreateStackRequestAction:
		from := len(h.batch.Changes())
		err := h.batch.TakeResult(int(a.ResultsSlot))
		h.recordBatchChanges(from)
		return err, true
	case *mc.ConsumeStackRequestAction:
		return h.handleConsume(a, s, tx), true
	case *mc.CraftResultsDeprecatedStackRequestAction:
		return h.handleCraftResults(a), true
	default:
		return nil, false
	}
}

// handleCraftRecipe handles a CraftRecipe stack request action: by the table
// that is open if it is one that crafts in a way of its own, else as a craft
// of the grid.
func (h *ItemStackRequestHandler) handleCraftRecipe(a *mc.CraftRecipeStackRequestAction, s *Session, tx *world.Tx) error {
	if s.merchantTradeOpen() {
		return h.handleMerchantCraft(a)
	}
	if m := s.menu.Load(); m != nil {
		switch tx.Block(m.Pos()).(type) {
		case block.SmithingTable:
			return h.handleSmithing(a, s)
		case block.Stonecutter:
			return h.handleStonecutting(a, s)
		case block.EnchantingTable:
			return h.handleEnchant(a)
		}
	}
	return h.handleCraft(a, s)
}

// handleConsume handles a Consume stack request action: it pays for the
// special recipe or the recipe-book craft in progress, and is ignored if none
// is.
func (h *ItemStackRequestHandler) handleConsume(a *mc.ConsumeStackRequestAction, s *Session, tx *world.Tx) error {
	if h.multiCraft != nil {
		return h.handleMultiConsume(a, s, tx)
	}
	if h.autoCraft != nil {
		return h.handleAutoCraftConsume(a, s, tx)
	}
	return nil
}

// handleCraftResults handles a CraftResultsDeprecated stack request action:
// it names the result of the special recipe in progress, and is ignored if
// none is.
func (h *ItemStackRequestHandler) handleCraftResults(a *mc.CraftResultsDeprecatedStackRequestAction) error {
	if h.multiCraft != nil {
		return h.handleMultiResult(a)
	}
	return nil
}
