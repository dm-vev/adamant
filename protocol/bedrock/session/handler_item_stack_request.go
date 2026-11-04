package session

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// ItemStackRequestHandler handles the ItemStackRequest packet. It handles the actions done within the
// inventory: each request is a batch of server/menu, whose operations the actions name.
type ItemStackRequestHandler struct {
	currentRequest int32

	changes         map[byte]map[byte]changeInfo
	responseChanges map[int32]map[*inventory.Inventory]map[byte]responseChange

	// batch is the batch of server/menu that the request being handled
	// runs as. Its journal holds the changes the response reports and that
	// a refusal undoes, and its results those of the crafts of the request.
	batch      *menu.Tx
	multiCraft *multiCraft
	autoCraft  *autoCraft

	current       time.Time
	ignoreDestroy bool
}

// responseChange represents a change in a specific item stack response. It holds the timestamp of the
// response which is used to get rid of changes that the client will have received.
type responseChange struct {
	id        int32
	timestamp time.Time
}

// changeInfo holds information on a slot change initiated by an item stack request: the slot as the response to
// the request reports it.
type changeInfo struct {
	after v2193.StackResponseSlotInfo
}

// multiCraft is a special recipe being crafted by the request being handled.
// The craft of the batch keeps the snapshot of its inputs; the session keeps
// the slots the client consumed and the result it named.
type multiCraft struct {
	batch    *menu.MultiCraft
	times    int
	consumed map[byte]int
	result   *v2193.StackRequestItem
}

// autoCraft is a recipe-book craft in progress. Its inputs are paid from
// the slots the client names in the Consume actions that follow the
// AutoCraftRecipe action, in the crafting grid or the inventory: BDS
// 1.26.52.3 takes exactly those slots and refuses a craft whose Consume
// actions do not pay for all of its inputs. The craft of the batch keeps
// what is owed and taken; the session keeps the slots the client named.
type autoCraft struct {
	batch    *menu.AutoCraft
	id       uint32
	consumed map[v2193.StackRequestSlotInfo]bool
}

// Handle ...
func (h *ItemStackRequestHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*v2193.ItemStackRequestPacket)
	h.current = time.Now()

	s.inTransaction.Store(true)
	defer s.inTransaction.Store(false)

	for _, req := range pk.Requests {
		if err := h.handleRequest(req, s, tx, c); err != nil {
			// Item stacks being out of sync isn't uncommon, so don't error. Just debug the error and let the
			// revert do its work.
			s.conf.Log.Debug("process packet: ItemStackRequest: resolve item stack request: " + err.Error())
		}
	}
	return nil
}

