package session_test

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

// bdsMenuRequests is the capture of testdata/bedrock_1_26_52_menu_requests.json:
// what BDS 1.26.52.3 answered to item stack requests it refused and to the
// requests after them, and what it sent when a dispenser and a dropper
// were opened.
type bdsMenuRequests struct {
	Version string `json:"version"`
	BuildID int    `json:"build_id"`
	Cases   []struct {
		Name     string      `json:"name"`
		Packets  []bdsPacket `json:"packets"`
		Requests []struct {
			Label   string `json:"label"`
			Status  uint8  `json:"status"`
			Changes []struct {
				Container uint8 `json:"container"`
				Slot      uint8 `json:"slot"`
				Count     uint8 `json:"count"`
			} `json:"changes"`
		} `json:"requests"`
	} `json:"cases"`
}

// menuRequestsKnownDiverging are the cases of the capture Adamant does not
// answer as BDS did, and why. TestMenuRequestsAgainstBDS fails if one of
// them no longer diverges, so that it leaves this list with the change
// that fixed it.
var menuRequestsKnownDiverging = map[string]string{
	"offhand/place-slot-0": "the off hand is taken at slot 0, which BDS refuses (55): it holds the off hand at slot 1 only",
	"merchant/preview": "the session takes the result of an offer from the preview of the trade screen, paid from the inventory " +
		"(menu.Tx.TakeTrade), and refuses it into the off hand with 50; BDS refuses such a take with 49 whatever the destination, " +
		"named by the stack ID 0 the probe sent, which is the only one tried",
}

