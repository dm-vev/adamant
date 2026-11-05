package session

import (
	"fmt"
	"reflect"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// stackRequest is the request an action being handled belongs to, and what
// the handlers of its actions are given beside the handler: the session, the
// transaction and the player.
type stackRequest struct {
	req mc.ItemStackRequest
	s   *Session
	tx  *world.Tx
	c   Controllable
}

// stackActionHandler handles an action of an item stack request.
type stackActionHandler func(h *ItemStackRequestHandler, action mc.StackRequestAction, r *stackRequest) error

// stackActionEntry is a row of stackRequestActions: the type of an action and
// the function that handles it.
type stackActionEntry struct {
	action reflect.Type
	run    stackActionHandler
}

// stackAction returns the row of the action type A handled by f, which is
// given the action as the type it is.
func stackAction[A mc.StackRequestAction](f func(h *ItemStackRequestHandler, a A, r *stackRequest) error) stackActionEntry {
	return stackActionEntry{
		action: reflect.TypeFor[A](),
		run: func(h *ItemStackRequestHandler, action mc.StackRequestAction, r *stackRequest) error {
			return f(h, action.(A), r)
		},
	}
}

// stackActions returns the table of the rows passed by the type of the action.
// It panics for two rows of one type.
func stackActions(rows ...stackActionEntry) map[reflect.Type]stackActionHandler {
	table := make(map[reflect.Type]stackActionHandler, len(rows))
	for _, row := range rows {
		if _, dup := table[row.action]; dup {
			panic(fmt.Sprintf("two handlers for the stack request action %v", row.action))
		}
		table[row.action] = row.run
	}
	return table
}

// stackRequestActions maps the type of an action of an item stack request to
// the function that handles it. An action of a type it does not hold is
// refused as unhandled.
//
//go:generate go run ./internal/gentables -kind stack
var stackRequestActions = stackActions(
	stackAction(func(h *ItemStackRequestHandler, a *mc.TakeStackRequestAction, r *stackRequest) error {
		return h.handleTake(a, r.s, r.tx)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.PlaceStackRequestAction, r *stackRequest) error {
		return h.handlePlace(a, r.s, r.tx)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.SwapStackRequestAction, r *stackRequest) error {
		return h.handleSwap(a, r.s, r.tx)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.DestroyStackRequestAction, r *stackRequest) error {
		return h.handleDestroy(a, r.s, r.tx, r.c)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.DropStackRequestAction, r *stackRequest) error {
		return h.handleDrop(a, r.s, r.tx)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.BeaconPaymentStackRequestAction, _ *stackRequest) error {
		return h.handleBeaconPayment(a)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.CraftRecipeStackRequestAction, r *stackRequest) error {
		return h.handleCraftRecipe(a, r.s, r.tx)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.AutoCraftRecipeStackRequestAction, r *stackRequest) error {
		return h.handleAutoCraft(a, r.s)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.CraftRecipeOptionalStackRequestAction, r *stackRequest) error {
		return h.handleCraftRecipeOptional(a, r.req.FilterStrings)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.CraftLoomRecipeStackRequestAction, _ *stackRequest) error {
		return h.handleLoomCraft(a)
	}),
	stackAction(func(h *ItemStackRequestHandler, _ *mc.CraftGrindstoneRecipeStackRequestAction, _ *stackRequest) error {
		return h.batchOp(h.batch.Grind)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.CraftCreativeStackRequestAction, r *stackRequest) error {
		return h.handleCreativeCraft(a, r.c)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.MineBlockStackRequestAction, r *stackRequest) error {
		return h.handleMineBlock(a, r.s, r.tx)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.CreateStackRequestAction, _ *stackRequest) error {
		return h.batchOp(func() error { return h.batch.TakeResult(int(a.ResultsSlot)) })
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.ConsumeStackRequestAction, r *stackRequest) error {
		return h.handleConsume(a, r.s, r.tx)
	}),
	stackAction(func(h *ItemStackRequestHandler, a *mc.CraftResultsDeprecatedStackRequestAction, _ *stackRequest) error {
		return h.handleCraftResults(a)
	}),
)

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