// handleRequest resolves a single item stack request from the client.
func (h *ItemStackRequestHandler) handleRequest(req v2193.ItemStackRequest, s *Session, tx *world.Tx, c Controllable) (err error) {
	h.currentRequest = req.RequestID
	h.multiCraft, h.autoCraft = nil, nil
	h.batch = c.BeginMenu(s.nameSlot)
	defer func() {
		h.multiCraft, h.autoCraft = nil, nil
		if err != nil {
			h.reject(req.RequestID, s, refusalStatus(err))
			return
		}
		h.batch.Commit()
		h.batch = nil
		h.resolve(req.RequestID, s)
		h.ignoreDestroy = false
	}()

	for _, action := range req.Actions {
		if h.multiCraft != nil {
			switch action.(type) {
			case *v2193.ConsumeStackRequestAction, *v2193.CraftResultsDeprecatedStackRequestAction:
			default:
				return fmt.Errorf("multi recipe contains unexpected action %T", action)
			}
		}
		if _, consume := action.(*v2193.ConsumeStackRequestAction); h.autoCraft != nil && !consume {
			// The Consume actions of a recipe-book craft have all come:
			// what follows takes its results.
			if err = h.finishAutoCraft(); err != nil {
				err = fmt.Errorf("%T: %w", action, err)
				return
			}
		}
		switch a := action.(type) {
		case *v2193.TakeStackRequestAction:
			err = h.handleTake(a, s, tx)
		case *v2193.PlaceStackRequestAction:
			err = h.handlePlace(a, s, tx)
		case *v2193.SwapStackRequestAction:
			err = h.handleSwap(a, s, tx)
		case *v2193.DestroyStackRequestAction:
			err = h.handleDestroy(a, s, tx, c)
		case *v2193.DropStackRequestAction:
			err = h.handleDrop(a, s, tx)
		case *v2193.BeaconPaymentStackRequestAction:
			err = h.handleBeaconPayment(a)
		case *v2193.CraftRecipeStackRequestAction:
			if s.merchantTradeOpen() {
				err = h.handleMerchantCraft(a)
				break
			}
			if m := s.menu.Load(); m != nil {
				var special bool
				switch tx.Block(m.Pos()).(type) {
				case block.SmithingTable:
					err, special = h.handleSmithing(a, s), true
				case block.Stonecutter:
					err, special = h.handleStonecutting(a, s), true
				case block.EnchantingTable:
					err, special = h.handleEnchant(a), true
				}
				if special {
					// This was a "special action" and was handled, so we can move onto the next action.
					break
				}
			}
			err = h.handleCraft(a, s)
		case *v2193.AutoCraftRecipeStackRequestAction:
			err = h.handleAutoCraft(a, s)
		case *v2193.CraftRecipeOptionalStackRequestAction:
			err = h.handleCraftRecipeOptional(a, req.FilterStrings)
		case *v2193.CraftLoomRecipeStackRequestAction:
			err = h.handleLoomCraft(a)
		case *v2193.CraftGrindstoneRecipeStackRequestAction:
			err = h.batchOp(h.batch.Grind)
		case *v2193.CraftCreativeStackRequestAction:
			err = h.handleCreativeCraft(a, c)
		case *v2193.MineBlockStackRequestAction:
			err = h.handleMineBlock(a, s, tx)
		case *v2193.CreateStackRequestAction:
			err = h.batchOp(func() error { return h.batch.TakeResult(int(a.ResultsSlot)) })
		case *v2193.ConsumeStackRequestAction:
			if h.multiCraft != nil {
				err = h.handleMultiConsume(a, s, tx)
			} else if h.autoCraft != nil {
				err = h.handleAutoCraftConsume(a, s, tx)
			}
		case *v2193.CraftResultsDeprecatedStackRequestAction:
			if h.multiCraft != nil {
				err = h.handleMultiResult(a)
			}
		default:
			return fmt.Errorf("unhandled stack request action %#v", action)
		}
		if err != nil {
			err = fmt.Errorf("%T: %w", action, err)
			return
		}
	}
	if h.multiCraft != nil {
		return h.finishMultiCraft()
	}
	if h.autoCraft != nil {
		return h.finishAutoCraft()
	}
	return
}

// handleTake handles a Take stack request action.
func (h *ItemStackRequestHandler) handleTake(a *v2193.TakeStackRequestAction, s *Session, tx *world.Tx) error {
	switch a.Source.Container.ContainerID {
	case v2193.ContainerTradeResultPreview, v2193.ContainerTradeTwoResultPreview:
		return h.handleMerchantTrade(a.Source, a.Destination, a.Count, s, tx)
	default:
		return h.handleTransfer(a.Source, a.Destination, a.Count, s, tx)
	}
}

// handleMerchantTrade takes the result of an offer of the merchant of the
// trade screen, which the client names by its index, to a slot.
func (h *ItemStackRequestHandler) handleMerchantTrade(from, to v2193.StackRequestSlotInfo, count byte, s *Session, tx *world.Tx) error {
	dest, ok := s.ref(to, tx)
	if !ok {
		return fmt.Errorf("unknown trade result destination %v", to.Container.ContainerID)
	}
	// The client takes the result of an offer from the slot of its
	// index.
	return tradeResult(h.batchOp(func() error { return h.batch.TakeTrade(int(from.Slot), int(count), dest) }))
}

// tradeResult returns err, the error of an operation that put the result
// of a trade in a slot, with the status BDS 1.26.52.3 refuses the request
// with if the slot does not take it: 50, where it refuses an item another
// operation puts there with 55.
func tradeResult(err error) error {
	if errors.Is(err, menu.ErrSlotRefusesItem) {
		return refusal{status: v2193.ItemStackResponseStatusFailedToValidateDstSlot, err: err}
	}
	return err
}

// handlePlace handles a Place stack request action.
func (h *ItemStackRequestHandler) handlePlace(a *v2193.PlaceStackRequestAction, s *Session, tx *world.Tx) error {
	return h.handleTransfer(a.Source, a.Destination, a.Count, s, tx)
}