// TestMenuRequestsAgainstBDS replays the cases of the capture on a session:
// the same items in the same slots, the same blocks opened, and the same
// requests, named by the stack IDs this server gave. It compares whether
// each request was accepted and, when it was, the slots of the response;
// and the type a block's window opened with. BDS's own statuses of refusal
// are compared for the cases that say so: elsewhere the session refuses
// with the generic error. The inventory screen, which BDS needs open for
// inventory requests, is not opened: the session does not track it.
func TestMenuRequestsAgainstBDS(t *testing.T) {
	b, err := os.ReadFile("testdata/bedrock_1_26_52_menu_requests.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture bdsMenuRequests
	if err := json.Unmarshal(b, &capture); err != nil {
		t.Fatal(err)
	}
	if capture.Version != "1.26.52.3" || capture.BuildID != 51798919 {
		t.Fatalf("capture of BDS %s build %d", capture.Version, capture.BuildID)
	}
	s := newScenario(t, "menu requests", scenarioConfig{})
	s.run(func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			t.Fatal(err)
		}
	})

	const inv = mc.ContainerCombinedHotBarAndInventory
	stale := mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: inv}, Slot: 0, StackNetworkID: 9999}
	type menu struct {
		pos   cube.Pos
		block world.Block
	}
	cases := []struct {
		capture string
		items   map[int]item.Stack
		// prepare, if set, readies the player further, after its items.
		prepare  func(tx *world.Tx, p *player.Player)
		menu     *menu
		requests []func() []mc.StackRequestAction
		// statuses compares the status of each refusal with BDS's too.
		statuses bool
		// offers, if set, are the offers of a villager that stands by,
		// whose trade screen is open for the requests.
		offers []map[string]any
		// knownSlots are the requests of the case, by index, whose slots
		// differ from BDS's for a reason of their own, which they must go
		// on doing; the rest of the case is compared.
		knownSlots map[int]string
	}{
		{
			capture: "inventory/slot-changed-twice-refused",
			items:   map[int]item.Stack{0: item.NewStack(item.Apple{}, 16), 2: item.NewStack(block.Planks{Wood: block.OakWood()}, 8)},
			requests: []func() []mc.StackRequestAction{
				func() []mc.StackRequestAction {
					return []mc.StackRequestAction{
						&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: s.slot(inv, 20)},
						&mc.PlaceStackRequestAction{Count: 1, Source: s.again(inv, 2), Destination: s.slot(inv, 21)},
						&mc.PlaceStackRequestAction{Count: 1, Source: stale, Destination: s.slot(inv, 22)},
					}
				},
				func() []mc.StackRequestAction {
					return []mc.StackRequestAction{&mc.TakeStackRequestAction{Count: 8, Source: s.slot(inv, 2), Destination: s.slot(mc.ContainerCursor, uiCursor)}}
				},
				func() []mc.StackRequestAction {
					return []mc.StackRequestAction{&mc.PlaceStackRequestAction{Count: 8, Source: s.slot(mc.ContainerCursor, uiCursor), Destination: s.slot(inv, 2)}}
				},
			},
		},
		{
			capture: "offhand/place-slot-0",
			items:   map[int]item.Stack{3: item.NewStack(item.Shield{}, 1)},
			requests: []func() []mc.StackRequestAction{
				func() []mc.StackRequestAction {
					return []mc.StackRequestAction{&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(mc.ContainerOffhand, 0)}}
				},
			},
		},
		{
			capture: "offhand/place-slot-1",
			items:   map[int]item.Stack{3: item.NewStack(item.Shield{}, 1)},
			requests: []func() []mc.StackRequestAction{
				func() []mc.StackRequestAction {
					return []mc.StackRequestAction{&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(mc.ContainerOffhand, 1)}}
				},
			},
		},
		{
			capture: "offhand/changed-refused",
			items:   map[int]item.Stack{0: item.NewStack(item.Apple{}, 16), 3: item.NewStack(item.Shield{}, 1), 4: item.NewStack(item.Totem{}, 1)},
			requests: []func() []mc.StackRequestAction{
				func() []mc.StackRequestAction {
					return []mc.StackRequestAction{
						&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(mc.ContainerOffhand, 1)},
						&mc.PlaceStackRequestAction{Count: 1, Source: stale, Destination: s.slot(inv, 22)},
					}
				},
				func() []mc.StackRequestAction {
					return []mc.StackRequestAction{
						&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(mc.ContainerOffhand, 0)},
						&mc.PlaceStackRequestAction{Count: 1, Source: stale, Destination: s.slot(inv, 22)},
					}
				},
				func() []mc.StackRequestAction {
					return []mc.StackRequestAction{&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 4), Destination: s.slot(mc.ContainerOffhand, 1)}}
				},
			},
		},
		{
			capture: "loom/no-screen-open",
			items:   map[int]item.Stack{5: item.NewStack(block.Banner{Colour: item.ColourWhite()}, 1), 6: item.NewStack(item.Dye{Colour: item.ColourRed()}, 1)},
			requests: []func() []mc.StackRequestAction{
				loomCraft(s), loomCraft(s),
				func() []mc.StackRequestAction {
					return []mc.StackRequestAction{&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 5), Destination: s.slot(mc.ContainerLoomInput, uiLoomInput)}}
				},
			},
		},
		{
			capture:  "anvil/level-entity-place",
			items:    map[int]item.Stack{0: item.NewStack(item.Apple{}, 16)},
			menu:     &menu{cube.Pos{-2, -60, 0}, block.Anvil{Type: block.UndamagedAnvil()}},
			requests: []func() []mc.StackRequestAction{levelEntityPlace(s, 0), levelEntityPlace(s, 1)},
		},
		{
			capture:  "crafting_table/level-entity-place",
			items:    map[int]item.Stack{0: item.NewStack(item.Apple{}, 16)},
			menu:     &menu{cube.Pos{-2, -60, 2}, block.CraftingTable{}},
			requests: []func() []mc.StackRequestAction{levelEntityPlace(s, 0), levelEntityPlace(s, 1)},
		},
		{
			capture:  "dispenser/open",
			items:    map[int]item.Stack{0: item.NewStack(item.Apple{}, 16)},
			menu:     &menu{cube.Pos{2, -60, 0}, block.NewDispenser()},
			requests: []func() []mc.StackRequestAction{levelEntityPlace(s, 0)},
		},
		{
			capture:  "dropper/open",
			items:    map[int]item.Stack{0: item.NewStack(item.Apple{}, 16)},
			menu:     &menu{cube.Pos{2, -60, 2}, block.NewDropper()},
			requests: []func() []mc.StackRequestAction{levelEntityPlace(s, 0)},
		},
		{
			capture: "inventory/move-onto-own-slot",
			items:   map[int]item.Stack{0: item.NewStack(item.Apple{}, 6), 1: item.NewStack(item.Apple{}, 10)},
			prepare: func(_ *world.Tx, p *player.Player) {
				p.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierLeather{}}, 1))
			},
			statuses: true,
			requests: []func() []mc.StackRequestAction{
				bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, inv, 1, inv, 1),
				bdsMove(&mc.TakeStackRequestAction{Count: 1}, s.slot, inv, 1, inv, 1),
				bdsMove(&mc.PlaceStackRequestAction{Count: 10}, s.slot, inv, 1, inv, 1),
				bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, mc.ContainerHotBar, 1, inv, 1),
				func() []mc.StackRequestAction {
					return []mc.StackRequestAction{&mc.SwapStackRequestAction{Source: s.slot(inv, 0), Destination: s.slot(inv, 0)}}
				},
				bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, mc.ContainerArmor, 0, mc.ContainerArmor, 0),
				func() []mc.StackRequestAction {
					stale := mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: inv}, Slot: 1, StackNetworkID: 9999}
					return []mc.StackRequestAction{&mc.PlaceStackRequestAction{Count: 1, Source: stale, Destination: stale}}
				},
				func() []mc.StackRequestAction {
					stale := mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: inv}, Slot: 1, StackNetworkID: 9999}
					return []mc.StackRequestAction{&mc.PlaceStackRequestAction{Count: 1, Source: stale, Destination: s.slot(inv, 1)}}
				},
				bdsMove(&mc.TakeStackRequestAction{Count: 6}, s.slot, inv, 0, mc.ContainerCursor, uiCursor),
				bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, mc.ContainerCursor, uiCursor, mc.ContainerCursor, uiCursor),
				bdsMove(&mc.PlaceStackRequestAction{Count: 6}, s.slot, mc.ContainerCursor, uiCursor, inv, 0),
			},
		},
		{
			capture:  "armour/apple-in-leggings-slot",
			items:    map[int]item.Stack{0: item.NewStack(item.Apple{}, 4)},
			statuses: true,
			requests: []func() []mc.StackRequestAction{
				bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, inv, 0, mc.ContainerArmor, 2),
				bdsMove(&mc.TakeStackRequestAction{Count: 1}, s.slot, inv, 0, mc.ContainerArmor, 2),
				bdsSwap(s, inv, 0, mc.ContainerArmor, 2),
			},
		},
		{
			capture:  "offhand/items",
			items:    offHandItems(),
			statuses: true,
			requests: append(append([]func() []mc.StackRequestAction{
				bdsMove(&mc.PlaceStackRequestAction{Count: 2}, s.slot, inv, 0, mc.ContainerOffhand, 1),
				bdsMove(&mc.TakeStackRequestAction{Count: 2}, s.slot, inv, 0, mc.ContainerOffhand, 1),
				bdsSwap(s, inv, 0, mc.ContainerOffhand, 1),
				bdsSwap(s, inv, 11, mc.ContainerOffhand, 1),
				bdsMove(&mc.TakeStackRequestAction{Count: 2}, s.slot, inv, 0, mc.ContainerCursor, uiCursor),
				bdsMove(&mc.PlaceStackRequestAction{Count: 2}, s.slot, mc.ContainerCursor, uiCursor, mc.ContainerOffhand, 1),
				bdsMove(&mc.PlaceStackRequestAction{Count: 2}, s.slot, mc.ContainerCursor, uiCursor, inv, 0),
			}, offHandEach(s)...),
				bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, inv, 11, mc.ContainerOffhand, 1),
				bdsSwap(s, inv, 0, mc.ContainerOffhand, 1),
				bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, inv, 0, mc.ContainerOffhand, 1),
			),
		},
		{
			capture:  "swap/empty-slot",
			items:    map[int]item.Stack{0: item.NewStack(item.Apple{}, 4), 1: item.NewStack(block.Stone{}, 4)},
			statuses: true,
			requests: []func() []mc.StackRequestAction{
				bdsSwap(s, inv, 0, inv, 20),
				bdsSwap(s, inv, 20, inv, 0),
				bdsSwap(s, inv, 20, inv, 21),
				bdsSwap(s, inv, 0, inv, 1),
			},
		},
		{
			capture:  "crafting/result-into-offhand",
			items:    map[int]item.Stack{2: item.NewStack(block.Planks{Wood: block.OakWood()}, 8)},
			statuses: true,
			requests: []func() []mc.StackRequestAction{
				bdsMove(&mc.PlaceStackRequestAction{Count: 2}, s.slot, inv, 2, mc.ContainerCraftingInput, uiInventoryGrid),
				bdsMove(&mc.PlaceStackRequestAction{Count: 2}, s.slot, inv, 2, mc.ContainerCraftingInput, uiInventoryGrid+2),
				stickCraft(s, mc.ContainerOffhand, 1),
				stickCraft(s, mc.ContainerArmor, 2),
				stickCraft(s, inv, 21),
				bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, mc.ContainerCraftingInput, uiInventoryGrid, inv, 2),
				bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, mc.ContainerCraftingInput, uiInventoryGrid+2, inv, 2),
			},
		},
		{
			// BDS refuses the sticks onto the apples with 55, the session
			// with the generic error, as stacks that differ: the statuses
			// are not compared.
			capture: "crafting/result-onto-other-item",
			items: map[int]item.Stack{0: item.NewStack(item.Apple{}, 4), 2: item.NewStack(block.Planks{Wood: block.OakWood()}, 8),
				3: item.NewStack(item.Totem{}, 1)},
			requests: []func() []mc.StackRequestAction{
				bdsMove(&mc.PlaceStackRequestAction{Count: 2}, s.slot, inv, 2, mc.ContainerCraftingInput, uiInventoryGrid),
				bdsMove(&mc.PlaceStackRequestAction{Count: 2}, s.slot, inv, 2, mc.ContainerCraftingInput, uiInventoryGrid+2),
				stickCraft(s, inv, 0),
				bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, inv, 3, mc.ContainerOffhand, 1),
				stickCraft(s, mc.ContainerOffhand, 1),
				bdsMove(&mc.PlaceStackRequestAction{Count: 2}, s.slot, mc.ContainerCraftingInput, uiInventoryGrid, inv, 2),
				bdsMove(&mc.PlaceStackRequestAction{Count: 2}, s.slot, mc.ContainerCraftingInput, uiInventoryGrid+2, inv, 2),
			},
		},
		{
			// The villager of the capture sold an emerald for 32 sticks
			// first.
			capture:  "merchant/result-into-offhand",
			items:    map[int]item.Stack{0: item.NewStack(item.Stick{}, 32), 1: item.NewStack(item.Stick{}, 32)},
			offers:   []map[string]any{bdsOffer(item.NewStack(item.Stick{}, 32), item.NewStack(item.Emerald{}, 1))},
			statuses: true,
			requests: []func() []mc.StackRequestAction{
				bdsMove(&mc.PlaceStackRequestAction{Count: 32}, s.slot, inv, 0, mc.ContainerTradeTwoIngredientOne, uiTradeInput),
				bdsTrade(s, 32, mc.ContainerOffhand, 1),
				bdsTrade(s, 32, mc.ContainerArmor, 2),
				bdsTrade(s, 32, inv, 20),
				bdsMove(&mc.PlaceStackRequestAction{Count: 32}, s.slot, inv, 1, mc.ContainerTradeTwoIngredientOne, uiTradeInput),
				bdsTrade(s, 32, inv, 22),
			},
			knownSlots: map[int]string{
				3: "the session names the second input of a trade, which stays empty, BDS does not",
				5: "the session names the second input of a trade, which stays empty, BDS does not",
			},
		},
		{
			// The villager of the capture sold an emerald for 10 clay
			// balls first.
			capture: "merchant/preview",
			items:   map[int]item.Stack{0: item.NewStack(item.ClayBall{}, 10)},
			offers:  []map[string]any{bdsOffer(item.NewStack(item.ClayBall{}, 10), item.NewStack(item.Emerald{}, 1))},
			requests: []func() []mc.StackRequestAction{
				bdsPreview(s, 1, mc.ContainerOffhand, 1),
				bdsPreview(s, 1, inv, 21),
			},
		},
		{
			capture: "smithing/empty-slot",
			items: map[int]item.Stack{
				0: item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1),
				1: item.NewStack(item.NetheriteIngot{}, 2),
				2: item.NewStack(item.SmithingTemplate{Template: item.TemplateNetheriteUpgrade()}, 2),
				3: item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1),
				4: item.NewStack(item.SmithingTemplate{Template: item.TemplateCoast()}, 1),
				5: item.NewStack(item.IronIngot{}, 2),
			},
			menu:     &menu{cube.Pos{-2, -60, 0}, block.SmithingTable{}},
			statuses: true,
			requests: smithingEmptyRequests(s),
		},
	}
	if len(cases) != len(capture.Cases) {
		t.Fatalf("%d cases replayed of the %d captured", len(cases), len(capture.Cases))
	}
	for i, c := range cases {
		want := capture.Cases[i]
		if want.Name != c.capture || len(want.Requests) != len(c.requests) {
			t.Fatalf("case %d replays %s with %d requests, the capture has %s with %d", i, c.capture, len(c.requests), want.Name, len(want.Requests))
		}
		var diverging []string
		var villager *world.EntityHandle

		s.track(s.gather(func() {
			s.run(func(tx *world.Tx, p *player.Player) {
				p.Inventory().Clear()
				p.SetHeldItems(item.Stack{}, item.Stack{})
				p.Armour().Clear()
				_, _, _, ui, _ := s.sess.Inventories()
				ui.Clear()
				for slot, it := range c.items {
					if err := p.Inventory().SetItem(slot, it); err != nil {
						t.Fatal(err)
					}
				}
				if c.prepare != nil {
					c.prepare(tx, p)
				}
				if c.offers != nil {
					villager = bdsVillager(t, tx, p, c.offers)
				}
				if c.menu != nil {
					tx.SetBlock(c.menu.pos, c.menu.block, nil)
					if container, ok := tx.Block(c.menu.pos).(block.Container); ok {
						// The capture's dispenser and dropper hold three
						// iron ingots in slot 4.
						if err := container.Inventory(tx, c.menu.pos).SetItem(4, item.NewStack(item.IronIngot{}, 3)); err != nil {
							t.Fatal(err)
						}
					}
				}
			})
		}))

		var trade *mc.UpdateTradePacket
		if villager != nil {
			for _, pk := range s.track(s.gather(func() {
				s.send(&mc.InventoryTransactionPacket{TransactionData: &mc.UseItemOnEntityTransactionData{
					TargetEntityRuntimeID: s.runtimeID(villager),
					ActionType:            mc.UseItemOnEntityActionInteract,
					HotBarSlot:            emptyHand,
					Position:              mgl32.Vec3{0.5, -58.38, 0.5},
					ClickedPosition:       mgl32.Vec3{0, 1, 0},
				}})
			})) {
				if u, ok := pk.(*mc.UpdateTradePacket); ok {
					trade = u
				}
			}
			if trade == nil {
				t.Fatalf("%s: the trade screen of the villager did not open", c.capture)
			}
		}

		var opened *mc.ContainerOpenPacket
		if c.menu != nil {
			pos := c.menu.pos
			for _, pk := range s.track(s.gather(func() {
				s.send(&mc.InventoryTransactionPacket{TransactionData: &mc.UseItemTransactionData{
					ActionType:       mc.UseItemActionClickBlock,
					TriggerType:      mc.TriggerTypePlayerInput,
					BlockPosition:    [3]int32{int32(pos[0]), int32(pos[1]), int32(pos[2])},
					BlockFace:        int32(cube.FaceUp),
					HotBarSlot:       emptyHand,
					Position:         mgl32.Vec3{0.5, -58.38, 0.5},
					ClickedPosition:  mgl32.Vec3{0.5, 1, 0.5},
					ClientPrediction: mc.ClientPredictionSuccess,
				}})
			})) {
				if o, ok := pk.(*mc.ContainerOpenPacket); ok {
					opened = o
				}
			}
			var bdsOpen *mc.ContainerOpenPacket
			for _, p := range want.Packets {
				if p.Type == "ContainerOpen" {
					bdsOpen = p.decode(t).(*mc.ContainerOpenPacket)
				}
			}
			switch {
			case opened == nil || bdsOpen == nil:
				t.Fatalf("%s: opened with %+v, BDS with %+v", c.capture, opened, bdsOpen)
			case opened.ContainerType != bdsOpen.ContainerType:
				diverging = append(diverging, fmt.Sprintf("opened as type %d, BDS's as %d", opened.ContainerType, bdsOpen.ContainerType))
			}
		}

		for j, build := range c.requests {
			// The actions are built first: a slot a request changed before is
			// named by its ID.
			id, actions := s.nextRequest, build()
			s.nextRequest -= 2
			var got *mc.ItemStackResponse
			pks := s.track(s.gather(func() {
				s.send(&mc.ItemStackRequestPacket{Requests: []mc.ItemStackRequest{{RequestID: id, Actions: actions}}})
			}))
			for _, pk := range pks {
				if r, ok := pk.(*mc.ItemStackResponsePacket); ok && len(r.Responses) == 1 && r.Responses[0].RequestID == id {
					got = &r.Responses[0]
				}
			}
			if got == nil {
				t.Fatalf("%s: request %d was not answered", c.capture, j)
			}
			bds := want.Requests[j]
			if (got.Status == mc.ItemStackResponseStatusOK) != (bds.Status == mc.ItemStackResponseStatusOK) ||
				c.statuses && got.Status != bds.Status {
				diverging = append(diverging, fmt.Sprintf("%q answered %d, BDS %d", bds.Label, got.Status, bds.Status))
				continue
			}
			var gotSlots, wantSlots []string
			for _, ci := range got.ContainerInfo {
				if ci.Container.ContainerID == mc.ContainerCreatedOutput {
					// The session names the created output a craft's
					// result was taken from, which BDS leaves out, as
					// TestAutoCraftMatchesBDS finds.
					continue
				}
				for _, si := range ci.SlotInfo {
					gotSlots = append(gotSlots, fmt.Sprintf("%d:%d*%d", ci.Container.ContainerID, si.Slot, si.Count))
				}
			}
			for _, ch := range bds.Changes {
				wantSlots = append(wantSlots, fmt.Sprintf("%d:%d*%d", ch.Container, ch.Slot, ch.Count))
			}
			slices.Sort(gotSlots)
			slices.Sort(wantSlots)
			reason, known := c.knownSlots[j]
			switch {
			case !slices.Equal(gotSlots, wantSlots) && known:
				t.Logf("%s: %q changed %v, BDS %v, as known (%s)", c.capture, bds.Label, gotSlots, wantSlots, reason)
			case !slices.Equal(gotSlots, wantSlots):
				diverging = append(diverging, fmt.Sprintf("%q changed %v, BDS %v", bds.Label, gotSlots, wantSlots))
			case known:
				t.Errorf("%s: %q now changes the slots BDS does: remove it from knownSlots (%s)", c.capture, bds.Label, reason)
			}
		}

		if opened != nil {
			s.track(s.gather(func() {
				s.send(&mc.ContainerClosePacket{WindowID: opened.WindowID, ContainerType: opened.ContainerType})
			}))
		}
		if trade != nil {
			s.track(s.gather(func() {
				s.send(&mc.ContainerClosePacket{WindowID: trade.WindowID, ContainerType: trade.WindowType})
			}))
			s.track(s.gather(func() {
				s.run(func(tx *world.Tx, _ *player.Player) {
					if e, ok := villager.Entity(tx); ok {
						tx.RemoveEntity(e)
					}
				})
			}))
		}

		known, isKnown := menuRequestsKnownDiverging[c.capture]
		switch {
		case isKnown && len(diverging) == 0:
			t.Errorf("%s now answers as BDS did: remove it from menuRequestsKnownDiverging (%s)", c.capture, known)
		case !isKnown && len(diverging) > 0:
			t.Errorf("%s differs from BDS: %s", c.capture, strings.Join(diverging, "; "))
		case isKnown:
			t.Logf("%s differs from BDS, as known (%s): %s", c.capture, known, strings.Join(diverging, "; "))
		}
	}
}

