package session_test

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// movesScenario sends the requests that move, swap, drop, destroy and
// touch items and craft creative items, for TestGoldenMenuRequests: each
// accepted, and refused for every check of the operation, in the inventory
// screen, a chest and a furnace, in survival and in creative. Handlers of
// the inventories record what they are called with, and cancel some of it.
func (s *scenario) movesScenario() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	// The orbs of the furnace fly off at random.
	normaliseOrbs = true
	cursor := func() v2193.StackRequestSlotInfo { return s.slot(v2193.ContainerCursor, uiCursor) }
	helmet := item.NewStack(item.Helmet{Tier: item.ArmourTierLeather{}}, 1).WithEnchantments(item.NewEnchantment(enchantment.CurseOfBinding, 1))
	inInventory := item.NewStack(item.Apple{}, 2).WithLockMode(item.LockInInventory)
	inSlot := item.NewStack(item.Apple{}, 2).WithLockMode(item.LockInSlot)
	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})
	s.step("a helmet of the curse of binding worn", func(_ *world.Tx, p *player.Player) {
		p.Armour().SetHelmet(helmet)
	})

	// Move.
	s.give("apples, stone, locked apples, a chestplate and a totem given",
		slotStack{0, item.NewStack(item.Apple{}, 16)},
		slotStack{1, item.NewStack(item.Apple{}, 10)},
		slotStack{2, item.NewStack(block.Stone{}, 4)},
		slotStack{4, inInventory},
		slotStack{5, inSlot},
		slotStack{6, item.NewStack(item.Chestplate{Tier: item.ArmourTierIron{}}, 1)},
		slotStack{9, item.NewStack(item.Apple{}, 64)},
		slotStack{11, item.NewStack(item.Totem{}, 1)},
	)
	s.request("move: four apples onto the apples", nil,
		&v2193.PlaceStackRequestAction{Count: 4, Source: s.slot(inv, 1), Destination: s.slot(inv, 0)})
	s.request("move: three apples split off into an empty slot", nil,
		&v2193.PlaceStackRequestAction{Count: 3, Source: s.slot(inv, 0), Destination: s.slot(inv, 20)})
	s.refuse("refused move: apples onto a full stack",
		&v2193.PlaceStackRequestAction{Count: 10, Source: s.slot(inv, 0), Destination: s.slot(inv, 9)})
	s.refuse("refused move: apples onto stone",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(inv, 2)})
	s.refuse("refused move: seven apples of six",
		&v2193.PlaceStackRequestAction{Count: 7, Source: s.slot(inv, 1), Destination: s.slot(inv, 21)})
	s.attempt("move: an apple onto its own slot",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 1), Destination: s.slot(inv, 1)})
	s.request("move: the apples locked in the inventory to the cursor", nil,
		&v2193.TakeStackRequestAction{Count: 2, Source: s.slot(inv, 4), Destination: cursor()})
	s.request("move: and back", nil,
		&v2193.PlaceStackRequestAction{Count: 2, Source: cursor(), Destination: s.slot(inv, 4)})
	s.refuse("refused move: the apples locked in their slot to the cursor",
		&v2193.TakeStackRequestAction{Count: 1, Source: s.slot(inv, 5), Destination: cursor()})
	s.refuse("refused move: the cursed helmet taken off",
		&v2193.TakeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerArmor, 0), Destination: cursor()})
	s.attempt("move: the cursed helmet onto its own slot",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerArmor, 0), Destination: s.slot(v2193.ContainerArmor, 0)})
	s.request("move: the chestplate put on", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 6), Destination: s.slot(v2193.ContainerArmor, 1)})
	s.attempt("move: an apple put in the slot of the leggings",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerArmor, 2)})
	s.request("move: the totem to the off hand", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 11), Destination: s.slot(v2193.ContainerOffhand, 1)})
	s.request("move: the totem of the off hand to the cursor", nil,
		&v2193.TakeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerOffhand, 1), Destination: cursor()})
	s.request("move: the totem of the cursor back", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: cursor(), Destination: s.slot(v2193.ContainerOffhand, 1)})
	s.clearInventory("the inventory emptied")

	// Bundles.
	s.give("bundles, apples and stone given",
		slotStack{3, item.NewStack(item.Bundle{}, 1)},
		slotStack{10, item.NewStack(item.Bundle{}, 1)},
		slotStack{12, item.NewStack(item.Apple{}, 16)},
		slotStack{13, item.NewStack(item.Apple{}, 64)},
		slotStack{14, item.NewStack(block.Stone{}, 4)},
	)
	s.request("bundle: three apples put in", nil,
		&v2193.PlaceStackRequestAction{Count: 3, Source: s.slot(inv, 12), Destination: s.slot(inv, 3)})
	s.attempt("bundle: a stack of apples put in, more than fits",
		&v2193.PlaceStackRequestAction{Count: 64, Source: s.slot(inv, 13), Destination: s.slot(inv, 3)})
	s.refuse("refused bundle: an apple put in the full bundle",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 12), Destination: s.slot(inv, 3)})
	s.refuse("refused bundle: an apple taken out onto stone",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(inv, 14)})
	s.request("bundle: an apple taken out onto apples", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(inv, 12)})
	s.refuse("refused bundle: two taken out of the one bundle",
		&v2193.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 3), Destination: s.slot(inv, 12)})
	s.refuse("refused bundle: an apple taken out of the empty bundle",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 10), Destination: s.slot(inv, 12)})
	s.request("bundle: the empty bundle moved", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 10), Destination: s.slot(inv, 22)})
	s.clearInventory("the inventory emptied")

	// Swap.
	s.give("apples, stone, locked apples and a chestplate given",
		slotStack{0, item.NewStack(item.Apple{}, 16)},
		slotStack{1, item.NewStack(block.Stone{}, 4)},
		slotStack{2, inSlot},
		slotStack{3, item.NewStack(item.Chestplate{Tier: item.ArmourTierGold{}}, 1)},
		slotStack{4, item.NewStack(item.Apple{}, 5)},
	)
	s.request("swap: the apples and the stone", nil,
		&v2193.SwapStackRequestAction{Source: s.slot(inv, 0), Destination: s.slot(inv, 1)})
	s.refuse("refused swap: the apples locked in their slot",
		&v2193.SwapStackRequestAction{Source: s.slot(inv, 2), Destination: s.slot(inv, 1)})
	s.refuse("refused swap: the cursed helmet",
		&v2193.SwapStackRequestAction{Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerArmor, 0)})
	s.request("swap: the chestplates", nil,
		&v2193.SwapStackRequestAction{Source: s.slot(inv, 3), Destination: s.slot(v2193.ContainerArmor, 1)})
	s.attempt("swap: apples and the slot of the leggings",
		&v2193.SwapStackRequestAction{Source: s.slot(inv, 4), Destination: s.slot(v2193.ContainerArmor, 2)})
	s.attempt("swap: a slot with itself",
		&v2193.SwapStackRequestAction{Source: s.slot(inv, 0), Destination: s.slot(inv, 0)})
	s.clearInventory("the inventory emptied")

	// Drop.
	s.give("apples and locked apples given",
		slotStack{0, item.NewStack(item.Apple{}, 10)},
		slotStack{1, inInventory},
		slotStack{2, item.NewStack(item.Apple{}, 5)},
	)
	s.request("drop: two apples", nil,
		&v2193.DropStackRequestAction{Count: 2, Source: s.slot(inv, 0)})
	s.refuse("refused drop: twenty apples of eight",
		&v2193.DropStackRequestAction{Count: 20, Source: s.slot(inv, 0)})
	s.refuse("refused drop: the cursed helmet",
		&v2193.DropStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerArmor, 0)})
	s.refuse("refused drop: the apples locked in the inventory",
		&v2193.DropStackRequestAction{Count: 1, Source: s.slot(inv, 1)})
	s.request("drop: the apples of the cursor", nil,
		&v2193.TakeStackRequestAction{Count: 2, Source: s.slot(inv, 2), Destination: cursor()},
		&v2193.DropStackRequestAction{Count: 2, Source: s.again(v2193.ContainerCursor, uiCursor)})
	s.request("drop: the totem of the off hand", nil,
		&v2193.DropStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerOffhand, 1)})
	s.tick("the dropped items in the next tick", 1)
	s.removeItems("the dropped items taken away")
	s.clearInventory("the inventory emptied")

	// Touch: the client tells of a tool that wore as it mined.
	s.give("a pickaxe given", slotStack{7, item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1).Damage(3)})
	s.request("mine block: the pickaxe", nil,
		&v2193.MineBlockStackRequestAction{HotbarSlot: 7, PredictedDurability: 1, StackNetworkID: s.slot(inv, 7).StackNetworkID})
	s.refuse("refused mine block: a stale stack ID",
		&v2193.MineBlockStackRequestAction{HotbarSlot: 7, PredictedDurability: 1, StackNetworkID: 9999})
	s.refuse("refused mine block: a slot of no stack",
		&v2193.MineBlockStackRequestAction{HotbarSlot: 30, PredictedDurability: 1, StackNetworkID: 1})

	// Neither destroys nor creates in survival.
	s.give("apples given", slotStack{2, item.NewStack(item.Apple{}, 10)})
	s.refuse("refused destroy: an apple in survival",
		&v2193.DestroyStackRequestAction{Count: 1, Source: s.slot(inv, 2)})
	s.refuse("refused creative craft: in survival",
		&v2193.CraftCreativeStackRequestAction{CreativeItemNetworkID: 1, NumberOfCrafts: 1},
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.created(), Destination: s.slot(inv, 25)})

	// Requests the first action of which is accepted and a later one
	// refused, and requests that name stacks by the ID of an earlier one.
	stale := v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: inv}, Slot: 0, StackNetworkID: 9999}
	refused := s.nextRequest
	s.refuse("refused: an apple moved, then a swap of a stale stack ID",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: s.slot(inv, 26)},
		&v2193.SwapStackRequestAction{Source: stale, Destination: s.slot(inv, 27)})
	s.refuse("refused: the apple named by the ID of the refused request",
		&v2193.PlaceStackRequestAction{Count: 1, Source: v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: inv}, Slot: 26, StackNetworkID: refused}, Destination: s.slot(inv, 27)})
	s.refuse("refused: an apple dropped, then too many",
		&v2193.DropStackRequestAction{Count: 1, Source: s.slot(inv, 2)},
		&v2193.DropStackRequestAction{Count: 30, Source: s.again(inv, 2)})
	s.tick("the dropped apple in the next tick", 1)
	s.removeItems("the dropped apple taken away")
	s.requests("two requests in one packet, the second naming the stack of the first",
		[]v2193.StackRequestAction{&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: s.slot(inv, 26)}},
		[]v2193.StackRequestAction{&v2193.PlaceStackRequestAction{Count: 1, Source: v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: inv}, Slot: 26, StackNetworkID: s.nextRequest}, Destination: s.slot(inv, 27)}})
	s.clearInventory("the inventory emptied")

	// A handler of the inventory sees every take, place and drop, and
	// cancels some.
	log := &handlerLog{}
	s.step("a handler set on the inventory", func(_ *world.Tx, p *player.Player) {
		p.Inventory().Handle(recordingHandler{name: "inventory", log: log, cancel: map[string]bool{"place 30": true, "take 31": true, "drop 32": true}})
	})
	s.give("apples for the handler",
		slotStack{2, item.NewStack(item.Apple{}, 5)},
		slotStack{31, item.NewStack(item.Apple{}, 3)},
		slotStack{32, item.NewStack(item.Apple{}, 3)},
	)
	s.refuse("refused move: the place cancelled",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: s.slot(inv, 30)})
	s.refuse("refused move: the take cancelled",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 31), Destination: s.slot(inv, 33)})
	s.refuse("refused swap: the take cancelled",
		&v2193.SwapStackRequestAction{Source: s.slot(inv, 31), Destination: s.slot(inv, 2)})
	s.refuse("refused drop: the drop cancelled",
		&v2193.DropStackRequestAction{Count: 1, Source: s.slot(inv, 32)})
	s.request("move, swap and drop the handler lets be", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 32), Destination: s.slot(inv, 33)},
		&v2193.SwapStackRequestAction{Source: s.again(inv, 33), Destination: s.slot(inv, 2)},
		&v2193.DropStackRequestAction{Count: 1, Source: s.again(inv, 33)})
	s.tick("the dropped apple in the next tick", 1)
	s.removeItems("the dropped apple taken away")
	s.step("the handler removed", func(_ *world.Tx, p *player.Player) { p.Inventory().Handle(nil) })
	s.step("the handler saw:"+log.String(), func(*world.Tx, *player.Player) {})
	s.clearInventory("the inventory emptied")

	// In creative mode.
	s.gameModeStep("game mode: creative", 1, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeCreative)
	})
	s.give("apples and locked apples given", slotStack{2, item.NewStack(item.Apple{}, 5)}, slotStack{5, inSlot})
	s.request("destroy: an apple", nil,
		&v2193.DestroyStackRequestAction{Count: 1, Source: s.slot(inv, 2)})
	s.refuse("refused destroy: more apples than there are",
		&v2193.DestroyStackRequestAction{Count: 40, Source: s.slot(inv, 2)})
	s.refuse("refused destroy: the apples locked in their slot",
		&v2193.DestroyStackRequestAction{Count: 1, Source: s.slot(inv, 5)})
	s.request("move: the cursed helmet taken off in creative", nil,
		&v2193.TakeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerArmor, 0), Destination: cursor()})
	s.request("destroy: the cursed helmet of the cursor", nil,
		&v2193.DestroyStackRequestAction{Count: 1, Source: cursor()})
	apple := creativeIndex(s, item.Apple{})
	s.request("creative craft: a stack of apples", nil,
		&v2193.CraftCreativeStackRequestAction{CreativeItemNetworkID: apple + 1, NumberOfCrafts: 1},
		&v2193.PlaceStackRequestAction{Count: 64, Source: s.created(), Destination: s.slot(inv, 28)})
	s.refuse("refused creative craft: network ID 0",
		&v2193.CraftCreativeStackRequestAction{CreativeItemNetworkID: 0, NumberOfCrafts: 1})
	s.refuse("refused creative craft: an item that does not exist",
		&v2193.CraftCreativeStackRequestAction{CreativeItemNetworkID: uint32(len(creative.Items()) + 1), NumberOfCrafts: 1})
	s.refuse("refused creative craft: twice in a request, the result once taken",
		&v2193.CraftCreativeStackRequestAction{CreativeItemNetworkID: apple + 1, NumberOfCrafts: 1},
		&v2193.CraftCreativeStackRequestAction{CreativeItemNetworkID: apple + 1, NumberOfCrafts: 1})
	s.gameModeStep("game mode: survival", 1, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeSurvival)
	})
	s.clearInventory("the inventory emptied")

	// A chest, and a furnace that has experience to give.
	s.give("stone, apples and locked apples given",
		slotStack{0, item.NewStack(block.Stone{}, 4)},
		slotStack{2, item.NewStack(item.Apple{}, 5)},
		slotStack{4, inInventory},
	)
	chest := cube.Pos{1, -60, -2}
	s.place("a chest is placed", chest, block.NewChest())
	s.clickBlock("the client opens the chest", chest)
	s.request("move: apples put in the chest", nil,
		&v2193.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 2), Destination: s.slot(v2193.ContainerLevelEntity, 0)})
	s.request("move: the apples locked in the inventory put in the cursor", nil,
		&v2193.TakeStackRequestAction{Count: 2, Source: s.slot(inv, 4), Destination: cursor()})
	s.refuse("refused move: the apples locked in the inventory put in the chest",
		&v2193.PlaceStackRequestAction{Count: 1, Source: cursor(), Destination: s.slot(v2193.ContainerLevelEntity, 1)})
	s.request("move: the locked apples put back", nil,
		&v2193.PlaceStackRequestAction{Count: 2, Source: cursor(), Destination: s.slot(inv, 4)})
	s.request("swap: the stone and the apples of the chest", nil,
		&v2193.SwapStackRequestAction{Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerLevelEntity, 0)})
	s.request("drop: a stone of the chest", nil,
		&v2193.DropStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerLevelEntity, 0)})
	s.refuse("refused swap: of a slot the chest has not",
		&v2193.SwapStackRequestAction{Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerLevelEntity, 40)})
	s.closeContainer("the client closes the chest")
	s.tick("the dropped stone in the next tick", 1)
	s.removeItems("the dropped stone taken away")

	furnace := cube.Pos{-1, -60, 2}
	s.step("a furnace is placed with coal, iron and the experience of smelting it", func(tx *world.Tx, _ *player.Player) {
		tx.SetBlock(furnace, block.NewFurnace(cube.North).DecodeNBT(map[string]any{"StoredXPInt": int32(7)}).(world.Block), nil)
		c := tx.Block(furnace).(block.Container)
		_ = c.Inventory(tx, furnace).SetItem(1, item.NewStack(item.Coal{}, 2))
		_ = c.Inventory(tx, furnace).SetItem(2, item.NewStack(item.IronIngot{}, 3))
	})
	s.clickBlock("the client opens the furnace", furnace)
	s.request("move: coal taken out of the fuel, no experience", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerFurnaceFuel, 1), Destination: s.slot(inv, 34)})
	s.request("move: an iron ingot taken out of the result, the experience given", nil,
		&v2193.TakeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerFurnaceResult, 2), Destination: s.slot(inv, 35)})
	s.removeOrbs("the experience taken away")
	s.request("swap: the coal and the iron of the result, no experience left", nil,
		&v2193.SwapStackRequestAction{Source: s.slot(inv, 34), Destination: s.slot(v2193.ContainerFurnaceResult, 2)})
	s.closeContainer("the client closes the furnace")
	s.clearInventory("the inventory emptied")
}