// handleTransfer handles the transferring of x count from a source slot to a destination slot.
func (h *ItemStackRequestHandler) handleTransfer(from, to v2193.StackRequestSlotInfo, count byte, s *Session, tx *world.Tx) error {
	src, dst, err := s.refPair(from, to, tx)
	if err != nil {
		return err
	}
	if err := sameSlot(src, dst); err != nil {
		return err
	}
	if err := h.verifySlots(s, tx, from, to); err != nil {
		return fmt.Errorf("source slot out of sync: %w", err)
	}
	err = h.batchOp(func() error { return h.batch.Move(src, dst, int(count)) })
	if from.Container.ContainerID == v2193.ContainerCreatedOutput && s.merchantTradeOpen() {
		return tradeResult(err)
	}
	return err
}

// handleSwap handles a Swap stack request action.
func (h *ItemStackRequestHandler) handleSwap(a *v2193.SwapStackRequestAction, s *Session, tx *world.Tx) error {
	src, dst, err := s.refPair(a.Source, a.Destination, tx)
	if err != nil {
		return err
	}
	if err := sameSlot(src, dst); err != nil {
		return err
	}
	if err := h.verifySlots(s, tx, a.Source, a.Destination); err != nil {
		return fmt.Errorf("slot out of sync: %w", err)
	}
	// BDS 1.26.52.3 swaps two stacks only: it refuses a swap of an empty
	// source (49) or with an empty destination (50), as the client moves a
	// stack into an empty slot with a Place or a Take.
	if it, _ := h.batch.Item(src.Slot); it.Empty() {
		return refusal{status: v2193.ItemStackResponseStatusFailedToValidateSrcSlot, err: fmt.Errorf("swap of the empty slot %d", src.Index)}
	}
	if it, _ := h.batch.Item(dst.Slot); it.Empty() {
		return refusal{status: v2193.ItemStackResponseStatusFailedToValidateDstSlot, err: fmt.Errorf("swap with the empty slot %d", dst.Index)}
	}
	return h.batchOp(func() error { return h.batch.Swap(src, dst) })
}

// sameSlot refuses a source and a destination that are one slot, however
// the client names it: as the hot bar or the inventory, the off hand at
// any index. BDS 1.26.52.3 refuses them (48) before it checks the stack
// IDs that name them.
func sameSlot(src, dst menu.Ref) error {
	if src.Slot != dst.Slot {
		return nil
	}
	return refusal{
		status: v2193.ItemStackResponseStatusDstContainerAndSlotEqualToSrcContainerAndSlot,
		err:    fmt.Errorf("slot %d is both the source and the destination: %w", src.Index, menu.ErrSameSlot),
	}
}

// refPair returns the slots of a source and a destination of an item stack
// request, as ref returns them.
func (s *Session) refPair(from, to v2193.StackRequestSlotInfo, tx *world.Tx) (src, dst menu.Ref, err error) {
	src, ok := s.ref(from, tx)
	if !ok {
		return src, dst, fmt.Errorf("unknown source container %v", from.Container.ContainerID)
	}
	dst, ok = s.ref(to, tx)
	if !ok {
		return src, dst, fmt.Errorf("unknown destination container %v", to.Container.ContainerID)
	}
	return src, dst, nil
}

// handleDestroy handles the destroying of an item by moving it into the creative inventory.
func (h *ItemStackRequestHandler) handleDestroy(a *v2193.DestroyStackRequestAction, s *Session, tx *world.Tx, c Controllable) error {
	if h.ignoreDestroy {
		return nil
	}
	if !c.GameMode().CreativeInventory() {
		return fmt.Errorf("can only destroy items in gamemode creative/spectator")
	}
	if err := h.verifySlot(a.Source, s, tx); err != nil {
		return fmt.Errorf("source slot out of sync: %w", err)
	}
	from, ok := s.ref(a.Source, tx)
	if !ok {
		return fmt.Errorf("unknown source container %v", a.Source.Container.ContainerID)
	}
	return h.batchOp(func() error { return h.batch.Destroy(from, int(a.Count)) })
}

// handleDrop handles the dropping of an item by moving it outside the inventory while having the
// inventory opened.
func (h *ItemStackRequestHandler) handleDrop(a *v2193.DropStackRequestAction, s *Session, tx *world.Tx) error {
	if err := h.verifySlot(a.Source, s, tx); err != nil {
		return fmt.Errorf("source slot out of sync: %w", err)
	}
	from, ok := s.ref(a.Source, tx)
	if !ok {
		return fmt.Errorf("unknown source container %v", a.Source.Container.ContainerID)
	}
	return h.batchOp(func() error { return h.batch.Drop(from, int(a.Count)) })
}