// bdsMove returns a request of one Place or Take action of the capture, a
// copy of a with the count it has, from slot from of container src to slot
// to of container dst, each named by the stack ID slotOf gives it.
func bdsMove(a mc.StackRequestAction, slotOf func(container uint8, slot int) mc.StackRequestSlotInfo, src uint8, from int, dst uint8, to int) func() []mc.StackRequestAction {
	return func() []mc.StackRequestAction {
		switch a := a.(type) {
		case *mc.PlaceStackRequestAction:
			return []mc.StackRequestAction{&mc.PlaceStackRequestAction{Count: a.Count, Source: slotOf(src, from), Destination: slotOf(dst, to)}}
		case *mc.TakeStackRequestAction:
			return []mc.StackRequestAction{&mc.TakeStackRequestAction{Count: a.Count, Source: slotOf(src, from), Destination: slotOf(dst, to)}}
		}
		panic(fmt.Sprintf("move of %T", a))
	}
}

// bdsSwap returns a request of one Swap action of the capture, of slot a of
// container ca with slot b of container cb.
func bdsSwap(s *scenario, ca uint8, a int, cb uint8, b int) func() []mc.StackRequestAction {
	return func() []mc.StackRequestAction {
		return []mc.StackRequestAction{&mc.SwapStackRequestAction{Source: s.slot(ca, a), Destination: s.slot(cb, b)}}
	}
}

