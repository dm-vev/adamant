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
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl32"
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
	"offhand/place-slot-0":              "the off hand is taken at slot 0, which BDS refuses (55): it holds the off hand at slot 1 only",
	"anvil/level-entity-place":          "container 7 of an anvil is a one-slot placeholder that takes an item, which then vanishes",
	"crafting_table/level-entity-place": "container 7 of a crafting table is a one-slot placeholder that takes an item, which then vanishes",
}

// TestMenuRequestsAgainstBDS replays the cases of the capture on a session:
// the same items in the same slots, the same blocks opened, and the same
// requests, named by the stack IDs this server gave. It compares whether
// each request was accepted and, when it was, the slots of the response;
// and the type a block's window opened with. BDS's own statuses of refusal
// are not compared: the session refuses with the generic error. The
// inventory screen, which BDS needs open for inventory requests, is not
// opened: the session does not track it.
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

	const inv = v2193.ContainerCombinedHotBarAndInventory
	stale := v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: inv}, Slot: 0, StackNetworkID: 9999}
	type menu struct {
		pos   cube.Pos
		block world.Block
	}
	cases := []struct {
		capture  string
		items    map[int]item.Stack
		menu     *menu
		requests []func() []v2193.StackRequestAction
	}{
		{
			capture: "inventory/slot-changed-twice-refused",
			items:   map[int]item.Stack{0: item.NewStack(item.Apple{}, 16), 2: item.NewStack(block.Planks{Wood: block.OakWood()}, 8)},
			requests: []func() []v2193.StackRequestAction{
				func() []v2193.StackRequestAction {
					return []v2193.StackRequestAction{
						&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: s.slot(inv, 20)},
						&v2193.PlaceStackRequestAction{Count: 1, Source: s.again(inv, 2), Destination: s.slot(inv, 21)},
						&v2193.PlaceStackRequestAction{Count: 1, Source: stale, Destination: s.slot(inv, 22)},
					}
				},
				func() []v2193.StackRequestAction {
					return []v2193.StackRequestAction{&v2193.TakeStackRequestAction{Count: 8, Source: s.slot(inv, 2), Destination: s.slot(v2193.ContainerCursor, uiCursor)}}
				},
				func() []v2193.StackRequestAction {
					return []v2193.StackRequestAction{&v2193.PlaceStackRequestAction{Count: 8, Source: s.slot(v2193.ContainerCursor, uiCursor), Destination: s.slot(inv, 2)}}
				},
			},
		},
		{
			capture: "offhand/place-slot-0",
			items:   map[int]item.Stack{3: item.NewStack(item.Shield{}, 1)},
			requests: []func() []v2193.StackRequestAction{
				func() []v2193.StackRequestAction {
					return []v2193.StackRequestAction{&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(v2193.ContainerOffhand, 0)}}
				},
			},
		},
		{
			capture: "offhand/place-slot-1",
			items:   map[int]item.Stack{3: item.NewStack(item.Shield{}, 1)},
			requests: []func() []v2193.StackRequestAction{
				func() []v2193.StackRequestAction {
					return []v2193.StackRequestAction{&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(v2193.ContainerOffhand, 1)}}
				},
			},
		},
		{
			capture: "offhand/changed-refused",
			items:   map[int]item.Stack{0: item.NewStack(item.Apple{}, 16), 3: item.NewStack(item.Shield{}, 1), 4: item.NewStack(item.Totem{}, 1)},
			requests: []func() []v2193.StackRequestAction{
				func() []v2193.StackRequestAction {
					return []v2193.StackRequestAction{
						&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(v2193.ContainerOffhand, 1)},
						&v2193.PlaceStackRequestAction{Count: 1, Source: stale, Destination: s.slot(inv, 22)},
					}
				},
				func() []v2193.StackRequestAction {
					return []v2193.StackRequestAction{
						&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(v2193.ContainerOffhand, 0)},
						&v2193.PlaceStackRequestAction{Count: 1, Source: stale, Destination: s.slot(inv, 22)},
					}
				},
				func() []v2193.StackRequestAction {
					return []v2193.StackRequestAction{&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 4), Destination: s.slot(v2193.ContainerOffhand, 1)}}
				},
			},
		},
		{
			capture: "loom/no-screen-open",
			items:   map[int]item.Stack{5: item.NewStack(block.Banner{Colour: item.ColourWhite()}, 1), 6: item.NewStack(item.Dye{Colour: item.ColourRed()}, 1)},
			requests: []func() []v2193.StackRequestAction{
				loomCraft(s), loomCraft(s),
				func() []v2193.StackRequestAction {
					return []v2193.StackRequestAction{&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 5), Destination: s.slot(v2193.ContainerLoomInput, uiLoomInput)}}
				},
			},
		},
		{
			capture:  "anvil/level-entity-place",
			items:    map[int]item.Stack{0: item.NewStack(item.Apple{}, 16)},
			menu:     &menu{cube.Pos{-2, -60, 0}, block.Anvil{Type: block.UndamagedAnvil()}},
			requests: []func() []v2193.StackRequestAction{levelEntityPlace(s, 0), levelEntityPlace(s, 1)},
		},
		{
			capture:  "crafting_table/level-entity-place",
			items:    map[int]item.Stack{0: item.NewStack(item.Apple{}, 16)},
			menu:     &menu{cube.Pos{-2, -60, 2}, block.CraftingTable{}},
			requests: []func() []v2193.StackRequestAction{levelEntityPlace(s, 0), levelEntityPlace(s, 1)},
		},
		{
			capture:  "dispenser/open",
			items:    map[int]item.Stack{0: item.NewStack(item.Apple{}, 16)},
			menu:     &menu{cube.Pos{2, -60, 0}, block.NewDispenser()},
			requests: []func() []v2193.StackRequestAction{levelEntityPlace(s, 0)},
		},
		{
			capture:  "dropper/open",
			items:    map[int]item.Stack{0: item.NewStack(item.Apple{}, 16)},
			menu:     &menu{cube.Pos{2, -60, 2}, block.NewDropper()},
			requests: []func() []v2193.StackRequestAction{levelEntityPlace(s, 0)},
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

		s.track(s.gather(func() {
			s.run(func(tx *world.Tx, p *player.Player) {
				p.Inventory().Clear()
				p.SetHeldItems(item.Stack{}, item.Stack{})
				for slot, it := range c.items {
					if err := p.Inventory().SetItem(slot, it); err != nil {
						t.Fatal(err)
					}
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

		var opened *v2193.ContainerOpenPacket
		if c.menu != nil {
			pos := c.menu.pos
			for _, pk := range s.track(s.gather(func() {
				s.send(&v2193.InventoryTransactionPacket{TransactionData: &v2193.UseItemTransactionData{
					ActionType:       v2193.UseItemActionClickBlock,
					TriggerType:      v2193.TriggerTypePlayerInput,
					BlockPosition:    [3]int32{int32(pos[0]), int32(pos[1]), int32(pos[2])},
					BlockFace:        int32(cube.FaceUp),
					HotBarSlot:       emptyHand,
					Position:         mgl32.Vec3{0.5, -58.38, 0.5},
					ClickedPosition:  mgl32.Vec3{0.5, 1, 0.5},
					ClientPrediction: v2193.ClientPredictionSuccess,
				}})
			})) {
				if o, ok := pk.(*v2193.ContainerOpenPacket); ok {
					opened = o
				}
			}
			var bdsOpen *v2193.ContainerOpenPacket
			for _, p := range want.Packets {
				if p.Type == "ContainerOpen" {
					bdsOpen = p.decode(t).(*v2193.ContainerOpenPacket)
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
			var got *v2193.ItemStackResponse
			pks := s.track(s.gather(func() {
				s.send(&v2193.ItemStackRequestPacket{Requests: []v2193.ItemStackRequest{{RequestID: id, Actions: actions}}})
			}))
			for _, pk := range pks {
				if r, ok := pk.(*v2193.ItemStackResponsePacket); ok && len(r.Responses) == 1 && r.Responses[0].RequestID == id {
					got = &r.Responses[0]
				}
			}
			if got == nil {
				t.Fatalf("%s: request %d was not answered", c.capture, j)
			}
			bds := want.Requests[j]
			if (got.Status == v2193.ItemStackResponseStatusOK) != (bds.Status == v2193.ItemStackResponseStatusOK) {
				diverging = append(diverging, fmt.Sprintf("%q answered %d, BDS %d", bds.Label, got.Status, bds.Status))
				continue
			}
			var gotSlots, wantSlots []string
			for _, ci := range got.ContainerInfo {
				for _, si := range ci.SlotInfo {
					gotSlots = append(gotSlots, fmt.Sprintf("%d:%d*%d", ci.Container.ContainerID, si.Slot, si.Count))
				}
			}
			for _, ch := range bds.Changes {
				wantSlots = append(wantSlots, fmt.Sprintf("%d:%d*%d", ch.Container, ch.Slot, ch.Count))
			}
			slices.Sort(gotSlots)
			slices.Sort(wantSlots)
			if !slices.Equal(gotSlots, wantSlots) {
				diverging = append(diverging, fmt.Sprintf("%q changed %v, BDS %v", bds.Label, gotSlots, wantSlots))
			}
		}

		if opened != nil {
			s.track(s.gather(func() {
				s.send(&v2193.ContainerClosePacket{WindowID: opened.WindowID, ContainerType: opened.ContainerType})
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

// loomCraft returns the request of the capture that puts a border on a
// banner in a loom.
func loomCraft(s *scenario) func() []v2193.StackRequestAction {
	return func() []v2193.StackRequestAction {
		return []v2193.StackRequestAction{
			&v2193.CraftLoomRecipeStackRequestAction{Pattern: "bo", TimesCrafted: 1},
			&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerLoomInput, uiLoomInput)},
			&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerLoomDye, uiLoomDye)},
			&v2193.PlaceStackRequestAction{Count: 1, Source: s.again(v2193.ContainerCreatedOutput, uiCreatedOutput), Destination: s.slot(v2193.ContainerCombinedHotBarAndInventory, 7)},
		}
	}
}

// levelEntityPlace returns the request of the capture that puts an apple of
// inventory slot 0 in container 7, the container of the block opened, at
// the slot passed.
func levelEntityPlace(s *scenario, slot int) func() []v2193.StackRequestAction {
	return func() []v2193.StackRequestAction {
		return []v2193.StackRequestAction{&v2193.PlaceStackRequestAction{Count: 1,
			Source:      s.slot(v2193.ContainerCombinedHotBarAndInventory, 0),
			Destination: s.slot(v2193.ContainerLevelEntity, slot)}}
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