// handleMineBlock handles the action associated with a block being mined by the player. This seems to be a workaround
// by Mojang to deal with the durability changes client-side.
func (h *ItemStackRequestHandler) handleMineBlock(a *v2193.MineBlockStackRequestAction, s *Session, tx *world.Tx) error {
	slot := v2193.StackRequestSlotInfo{
		Container:      v2193.FullContainerName{ContainerID: v2193.ContainerInventory},
		Slot:           byte(a.HotbarSlot),
		StackNetworkID: a.StackNetworkID,
	}
	if err := h.verifySlot(slot, s, tx); err != nil {
		return err
	}
	r, ok := s.ref(slot, tx)
	if !ok {
		return fmt.Errorf("unknown container %v", slot.Container.ContainerID)
	}
	return h.batchOp(func() error { return h.batch.Touch(r) })
}

// handleCraft handles the CraftRecipe request action.
func (h *ItemStackRequestHandler) handleCraft(a *v2193.CraftRecipeStackRequestAction, s *Session) error {
	craft, ok := s.recipes[a.RecipeNetworkID]
	if !ok {
		// Try dynamic recipes if no static recipe matches
		return h.craftDynamic(int(a.NumberOfCrafts))
	}
	if multi, ok := craft.(recipe.Multi); ok {
		mc, err := h.batch.BeginMultiCraft(multi, int(a.NumberOfCrafts))
		if err != nil {
			return err
		}
		h.multiCraft = &multiCraft{batch: mc, times: int(a.NumberOfCrafts), consumed: make(map[byte]int)}
		return nil
	}
	if err := h.batchOp(func() error { return h.batch.Craft(craft, int(a.NumberOfCrafts)) }); err != nil {
		return fmt.Errorf("recipe with network id %v: %w", a.RecipeNetworkID, err)
	}
	return nil
}

// craftDynamic crafts the dynamic recipe that the crafting grid matches.
func (h *ItemStackRequestHandler) craftDynamic(timesCrafted int) error {
	return h.batchOp(func() error { return h.batch.CraftDynamic(timesCrafted) })
}

// handleMultiConsume consumes a slot of the input of the special recipe
// being crafted.
func (h *ItemStackRequestHandler) handleMultiConsume(a *v2193.ConsumeStackRequestAction, s *Session, tx *world.Tx) error {
	mc := h.multiCraft.batch
	inputs := mc.Inputs()
	inputSlot := int(a.Source.Slot) - inputs[0].Index
	if inputSlot < 0 || inputSlot >= len(inputs) {
		return fmt.Errorf("multi recipe consumed slot outside the crafting grid")
	}
	if container, _ := nameParts(inputs[inputSlot].Name); a.Source.Container.ContainerID != container || a.Count == 0 {
		return fmt.Errorf("multi recipe consumed invalid crafting input")
	}
	if _, ok := h.multiCraft.consumed[a.Source.Slot]; ok {
		return fmt.Errorf("multi recipe consumed crafting slot %v more than once", a.Source.Slot)
	}
	if err := h.verifySlot(a.Source, s, tx); err != nil {
		return err
	}
	if err := mc.Consume(inputs[inputSlot], int(a.Count)); err != nil {
		return err
	}
	h.multiCraft.consumed[a.Source.Slot] = int(a.Count)
	return nil
}

// handleMultiResult keeps the result the client names for the special
// recipe being crafted.
func (h *ItemStackRequestHandler) handleMultiResult(a *v2193.CraftResultsDeprecatedStackRequestAction) error {
	craft := h.multiCraft
	if craft.result != nil || int(a.TimesCrafted) != craft.times || len(a.ResultItems) != 1 {
		return fmt.Errorf("multi recipe supplied invalid result metadata")
	}
	result := a.ResultItems[0]
	if result.Count == 0 {
		return fmt.Errorf("multi recipe result count must be at least 1")
	}
	craft.result = &result
	return nil
}

// finishMultiCraft finishes the special recipe being crafted, whose result
// must be the one the client named.
func (h *ItemStackRequestHandler) finishMultiCraft() error {
	craft := h.multiCraft
	if craft.result == nil {
		return fmt.Errorf("multi recipe requires one result and all consumed inputs")
	}
	requested := *craft.result
	return h.batchOp(func() error {
		return craft.batch.Finish(func(result item.Stack) bool { return requestedMultiResultMatches(result, requested) })
	})
}