// bdsOffHandItems are the items the offhand/items case of the capture puts
// in the off hand one by one, from inventory slot 9 on, and whether BDS took
// each.
var bdsOffHandItems = []struct {
	it    world.Item
	takes bool
}{
	{item.Arrow{}, true}, {item.Shield{}, true}, {item.Totem{}, true}, {item.Firework{}, true}, {item.NautilusShell{}, true},
	{item.EmptyMap{}, false}, {item.FilledMap{}, true}, {item.Compass{}, false}, {block.Torch{}, false},
}

// offHandItems returns the items of the offhand/items case of the capture,
// by slot: two apples, and bdsOffHandItems from slot 9.
func offHandItems() map[int]item.Stack {
	items := map[int]item.Stack{0: item.NewStack(item.Apple{}, 2)}
	for i, it := range bdsOffHandItems {
		items[9+i] = item.NewStack(it.it, 1)
	}
	return items
}

// offHandEach returns the requests of the offhand/items case that put each
// of bdsOffHandItems in the off hand, and take back those BDS took.
func offHandEach(s *scenario) []func() []mc.StackRequestAction {
	var out []func() []mc.StackRequestAction
	for i, it := range bdsOffHandItems {
		out = append(out, bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, mc.ContainerCombinedHotBarAndInventory, 9+i, mc.ContainerOffhand, 1))
		if it.takes {
			out = append(out, bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, mc.ContainerOffhand, 1, mc.ContainerCombinedHotBarAndInventory, 9+i))
		}
	}
	return out
}

