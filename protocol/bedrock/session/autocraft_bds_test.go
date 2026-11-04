package session

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// autoCraftCapture is the recipe-book crafts captured from BDS 1.26.52.3 in
// testdata/bedrock_1_26_52_autocraft.json: two oak planks make four sticks,
// with planks in the crafting grid, the inventory or both.
type autoCraftCapture struct {
	Version string `json:"version"`
	BuildID int    `json:"build_id"`
	Recipe  struct {
		NetworkID uint32 `json:"network_id"`
	} `json:"recipe"`
	Cases []autoCraftCase `json:"cases"`
}

type autoCraftCase struct {
	Name           string          `json:"name"`
	Before         []autoCraftSlot `json:"before"`
	Request        string          `json:"request"`
	Response       string          `json:"response"`
	ResponseSHA256 string          `json:"response_sha256"`
	Status         uint8           `json:"status"`
	Changes        []autoCraftSlot `json:"changes"`
}

type autoCraftSlot struct {
	Container byte  `json:"container"`
	Slot      byte  `json:"slot"`
	Count     int   `json:"count"`
	StackID   int32 `json:"stack_id"`
}

func loadAutoCraftCapture(t *testing.T) autoCraftCapture {
	t.Helper()
	b, err := os.ReadFile("testdata/bedrock_1_26_52_autocraft.json")
	if err != nil {
		t.Fatal(err)
	}
	var c autoCraftCapture
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if c.Version != "1.26.52.3" || c.BuildID != 51798919 || len(c.Cases) == 0 {
		t.Fatalf("capture of %s (%d) with %d cases", c.Version, c.BuildID, len(c.Cases))
	}
	return c
}

// decodeCaptured decodes a packet captured whole, header first.
func decodeCaptured(t *testing.T, s string) any {
	t.Helper()
	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	h, body, err := bedrock.ReadHeader(raw)
	if err != nil {
		t.Fatal(err)
	}
	pk := v2193.New(h.ID)
	r := wire.NewReader(body)
	pk.Decode(r)
	if r.Err() != nil || r.Len() != 0 {
		t.Fatalf("packet %d does not decode: %v, %d bytes left", h.ID, r.Err(), r.Len())
	}
	return pk
}

// stickRecipe returns the crafting-table recipe of four sticks from two
// planks, as the vanilla data has it.
func stickRecipe(t *testing.T) recipe.Recipe {
	t.Helper()
	planks := recipe.NewItemTag("minecraft:planks", 1)
	r := recipe.NewShaped([]recipe.Item{planks, planks}, item.NewStack(item.Stick{}, 4), recipe.Shape{1, 2}, "crafting_table")
	if name, _ := (block.Planks{Wood: block.OakWood()}).EncodeItem(); !planks.Contains(name) {
		t.Fatal("oak planks do not match the planks tag")
	}
	return r
}

// replayAutoCraft sends the request of a captured case to a session in the
// state BDS was in, and returns the error of the request and the slots of
// the inventory (container 12) and the crafting grid (container 13) after
// it. The stack network IDs of the request are BDS's: they are put in the
// place of the session's. The session handles the request in a transaction
// of w.
func replayAutoCraft(t *testing.T, craft recipe.Recipe, id uint32, c autoCraftCase, w *world.World) (error, map[[2]byte]int, *v2193.ItemStackResponsePacket) {
	t.Helper()
	s := craftingTestSession()
	screen, _, _ := strings.Cut(c.Name, "/")
	switch screen {
	case "inventory":
		s.invOpened = true
	case "table":
		openTestMenu(t, s, block.CraftingTable{})
		s.openedContainerID.Store(uint32(v2193.ContainerTypeWorkbench))
	case "crafter":
		// A menu open in a window of the type of a crafter: the screen
		// gate reads no more.
		openTestMenu(t, s, block.CraftingTable{})
		s.openedContainerID.Store(uint32(v2193.ContainerTypeCrafter))
	case "closed":
	default:
		t.Fatalf("%s: unknown screen", c.Name)
	}
	s.recipes[id] = craft

	ids := map[int32]int32{}
	for _, slot := range c.Before {
		stack := item.NewStack(block.Planks{Wood: block.OakWood()}, slot.Count)
		inv := s.inv
		if slot.Container == v2193.ContainerCraftingInput {
			inv = s.ui
		}
		if err := inv.SetItem(int(slot.Slot), stack); err != nil {
			t.Fatal(err)
		}
		placed, _ := inv.Item(int(slot.Slot))
		ids[slot.StackID] = item_id(placed)
	}
	pk := decodeCaptured(t, c.Request).(*v2193.ItemStackRequestPacket)
	if len(pk.Requests) != 1 {
		t.Fatalf("%s: %d requests", c.Name, len(pk.Requests))
	}
	req := pk.Requests[0]
	ours := func(info *v2193.StackRequestSlotInfo) {
		if info.StackNetworkID > 0 {
			info.StackNetworkID = ids[info.StackNetworkID]
		}
	}
	for _, a := range req.Actions {
		switch a := a.(type) {
		case *v2193.ConsumeStackRequestAction:
			ours(&a.Source)
		case *v2193.PlaceStackRequestAction:
			ours(&a.Source)
			ours(&a.Destination)
		case *v2193.TakeStackRequestAction:
			ours(&a.Source)
			ours(&a.Destination)
		}
	}

	h := craftingTestHandler()
	var err error
	<-w.Exec(func(tx *world.Tx) { err = h.handleRequest(req, s, tx, batchController{tx: tx, s: s}) })
	slots := map[[2]byte]int{}
	for slot, stack := range s.inv.Slots() {
		if !stack.Empty() {
			slots[[2]byte{v2193.ContainerCombinedHotBarAndInventory, byte(slot)}] = stack.Count()
		}
	}
	for slot := menu.UICraftingSmall; slot < menu.UICraftingLarge+9; slot++ {
		if stack, _ := s.ui.Item(slot); !stack.Empty() {
			slots[[2]byte{v2193.ContainerCraftingInput, byte(slot)}] = stack.Count()
		}
	}
	var resp *v2193.ItemStackResponsePacket
	for _, p := range s.heldPackets {
		if r, ok := p.(*v2193.ItemStackResponsePacket); ok {
			resp = r
		}
	}
	return err, slots, resp
}

