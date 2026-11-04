package session

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/google/uuid"
)

// batchController is a player whose batches the differential tests begin
// over the inventories of a session: BeginMenu is all of Controllable that
// the handlers of crafts call.
type batchController struct {
	Controllable
	tx *world.Tx
	s  *Session
}

func (c batchController) GameMode() world.GameMode { return world.GameModeSurvival }
func (c batchController) BeginMenu(n menu.Namer) *menu.Tx {
	inv := menu.Inventories{Main: c.s.inv, OffHand: c.s.offHand, UI: c.s.ui, Armour: inventory.NewArmour(nil)}
	return menu.Begin(menu.Opts{Tx: c.tx, Owner: craftOwner{}, Inv: inv, Menu: c.s.menu.Load(), Name: n})
}

// multiCase is a special recipe crafted of a grid, or of the two inputs of
// a cartography table, which the client consumes some of, naming a result.
type multiCase struct {
	name, id string
	// screen is the screen the grid is of: the inventory, a crafting
	// table or a cartography table.
	screen   string
	input    []item.Stack
	result   item.Stack
	consumes []int
	// count is the count of each consume, 1 if 0; times is the times
	// crafted, 1 if 0.
	count, times int
	// maps is the map the world holds, if any.
	maps *world.MapData
}

// TestMultiCraftsMatchTheSession crafts the special recipes of the
// session's tests both ways: as the session's handlers craft them, and
// through the session with menu requests, where menu.MultiCraft keeps the
// snapshot, counts the consumption, derives and checks the result and
// changes the map. Each case runs in a world of its own, the same for both,
// and what each left must be the same: the verdict, the UI, the response
// and the maps of the world.
func TestMultiCraftsMatchTheSession(t *testing.T) {
	t.Parallel()
	bow := item.NewStack(item.Bow{}, 1)
	base := block.Banner{Colour: item.ColourWhite()}
	red := item.NewStack(item.Dye{Colour: item.ColourRed()}, 1)
	creeper := item.NewStack(item.BannerPattern{Type: item.CreeperBannerPattern()}, 1)
	patterned := func(p block.BannerPatternType) item.Stack {
		b := base
		b.Patterns = []block.BannerPatternLayer{{Type: p, Colour: item.ColourRed()}}
		return item.NewStack(b, 1)
	}
	border := make([]item.Stack, 9)
	for i := range border {
		border[i] = red
	}
	border[4] = item.NewStack(base, 1)
	paperAround := make([]item.Stack, 9)
	for i := range paperAround {
		paperAround[i] = item.NewStack(item.Paper{}, 1)
	}
	paperAround[4] = craftingMapStack()
	mapData := world.MapData{ID: 42, Scale: 1, Colors: make([]byte, 128*128)}
	original := item.WrittenBook{Title: "Trusted", Author: "Server", Pages: []string{"page"}, Generation: item.OriginalGeneration()}
	copied := original
	copied.Generation = item.CopyGeneration()
	duplicated := block.Banner{Colour: item.ColourRed(), Patterns: []block.BannerPatternLayer{{Type: block.BorderBannerPattern(), Colour: item.ColourBlack()}}}
	glass := item.NewStack(block.GlassPane{}, 1)
	repairOf := func(a, b int) item.Stack {
		return bow.WithDurability(min(bow.MaxDurability(), a+b+bow.MaxDurability()/20))
	}
	cases := []multiCase{
		{name: "repair", id: repairMultiRecipe, input: []item.Stack{bow.WithDurability(10), bow.WithDurability(20)}, result: repairOf(10, 20), consumes: []int{1, 0}},
		{name: "repair: unrelated payment", id: repairMultiRecipe, input: []item.Stack{bow, bow, item.NewStack(item.Apple{}, 1)}, result: bow, consumes: []int{2}},
		{name: "repair: one of two", id: repairMultiRecipe, input: []item.Stack{bow, bow}, result: bow, consumes: []int{0}},
		{name: "repair: a valuable source retained", id: repairMultiRecipe, input: []item.Stack{bow.WithCustomName("valuable"), bow, item.NewStack(item.Apple{}, 1)}, result: bow, consumes: []int{1, 2}},
		{name: "repair: a slot twice", id: repairMultiRecipe, input: []item.Stack{bow, bow}, result: bow, consumes: []int{0, 0}},
		{name: "repair: an apple too", id: repairMultiRecipe, input: []item.Stack{bow, bow, item.NewStack(item.Apple{}, 1)}, result: bow, consumes: []int{0, 1, 2}},
		{name: "repair: two bows named", id: repairMultiRecipe, input: []item.Stack{bow.WithDurability(10), bow.WithDurability(20)}, result: repairOf(10, 20).Grow(1), consumes: []int{0, 1}},
		{name: "repair: twice", id: repairMultiRecipe, times: 2, input: []item.Stack{bow, bow}, result: bow, consumes: []int{0, 1}},
		{name: "repair: none of a slot", id: repairMultiRecipe, count: -1, input: []item.Stack{bow, bow}, result: bow, consumes: []int{0, 1}},
		{name: "repair: of the table", id: repairMultiRecipe, screen: "table", input: []item.Stack{{}, {}, {}, bow.WithDurability(10), bow.WithDurability(20)}, result: repairOf(10, 20), consumes: []int{3, 4}},
		{name: "repair: of a locked bow", id: repairMultiRecipe, input: []item.Stack{bow.WithDurability(10).WithLockMode(item.LockInInventory), bow.WithDurability(20)}, result: repairOf(10, 20), consumes: []int{0, 1}},
		{name: "firework", id: fireworkMultiRecipe, input: []item.Stack{item.NewStack(item.Paper{}, 1), item.NewStack(item.Gunpowder{}, 1), item.NewStack(item.FireworkStar{}, 1)},
			result: item.NewStack(item.Firework{Duration: time.Second, Explosions: []item.FireworkExplosion{{}}}, 3), consumes: []int{0, 1, 2}},
		{name: "firework: two paper consumed", id: fireworkMultiRecipe, count: 2, input: []item.Stack{item.NewStack(item.Paper{}, 2), item.NewStack(item.Gunpowder{}, 2)},
			result: item.NewStack(item.Firework{Duration: time.Second, Explosions: []item.FireworkExplosion{}}, 3), consumes: []int{0, 1}},
		{name: "firework: twice", id: fireworkMultiRecipe, count: 2, times: 2, input: []item.Stack{item.NewStack(item.Paper{}, 2), item.NewStack(item.Gunpowder{}, 2)},
			result: item.NewStack(item.Firework{Duration: time.Second, Explosions: []item.FireworkExplosion{}}, 6), consumes: []int{0, 1}},
		{name: "map clone", id: mapCloningCartographyMultiRecipe, input: []item.Stack{craftingMapStack().WithCustomName("trusted map"), item.NewStack(item.EmptyMap{}, 1)},
			result: craftingMapStack().WithCustomName("trusted map").Grow(1), consumes: []int{0, 1}},
		{name: "book clone", id: bookCloningMultiRecipe, input: []item.Stack{item.NewStack(original, 1), item.NewStack(item.BookAndQuill{}, 1)}, result: item.NewStack(copied, 2), consumes: []int{0, 1}},
		{name: "banner duplicate", id: bannerDuplicateMultiRecipe, input: []item.Stack{item.NewStack(duplicated, 1), item.NewStack(block.Banner{Colour: item.ColourRed()}, 1)},
			result: item.NewStack(duplicated, 2), consumes: []int{0, 1}},
		{name: "banner duplicate: the blank first", id: bannerDuplicateMultiRecipe, input: []item.Stack{item.NewStack(base, 1), patterned(block.BorderBannerPattern())},
			result: patterned(block.BorderBannerPattern()).Grow(1), consumes: []int{0, 1}},
		{name: "banner pattern of a template", id: bannerAddPatternMultiRecipe, input: []item.Stack{item.NewStack(base, 1), red, creeper}, result: patterned(block.CreeperBannerPattern()), consumes: []int{0, 1, 2}},
		{name: "banner pattern of the grid", id: bannerAddPatternMultiRecipe, screen: "table", input: border, result: patterned(block.BorderBannerPattern()), consumes: []int{0, 1, 2, 3, 4, 5, 6, 7, 8}},
		{name: "banner pattern: an illager banner", id: bannerAddPatternMultiRecipe, input: []item.Stack{item.NewStack(block.Banner{Colour: item.ColourWhite(), Illager: true}, 1), red, creeper},
			result: patterned(block.CreeperBannerPattern()), consumes: []int{0, 1, 2}},
		{name: "banner pattern: no template", id: bannerAddPatternMultiRecipe, input: []item.Stack{item.NewStack(base, 1), red}, result: patterned(block.CreeperBannerPattern()), consumes: []int{0, 1}},
		{name: "banner pattern: the wrong template", id: bannerAddPatternMultiRecipe, input: []item.Stack{item.NewStack(base, 1), red, creeper}, result: patterned(block.SkullBannerPattern()), consumes: []int{0, 1, 2}},
		{name: "banner pattern: no arrangement", id: bannerAddPatternMultiRecipe, screen: "table", input: []item.Stack{item.NewStack(base, 1), red, red, {}, {}, {}, {}, {}, {}},
			result: patterned(block.StripeTopBannerPattern()), consumes: []int{0, 1, 2}},
		{name: "map extended in the grid", id: mapExtendingMultiRecipe, screen: "table", input: paperAround, result: craftingMapStack(), consumes: []int{0, 1, 2, 3, 4, 5, 6, 7, 8}, maps: &mapData},
		{name: "map extended at the cartography table", id: mapExtendingCartographyMultiRecipe, screen: "cartography", input: []item.Stack{craftingMapStack(), item.NewStack(item.Paper{}, 1)},
			result: craftingMapStack(), consumes: []int{0, 1}, maps: &mapData},
		{name: "map locked at the cartography table", id: mapLockingMultiRecipe, screen: "cartography", input: []item.Stack{craftingMapStack(), glass}, result: craftingMapStack(), consumes: []int{0, 1}, maps: &mapData},
		{name: "map locked into two", id: mapLockingMultiRecipe, screen: "cartography", input: []item.Stack{craftingMapStack(), glass}, result: craftingMapStack().Grow(1), consumes: []int{0, 1}, maps: &mapData},
		{name: "map locked of no map data", id: mapLockingMultiRecipe, screen: "cartography", input: []item.Stack{craftingMapStack(), glass}, result: craftingMapStack(), consumes: []int{0, 1}},
		{name: "repair at the cartography table", id: repairMultiRecipe, screen: "cartography", input: []item.Stack{bow, bow}, result: bow, consumes: []int{0, 1}},
		{name: "map locked in the grid", id: mapLockingMultiRecipe, input: []item.Stack{craftingMapStack(), glass}, result: craftingMapStack(), consumes: []int{0, 1}, maps: &mapData},
	}
	accepted := 0
	for _, c := range cases {
		old := craftMultiOnce(t, c, false)
		moved := craftMultiOnce(t, c, true)
		if old != moved {
			t.Errorf("%s:\nthe session: %s\nthrough the menu: %s", c.name, old, moved)
		}
		if strings.HasPrefix(old, "accepted true") {
			accepted++
		}
	}
	if accepted < 10 {
		t.Fatalf("%d cases of %d accepted", accepted, len(cases))
	}
}