// stickCraft returns the request of the capture that crafts four sticks of
// the planks of the first and third slot of the grid of the inventory and
// places them in the slot passed.
func stickCraft(s *scenario, container uint8, slot int) func() []mc.StackRequestAction {
	return func() []mc.StackRequestAction {
		return []mc.StackRequestAction{
			&mc.CraftRecipeStackRequestAction{RecipeNetworkID: s.recipe("crafting_table", item.Stick{}, "planks"), NumberOfCrafts: 1},
			&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerCraftingInput, uiInventoryGrid)},
			&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerCraftingInput, uiInventoryGrid+2)},
			&mc.PlaceStackRequestAction{Count: 4, Source: s.created(), Destination: s.slot(container, slot)},
		}
	}
}

// bdsOffer returns an offer of a villager of the capture: sell for buy, as
// many times as a novice offer may be used, priced by no demand.
func bdsOffer(buy, sell item.Stack) map[string]any {
	return map[string]any{
		"buy": item.WriteNBT(buy), "sell": item.WriteNBT(sell),
		"uses": int32(0), "maxUses": int32(16), "xp": int32(0), "rewardExp": byte(0),
		"demand": int32(0), "priceMultiplier": float32(0.05),
	}
}

// bdsVillager adds a farmer of the offers passed in front of the player,
// which stays where it stands.
func bdsVillager(t *testing.T, tx *world.Tx, p *player.Player, offers []map[string]any) *world.EntityHandle {
	t.Helper()
	recipes := make([]any, len(offers))
	for i, o := range offers {
		recipes[i] = o
	}
	h, err := entity.NewFromStructureNBT(map[string]any{
		"id":           "minecraft:villager",
		"NoAI":         byte(1),
		"VillagerData": map[string]any{"type": "minecraft:plains", "profession": "minecraft:farmer", "level": int32(1)},
		"Offers":       map[string]any{"Recipes": recipes},
	}, world.EntitySpawnOpts{Position: p.Position().Add(mgl64.Vec3{0, 0, -2}), SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	tx.AddEntity(h)
	return h
}

// bdsTrade returns the request of the capture that makes the first offer of
// the villager with the price paid from the first input, and places its
// result, one item, in the slot passed.
func bdsTrade(s *scenario, price int, container uint8, slot int) func() []mc.StackRequestAction {
	return func() []mc.StackRequestAction {
		return []mc.StackRequestAction{
			&mc.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
			&mc.ConsumeStackRequestAction{Count: uint8(price), Source: s.slot(mc.ContainerTradeTwoIngredientOne, uiTradeInput)},
			&mc.PlaceStackRequestAction{Count: 1, Source: s.created(), Destination: s.slot(container, slot)},
		}
	}
}

// bdsPreview returns the request of the capture that takes count items of
// the result of the first offer of the villager from the preview of the
// trade screen into the slot passed.
func bdsPreview(s *scenario, count uint8, container uint8, slot int) func() []mc.StackRequestAction {
	return func() []mc.StackRequestAction {
		return []mc.StackRequestAction{&mc.TakeStackRequestAction{Count: count,
			Source:      mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: mc.ContainerTradeTwoResultPreview}},
			Destination: s.slot(container, slot)}}
	}
}