// TestAutoCraftMatchesBDS replays the recipe-book crafts captured from BDS
// 1.26.52.3: the session takes the inputs from exactly the slots the
// Consume actions name, in the grid or the inventory, as BDS does, and
// refuses the requests BDS refuses, where menu.AutoCraft pays for the
// craft; the plain crafts in a crafting table run too.
func TestAutoCraftMatchesBDS(t *testing.T) {
	t.Parallel()
	capture := loadAutoCraftCapture(t)
	craft := stickRecipe(t)
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	for _, c := range capture.Cases {
		t.Run(c.Name, func(t *testing.T) { matchAutoCraft(t, capture, craft, c, w) })
	}
}

// matchAutoCraft replays a captured case in a transaction of w, and
// compares the outcome with BDS's.
func matchAutoCraft(t *testing.T, capture autoCraftCapture, craft recipe.Recipe, c autoCraftCase, w *world.World) {
	bds := decodeCaptured(t, c.Response).(*v2193.ItemStackResponsePacket)
	if len(bds.Responses) != 1 || bds.Responses[0].Status != c.Status {
		t.Fatalf("captured response does not decode to status %d", c.Status)
	}
	err, slots, resp := replayAutoCraft(t, craft, capture.Recipe.NetworkID, c, w)
	if accepted := c.Status == v2193.ItemStackResponseStatusOK; accepted != (err == nil) {
		t.Fatalf("BDS answered status %d, the session: %v", c.Status, err)
	}
	want := map[[2]byte]int{}
	for _, slot := range c.Before {
		want[[2]byte{slot.Container, slot.Slot}] = slot.Count
	}
	for _, change := range c.Changes {
		key := [2]byte{change.Container, change.Slot}
		if change.Count == 0 {
			delete(want, key)
		} else {
			want[key] = change.Count
		}
	}
	if fmt.Sprint(slots) != fmt.Sprint(want) {
		t.Fatalf("slots after the request: %v, BDS: %v", slots, want)
	}
	if err != nil {
		return
	}
	// The response names the slots BDS names, in the inventory and
	// the grid. The created output, which BDS leaves out, is not
	// compared.
	var got, expected []string
	for _, info := range resp.Responses[0].ContainerInfo {
		if info.Container.ContainerID == v2193.ContainerCreatedOutput {
			continue
		}
		for _, slot := range info.SlotInfo {
			got = append(got, fmt.Sprintf("%d:%d=%d", info.Container.ContainerID, slot.Slot, slot.Count))
		}
	}
	for _, change := range c.Changes {
		expected = append(expected, fmt.Sprintf("%d:%d=%d", change.Container, change.Slot, change.Count))
	}
	slices.Sort(got)
	slices.Sort(expected)
	if !slices.Equal(got, expected) {
		t.Fatalf("response changes %v, BDS: %v", got, expected)
	}
}

// TestAutoCraftOutcomeIsStable replays the captured crafts that have planks
// both in the grid and in the inventory many times: which slots pay for the
// craft does not depend on the order of a Go map.
func TestAutoCraftOutcomeIsStable(t *testing.T) {
	t.Parallel()
	capture := loadAutoCraftCapture(t)
	craft := stickRecipe(t)
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	runs := 0
	for _, c := range capture.Cases {
		if c.Status != v2193.ItemStackResponseStatusOK || len(c.Before) < 2 {
			continue
		}
		var first string
		for i := range 200 {
			err, slots, _ := replayAutoCraft(t, craft, capture.Recipe.NetworkID, c, w)
			if err != nil {
				t.Fatalf("%s, run %d: %v", c.Name, i, err)
			}
			if got := fmt.Sprint(slots); i == 0 {
				first = got
			} else if got != first {
				t.Fatalf("%s, run %d: slots %s, first run %s", c.Name, i, got, first)
			}
		}
		runs++
	}
	if runs == 0 {
		t.Fatal("no captured craft with planks in the grid and the inventory")
	}
}