// craftMultiOnce crafts c in a world of its own, through the session's
// handlers or with menu requests, and returns what it left.
func craftMultiOnce(t *testing.T, c multiCase, menuRequests bool) string {
	t.Helper()
	var w *world.World
	var provider *mapCraftingProvider
	if c.maps != nil {
		w, provider = mapCraftingWorld(t, *c.maps)
	} else {
		w = world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
		t.Cleanup(func() { _ = w.Close() })
	}
	s := craftingTestSession()
	s.sessionRequests = !menuRequests
	var outcome string
	<-w.Exec(func(tx *world.Tx) {
		offset := craftingGridSmallOffset
		container := []byte{v2193.ContainerCraftingInput}
		switch c.screen {
		case "table":
			tx.SetBlock(cube.Pos{}, block.CraftingTable{}, nil)
			s.menu.Store(menu.OpenBlock(tx, cube.Pos{}, s, nil))
			s.openedContainerID.Store(uint32(v2193.ContainerTypeWorkbench))
			offset = craftingGridLargeOffset
		case "cartography":
			tx.SetBlock(cube.Pos{}, block.CartographyTable{}, nil)
			s.menu.Store(menu.OpenBlock(tx, cube.Pos{}, s, nil))
			s.openedContainerID.Store(uint32(v2193.ContainerTypeCartography))
			offset = cartographyInputSlot
			container = []byte{v2193.ContainerCartographyInput, v2193.ContainerCartographyAdditional}
		}
		for i, it := range c.input {
			_ = s.ui.SetItem(offset+i, it)
		}
		s.recipes[1] = recipeOfUUID(c.id)
		times := max(c.times, 1)
		name, meta := c.result.Item().EncodeItem()
		actions := []v2193.StackRequestAction{
			&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: byte(times)},
			&v2193.CraftResultsDeprecatedStackRequestAction{TimesCrafted: byte(times), ResultItems: []v2193.StackRequestItem{{
				Identifier: name, MetadataValue: uint32(meta), Count: uint16(c.result.Count()), NBTData: networkItemNBT(c.result),
			}}},
		}
		count := c.count
		switch count {
		case 0:
			count = 1
		case -1:
			count = 0
		}
		for _, i := range c.consumes {
			stack, _ := s.ui.Item(offset + i)
			actions = append(actions, &v2193.ConsumeStackRequestAction{Count: byte(count), Source: v2193.StackRequestSlotInfo{
				Container: v2193.FullContainerName{ContainerID: container[min(i, len(container)-1)]}, Slot: byte(offset + i), StackNetworkID: item_id(stack),
			}})
		}
		h := craftingTestHandler()
		err := h.handleRequest(v2193.ItemStackRequest{RequestID: -1, Actions: actions}, s, tx, batchController{tx: tx, s: s})

		var b strings.Builder
		fmt.Fprintf(&b, "accepted %v\n", err == nil)
		for slot := cartographyInputSlot; slot <= craftingResult; slot++ {
			if it, _ := s.ui.Item(slot); !it.Empty() {
				fmt.Fprintf(&b, "ui %d: %d %v\n", slot, it.Count(), item.WriteNBT(it))
			}
		}
		for _, p := range s.heldPackets {
			if r, ok := p.(*v2193.ItemStackResponsePacket); ok {
				for _, resp := range r.Responses {
					var slots []string
					for _, info := range resp.ContainerInfo {
						for _, slot := range info.SlotInfo {
							slots = append(slots, fmt.Sprintf("%d:%d=%d", info.Container.ContainerID, slot.Slot, slot.Count))
						}
					}
					slices.Sort(slots)
					fmt.Fprintf(&b, "response %d %v\n", resp.Status, slots)
				}
			}
		}
		if provider != nil {
			provider.mu.Lock()
			ids := slices.SortedFunc(func(yield func(int64) bool) {
				for id := range provider.maps {
					if !yield(id) {
						return
					}
				}
			}, cmp.Compare[int64])
			for _, id := range ids {
				m := provider.maps[id]
				fmt.Fprintf(&b, "map %d: scale %d, locked %v, parent %d\n", id, m.Scale, m.Locked, m.ParentID)
			}
			provider.mu.Unlock()
		}
		outcome = b.String()
	})
	return outcome
}

// recipeOfUUID returns the multi recipe of the UUID passed.
func recipeOfUUID(id string) recipe.Multi {
	return recipe.NewMulti(uuid.MustParse(id))
}