// smithingEmptyRequests returns the requests of the smithing/empty-slot case
// of the capture: crafts at a smithing table with its slots empty in each
// way, and with all three filled, with the moves of items in and out of it
// between them.
func smithingEmptyRequests(s *scenario) []func() []mc.StackRequestAction {
	const (
		inv      = mc.ContainerCombinedHotBarAndInventory
		input    = mc.ContainerSmithingTableInput
		material = mc.ContainerSmithingTableMaterial
		template = mc.ContainerSmithingTableTemplate
	)
	slots := map[uint8]int{input: uiSmithingInput, material: uiSmithingMaterial, template: uiSmithingTemplate}
	put := func(from int, c uint8) func() []mc.StackRequestAction {
		return bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, inv, from, c, slots[c])
	}
	takeOut := func(c uint8, to int) func() []mc.StackRequestAction {
		return bdsMove(&mc.PlaceStackRequestAction{Count: 1}, s.slot, c, slots[c], inv, to)
	}
	sword := func() uint32 { return s.smithingRecipe(item.Sword{Tier: item.ToolTierNetherite}) }
	trim := func() uint32 { return s.trimRecipe(item.SmithingTemplate{Template: item.TemplateCoast()}) }
	craft := func(recipe func() uint32, to int, consumed ...uint8) func() []mc.StackRequestAction {
		return func() []mc.StackRequestAction {
			actions := []mc.StackRequestAction{&mc.CraftRecipeStackRequestAction{RecipeNetworkID: recipe(), NumberOfCrafts: 1}}
			for _, c := range consumed {
				actions = append(actions, &mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(c, slots[c])})
			}
			return append(actions, &mc.PlaceStackRequestAction{Count: 1, Source: s.created(), Destination: s.slot(inv, to)})
		}
	}
	all := []uint8{input, material, template}
	return []func() []mc.StackRequestAction{
		craft(sword, 10, all...),
		put(0, input),
		craft(sword, 10, all...),
		put(2, template),
		craft(sword, 10, input, template),
		craft(sword, 10, all...),
		craft(sword, 10),
		put(1, material),
		takeOut(template, 2),
		craft(sword, 10, input, material),
		craft(sword, 10, all...),
		put(2, template),
		takeOut(input, 0),
		craft(sword, 10, material, template),
		craft(sword, 10, all...),
		takeOut(template, 2),
		craft(sword, 10, all...),
		takeOut(material, 1),
		put(2, template),
		craft(sword, 10, all...),
		takeOut(template, 2),
		put(3, input),
		put(4, template),
		craft(trim, 11, input, template),
		craft(trim, 11, all...),
		put(5, material),
		craft(trim, 11, all...),
		put(0, input),
		put(1, material),
		put(2, template),
		craft(sword, 10, all...),
	}
}