// requestedMultiResultMatches reports whether the result a client names for
// a special recipe is the stack the recipe makes.
func requestedMultiResultMatches(want item.Stack, got v2193.StackRequestItem) bool {
	name, meta := want.Item().EncodeItem()
	if got.Identifier != name || got.MetadataValue > math.MaxInt16 || int16(got.MetadataValue) != meta || int(got.Count) != want.Count() {
		return false
	}
	wantNBT := networkItemNBT(want)
	return len(wantNBT) == 0 && len(got.NBTData) == 0 || reflect.DeepEqual(wantNBT, got.NBTData)
}

// handleAutoCraft handles the AutoCraftRecipe request action.
func (h *ItemStackRequestHandler) handleAutoCraft(a *v2193.AutoCraftRecipeStackRequestAction, s *Session) error {
	craft, ok := s.recipes[a.RecipeNetworkID]
	if !ok {
		// Try dynamic recipes if no static recipe matches
		return h.craftDynamic(int(a.NumberOfCrafts))
	}
	ac, err := h.batch.BeginAutoCraft(craft, int(a.NumberOfCrafts))
	if err != nil {
		return fmt.Errorf("recipe with network id %v: %w", a.RecipeNetworkID, err)
	}
	// BDS refuses a recipe-book craft unless the inventory or a
	// crafting table is open, with a crafter open too.
	if s.menu.Load() != nil {
		if s.openedContainerID.Load() != uint32(v2193.ContainerTypeWorkbench) {
			return fmt.Errorf("recipe with network id %v crafted outside a crafting screen", a.RecipeNetworkID)
		}
	} else if !s.invOpened {
		return fmt.Errorf("recipe with network id %v crafted outside a crafting screen", a.RecipeNetworkID)
	}
	h.autoCraft = &autoCraft{batch: ac, id: a.RecipeNetworkID, consumed: make(map[v2193.StackRequestSlotInfo]bool)}
	return nil
}

// handleAutoCraftConsume pays for the recipe-book craft in progress with
// the slot a Consume action names.
func (h *ItemStackRequestHandler) handleAutoCraftConsume(a *v2193.ConsumeStackRequestAction, s *Session, tx *world.Tx) error {
	craft := h.autoCraft
	switch a.Source.Container.ContainerID {
	case v2193.ContainerCombinedHotBarAndInventory:
	case v2193.ContainerCraftingInput:
		if !craft.batch.InGrid(menu.Slot{Inv: s.ui, Index: int(a.Source.Slot)}) {
			return fmt.Errorf("recipe %v: consumed slot %v outside the crafting grid", craft.id, a.Source.Slot)
		}
	default:
		return fmt.Errorf("recipe %v: cannot consume from container %v", craft.id, a.Source.Container.ContainerID)
	}
	key := v2193.StackRequestSlotInfo{Container: a.Source.Container, Slot: a.Source.Slot}
	if craft.consumed[key] {
		return fmt.Errorf("recipe %v: consumed slot %v of container %v more than once", craft.id, a.Source.Slot, a.Source.Container.ContainerID)
	}
	craft.consumed[key] = true
	if err := h.verifySlot(a.Source, s, tx); err != nil {
		return err
	}
	r, ok := s.ref(a.Source, tx)
	if !ok {
		return fmt.Errorf("unknown container %v", a.Source.Container.ContainerID)
	}
	if err := h.batchOp(func() error { return craft.batch.Pay(r, int(a.Count)) }); err != nil {
		return fmt.Errorf("recipe %v: %w", craft.id, err)
	}
	return nil
}

// finishAutoCraft creates the results of the recipe-book craft in progress
// once the client has named all the slots that pay for it.
func (h *ItemStackRequestHandler) finishAutoCraft() error {
	craft := h.autoCraft
	h.autoCraft = nil
	if err := h.batchOp(craft.batch.Finish); err != nil {
		return fmt.Errorf("recipe %v: %w", craft.id, err)
	}
	return nil
}

// handleCreativeCraft handles the CreativeCraft request action.
func (h *ItemStackRequestHandler) handleCreativeCraft(a *v2193.CraftCreativeStackRequestAction, c Controllable) error {
	if !c.GameMode().CreativeInventory() {
		return fmt.Errorf("can only craft creative items in gamemode creative/spectator")
	}
	if a.CreativeItemNetworkID == 0 {
		return fmt.Errorf("creative item network ID must be at least 1")
	}
	return h.batchOp(func() error { return h.batch.CreateCreative(int(a.CreativeItemNetworkID - 1)) })
}