// requests sends one item stack request packet of a request for each list
// of actions passed, which the server may accept or refuse.
func (s *scenario) requests(heading string, actions ...[]v2193.StackRequestAction) {
	s.t.Helper()
	pk := &v2193.ItemStackRequestPacket{}
	for _, a := range actions {
		pk.Requests = append(pk.Requests, v2193.ItemStackRequest{RequestID: s.nextRequest, Actions: a})
		s.nextRequest -= 2
	}
	before := len(s.responses)
	s.client(heading, pk)
	if got := len(s.responses) - before; got != len(actions) {
		s.t.Errorf("%s: %d requests answered, want %d", heading, got, len(actions))
	}
}

// removeItems removes the item entities of the world of the scenario.
func (s *scenario) removeItems(heading string) {
	s.t.Helper()
	s.removeEntities(heading, entity.ItemType)
}

// removeOrbs removes the experience orbs of the world of the scenario,
// which fly off at random, before they move.
func (s *scenario) removeOrbs(heading string) {
	s.t.Helper()
	s.removeEntities(heading, entity.ExperienceOrbType)
}

func (s *scenario) removeEntities(heading string, t world.EntityType) {
	s.t.Helper()
	s.step(heading, func(tx *world.Tx, _ *player.Player) {
		// In the order they were added: the world keeps them in a map.
		var removed []world.Entity
		for e := range tx.Entities() {
			if e.H().Type() == t {
				removed = append(removed, e)
			}
		}
		slices.SortFunc(removed, func(a, b world.Entity) int { return cmp.Compare(a.H().RuntimeID(), b.H().RuntimeID()) })
		for _, e := range removed {
			tx.RemoveEntity(e)
		}
	})
}