// loomCraft returns the request of the capture that puts a border on a
// banner in a loom.
func loomCraft(s *scenario) func() []mc.StackRequestAction {
	return func() []mc.StackRequestAction {
		return []mc.StackRequestAction{
			&mc.CraftLoomRecipeStackRequestAction{Pattern: "bo", TimesCrafted: 1},
			&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerLoomInput, uiLoomInput)},
			&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerLoomDye, uiLoomDye)},
			&mc.PlaceStackRequestAction{Count: 1, Source: s.again(mc.ContainerCreatedOutput, uiCreatedOutput), Destination: s.slot(mc.ContainerCombinedHotBarAndInventory, 7)},
		}
	}
}

// levelEntityPlace returns the request of the capture that puts an apple of
// inventory slot 0 in container 7, the container of the block opened, at
// the slot passed.
func levelEntityPlace(s *scenario, slot int) func() []mc.StackRequestAction {
	return func() []mc.StackRequestAction {
		return []mc.StackRequestAction{&mc.PlaceStackRequestAction{Count: 1,
			Source:      s.slot(mc.ContainerCombinedHotBarAndInventory, 0),
			Destination: s.slot(mc.ContainerLevelEntity, slot)}}
	}
}

// track keeps the stack network IDs and the window of the packets passed,
// as the record of a scenario does, and returns them.
func (s *scenario) track(pks []proto.Packet) []proto.Packet {
	for _, pk := range pks {
		s.trackStacks(pk)
	}
	return pks
}