// handleCraftRecipeOptional handles the CraftRecipeOptional request action, sent when taking a result from an anvil
// menu. It also contains information such as the new name of the item and the multi-recipe network ID.
func (h *ItemStackRequestHandler) handleCraftRecipeOptional(a *v2193.CraftRecipeOptionalStackRequestAction, filterStrings []string) error {
	var name *string
	if len(filterStrings) > 0 {
		// Guard against client-provided indexes that would otherwise panic when slicing.
		index := int64(a.FilterStringIndex)
		if index < 0 || index >= int64(len(filterStrings)) {
			return fmt.Errorf("filter string index %v is out of bounds", a.FilterStringIndex)
		}
		name = &filterStrings[index]
	}
	return h.batchOp(func() error { return h.batch.Repair(name) })
}

// handleSmithing handles a CraftRecipe stack request action made using a smithing table.
func (h *ItemStackRequestHandler) handleSmithing(a *v2193.CraftRecipeStackRequestAction, s *Session) error {
	craft, ok := s.recipes[a.RecipeNetworkID]
	if !ok {
		return fmt.Errorf("recipe with network id %v does not exist", a.RecipeNetworkID)
	}
	return h.batchOp(func() error { return h.batch.Smith(craft) })
}

// handleStonecutting handles a CraftRecipe stack request action made using a stonecutter.
func (h *ItemStackRequestHandler) handleStonecutting(a *v2193.CraftRecipeStackRequestAction, s *Session) error {
	craft, ok := s.recipes[a.RecipeNetworkID]
	if !ok {
		return fmt.Errorf("recipe with network id %v does not exist", a.RecipeNetworkID)
	}
	return h.batchOp(func() error { return h.batch.Cut(craft, int(a.NumberOfCrafts)) })
}

// handleLoomCraft handles a CraftLoomRecipe stack request action made using a loom table.
func (h *ItemStackRequestHandler) handleLoomCraft(a *v2193.CraftLoomRecipeStackRequestAction) error {
	pattern, ok := block.BannerPatternByID(a.Pattern)
	if !ok {
		return fmt.Errorf("unknown banner pattern id %q", a.Pattern)
	}
	return h.batchOp(func() error { return h.batch.Weave(pattern, int(a.TimesCrafted)) })
}

// handleBeaconPayment handles the selection of effects in a beacon and the removal of the item used to pay
// for those effects.
func (h *ItemStackRequestHandler) handleBeaconPayment(a *v2193.BeaconPaymentStackRequestAction) error {
	primary, err := beaconEffect(a.PrimaryEffect)
	if err != nil {
		return fmt.Errorf("primary effect selected is not allowed: %w", err)
	}
	secondary, err := beaconEffect(a.SecondaryEffect)
	if err != nil {
		return fmt.Errorf("secondary effect selected is not allowed: %w", err)
	}
	if err := h.batchOp(func() error { return h.batch.PayBeacon(primary, secondary) }); err != nil {
		return err
	}
	// The client will send a Destroy action after this action, which
	// is ignored: the payment is gone already.
	h.ignoreDestroy = true
	return nil
}

// beaconEffect returns the lasting effect of the ID of a beacon payment, nil
// for 0, no effect. An ID of no effect, or of an instant one, fails.
func beaconEffect(id int32) (effect.LastingType, error) {
	if id == 0 {
		return nil, nil
	}
	e, ok := effect.ByID(int(id))
	if !ok {
		return nil, fmt.Errorf("unknown effect %v", id)
	}
	lasting, ok := e.(effect.LastingType)
	if !ok {
		return nil, fmt.Errorf("effect %v is not lasting", id)
	}
	return lasting, nil
}

// merchantTradeOpen reports if the session has a trade screen open.
func (s *Session) merchantTradeOpen() bool {
	m := s.menu.Load()
	return m != nil && m.Kind() == menu.Merchant
}

// handleMerchantCraft handles the trade request the Bedrock trade screen
// sends: a CraftRecipe action naming the offer by its network ID, which is
// its index plus one, followed by Consume actions for the inputs the player
// put in the two input slots and the taking of the created output.
func (h *ItemStackRequestHandler) handleMerchantCraft(a *v2193.CraftRecipeStackRequestAction) error {
	// The client names the offer by its network ID, its index plus one.
	index, times := int(a.RecipeNetworkID)-1, max(int(a.NumberOfCrafts), 1)
	return h.batchOp(func() error { return h.batch.Trade(index, times) })
}