// creativeIndex returns the index of the creative item of the item passed.
func creativeIndex(s *scenario, it world.Item) uint32 {
	s.t.Helper()
	for i, c := range creative.Items() {
		if c.Stack.Item() == it {
			return uint32(i)
		}
	}
	s.t.Fatalf("%T is no creative item", it)
	return 0
}

// handlerLog is what the handlers of a scenario were called with, in order.
type handlerLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *handlerLog) add(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
}

func (l *handlerLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return " " + strings.Join(l.lines, "; ")
}

// recordingHandler is an inventory.Handler that logs each call and cancels
// those named in cancel, as "take 3".
type recordingHandler struct {
	name   string
	log    *handlerLog
	cancel map[string]bool
}

func (h recordingHandler) HandleTake(ctx *inventory.Context, slot int, it item.Stack) {
	h.note(ctx, "take", slot, it)
}

func (h recordingHandler) HandlePlace(ctx *inventory.Context, slot int, it item.Stack) {
	h.note(ctx, "place", slot, it)
}

func (h recordingHandler) HandleDrop(ctx *inventory.Context, slot int, it item.Stack) {
	h.note(ctx, "drop", slot, it)
}

func (h recordingHandler) note(ctx *inventory.Context, kind string, slot int, it item.Stack) {
	name := "none"
	if !it.Empty() {
		name, _ = it.Item().EncodeItem()
	}
	key := fmt.Sprintf("%s %d", kind, slot)
	h.log.add(fmt.Sprintf("%s %s of %d %s", h.name, key, it.Count(), name))
	if h.cancel[key] {
		ctx.Cancel()
	}
}

// normaliseOrbs has the scenarios leave out the random motion of the
// experience orbs the client is sent: their velocity when added is set to
// zero, and the packets that move them after are not recorded. Only
// TestGoldenMenuRequests sets it, in the scenarios that make orbs.
var normaliseOrbs bool

// orbs are the runtime IDs of the experience orbs the client was sent.
var orbs = map[uint64]bool{}

// orb sets the velocity of an experience orb added in the packet v to zero.
func (n *normaliser) orb(v reflect.Value) {
	if pk, ok := v.Addr().Interface().(*v2193.AddActorPacket); ok && pk.EntityType == "minecraft:xp_orb" {
		orbs[pk.EntityRuntimeID] = true
		pk.Velocity, n.touched = [3]float32{}, true
	}
}

// orbMotion reports whether pk moves an experience orb.
func orbMotion(pk proto.Packet) bool {
	switch pk := pk.(type) {
	case *v2193.MoveActorDeltaPacket:
		return orbs[pk.EntityRuntimeID]
	case *v2193.MoveActorAbsolutePacket:
		return orbs[pk.EntityRuntimeID]
	case *v2193.SetActorMotionPacket:
		return orbs[pk.EntityRuntimeID]
	}
	return false
}