// verifySlots verifies a list of slots passed.
func (h *ItemStackRequestHandler) verifySlots(s *Session, tx *world.Tx, slots ...v2193.StackRequestSlotInfo) error {
	for _, slot := range slots {
		if err := h.verifySlot(slot, s, tx); err != nil {
			return err
		}
	}
	return nil
}

// verifySlot checks if the slot passed by the client is the same as that expected by the server.
func (h *ItemStackRequestHandler) verifySlot(slot v2193.StackRequestSlotInfo, s *Session, tx *world.Tx) error {
	if err := h.tryAcknowledgeChanges(s, tx, slot); err != nil {
		return err
	}
	if len(h.responseChanges) > 256 {
		return fmt.Errorf("too many unacknowledged request slot changes")
	}
	inv, ok := s.invByID(int32(slot.Container.ContainerID), tx)
	if !ok {
		return fmt.Errorf("unknown container %v", slot.Container.ContainerID)
	}

	i, err := h.itemInSlot(slot, s, tx)
	if err != nil {
		return err
	}
	clientID, err := h.resolveID(inv, slot)
	if err != nil {
		return err
	}
	// The client seems to send negative stack network IDs for predictions, which we can ignore. We'll simply
	// override this network ID later.
	// item.Stack keeps its network stack ID unexported; session.item_id reads it via go:linkname.
	if id := item_id(i); id != clientID {
		return fmt.Errorf("stack ID mismatch: client expected %v, but server had %v", clientID, id)
	}
	return nil
}

// resolveID resolves the stack network ID in the slot passed. If it is negative, it points to an earlier
// request, in which case it will look it up in the changes of an earlier response to a request to find the
// actual stack network ID in the slot. If it is positive, the ID will be returned again.
func (h *ItemStackRequestHandler) resolveID(inv *inventory.Inventory, slot v2193.StackRequestSlotInfo) (int32, error) {
	if slot.StackNetworkID >= 0 {
		return slot.StackNetworkID, nil
	}
	containerChanges, ok := h.responseChanges[slot.StackNetworkID]
	if !ok {
		return 0, fmt.Errorf("slot pointed to stack request %v, but request could not be found", slot.StackNetworkID)
	}
	changes, ok := containerChanges[inv]
	if !ok {
		return 0, fmt.Errorf("slot pointed to stack request %v with container %v, but that container was not changed in the request", slot.StackNetworkID, slot.Container.ContainerID)
	}
	actual, ok := changes[slot.Slot]
	if !ok {
		return 0, fmt.Errorf("slot pointed to stack request %v with container %v and slot %v, but that slot was not changed in the request", slot.StackNetworkID, slot.Container.ContainerID, slot.Slot)
	}
	return actual.id, nil
}

// tryAcknowledgeChanges iterates through all cached response changes and checks if the stack request slot
// info passed from the client has the right stack network ID in any of the stored slots. If this is the case,
// that entry is removed, so that the maps are cleaned up eventually.
func (h *ItemStackRequestHandler) tryAcknowledgeChanges(s *Session, tx *world.Tx, slot v2193.StackRequestSlotInfo) error {
	inv, ok := s.invByID(int32(slot.Container.ContainerID), tx)
	if !ok {
		return fmt.Errorf("could not find container with id %v", slot.Container.ContainerID)
	}

	for requestID, containerChanges := range h.responseChanges {
		for newInv, changes := range containerChanges {
			for slotIndex, val := range changes {
				if (slot.Slot == slotIndex && slot.StackNetworkID >= 0 && newInv == inv) || h.current.Sub(val.timestamp) > time.Second*5 {
					delete(changes, slotIndex)
				}
			}
			if len(changes) == 0 {
				delete(containerChanges, newInv)
			}
		}
		if len(containerChanges) == 0 {
			delete(h.responseChanges, requestID)
		}
	}
	return nil
}

// itemInSlot looks for the item in the slot as indicated by the slot info passed.
func (h *ItemStackRequestHandler) itemInSlot(slot v2193.StackRequestSlotInfo, s *Session, tx *world.Tx) (item.Stack, error) {
	inv, ok := s.invByID(int32(slot.Container.ContainerID), tx)
	if !ok {
		return item.Stack{}, fmt.Errorf("unable to find container with ID %v", slot.Container.ContainerID)
	}

	sl := int(slot.Slot)
	if inv == s.offHand {
		sl = 0
	}

	i, err := inv.Item(sl)
	if err != nil {
		return i, err
	}
	return i, nil
}

// recordChange records that the slot of the container ID passed, of the
// inventory inv, was set to i: the response to the request shows it, and a
// later request may name the stack by the ID of this one.
func (h *ItemStackRequestHandler) recordChange(container, slot byte, inv *inventory.Inventory, i item.Stack) {
	respSlot := v2193.StackResponseSlotInfo{
		Slot:                 slot,
		HotbarSlot:           slot,
		Count:                byte(i.Count()),
		StackNetworkID:       item_id(i),
		DurabilityCorrection: int32(i.MaxDurability() - i.Durability()),
	}

	if h.changes[container] == nil {
		h.changes[container] = map[byte]changeInfo{}
	}
	h.changes[container][slot] = changeInfo{after: respSlot}

	if h.responseChanges[h.currentRequest] == nil {
		h.responseChanges[h.currentRequest] = map[*inventory.Inventory]map[byte]responseChange{}
	}
	if h.responseChanges[h.currentRequest][inv] == nil {
		h.responseChanges[h.currentRequest][inv] = map[byte]responseChange{}
	}
	h.responseChanges[h.currentRequest][inv][slot] = responseChange{
		id:        respSlot.StackNetworkID,
		timestamp: h.current,
	}
}

// batchOp runs op, an operation of the batch of the request, and records
// the changes it made, those before it failed too: the response shows them,
// and a later request may name their stacks even if this one is refused.
func (h *ItemStackRequestHandler) batchOp(op func() error) error {
	from := len(h.batch.Changes())
	err := op()
	for _, c := range h.batch.Changes()[from:] {
		container, slot := nameParts(c.Ref.Name)
		h.recordChange(container, slot, c.Ref.Inv, c.After)
	}
	return err
}

// resolve resolves the request with the ID passed.
func (h *ItemStackRequestHandler) resolve(id int32, s *Session) {
	info := make([]v2193.StackResponseContainerInfo, 0, len(h.changes))
	for container, slotInfo := range h.changes {
		slots := make([]v2193.StackResponseSlotInfo, 0, len(slotInfo))
		for _, slot := range slotInfo {
			slots = append(slots, slot.after)
		}
		info = append(info, v2193.StackResponseContainerInfo{
			Container: v2193.FullContainerName{ContainerID: container},
			SlotInfo:  slots,
		})
	}
	s.writePacket(&v2193.ItemStackResponsePacket{Responses: []v2193.ItemStackResponse{{
		Status:        v2193.ItemStackResponseStatusOK,
		RequestID:     id,
		ContainerInfo: info,
	}}})

	h.changes = map[byte]map[byte]changeInfo{}
}

// refusal is an error that refuses a request with a status of its own: the
// one BDS 1.26.52.3 refuses the request with.
type refusal struct {
	status uint8
	err    error
}

func (r refusal) Error() string { return r.err.Error() }
func (r refusal) Unwrap() error { return r.err }

// refusalStatus returns the status of the response that refuses a request
// for err: that of the refusal it wraps, BDS's for the reasons of the menu
// it names, or else the generic error.
func refusalStatus(err error) uint8 {
	var r refusal
	var missing menu.MissingInputError
	switch {
	case errors.As(err, &r):
		return r.status
	case errors.As(err, &missing) && missing.Role == menu.RoleSmithingTemplate:
		// The input and the material are there: BDS finds no recipe of
		// what the table holds.
		return v2193.ItemStackResponseStatusMismatchedRecipeForInputGridItems
	case errors.As(err, &missing):
		return v2193.ItemStackResponseStatusMissingInputItem
	case errors.Is(err, menu.ErrSameSlot):
		return v2193.ItemStackResponseStatusDstContainerAndSlotEqualToSrcContainerAndSlot
	case errors.Is(err, menu.ErrSlotRefusesItem):
		return v2193.ItemStackResponseStatusCannotPlaceItem
	}
	return v2193.ItemStackResponseStatusError
}

// reject rejects the item stack request sent by the client so that it is
// reverted client-side, with the status passed.
func (h *ItemStackRequestHandler) reject(id int32, s *Session, status uint8) {
	s.writePacket(&v2193.ItemStackResponsePacket{
		Responses: []v2193.ItemStackResponse{{
			Status:    status,
			RequestID: id,
		}},
	})

	// Undo the changes made for the actions that were valid, the last first, so that a slot changed more than
	// once ends up with what it held before the request.
	h.batch.Rollback()
	h.batch = nil
	h.changes = map[byte]map[byte]changeInfo{}
}
