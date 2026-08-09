package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestGameTypeFromMode(t *testing.T) {
	tests := map[string]struct {
		mode world.GameMode
		want int32
	}{
		"survival":  {world.GameModeSurvival, packet.GameTypeSurvival},
		"creative":  {world.GameModeCreative, packet.GameTypeCreative},
		"spectator": {world.GameModeSpectator, packet.GameTypeSpectator},
		"fallback":  {world.GameModeAdventure, packet.GameTypeSurvival},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := GameTypeFromMode(test.mode); got != test.want {
				t.Fatalf("game type = %d, want %d", got, test.want)
			}
		})
	}
}

func TestResyncInventory(t *testing.T) {
	s := &Session{
		br:      world.DefaultBlockRegistry,
		inv:     inventory.New(36, nil),
		offHand: inventory.New(1, nil),
		packets: make(chan packet.Packet, 2),
	}
	s.ResyncInventory()

	for _, want := range []uint32{protocol.WindowIDInventory, protocol.WindowIDOffHand} {
		pk := <-s.packets
		content, ok := pk.(*packet.InventoryContent)
		if !ok {
			t.Fatalf("packet type = %T, want *packet.InventoryContent", pk)
		}
		if content.WindowID != want {
			t.Errorf("window ID = %v, want %v", content.WindowID, want)
		}
	}
}

func TestSendGameModeResendsAbilitiesAfterDelay(t *testing.T) {
	s, c, w := abilityTestSession(t, 10*time.Millisecond)
	var task *world.Task
	if err := w.Do(func(*world.Tx) {
		s.SendGameMode(c)
		task = abilityResendTask(s)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("send game mode: %v", err)
	}

	pk := <-s.packets
	gameType, ok := pk.(*packet.SetPlayerGameType)
	if !ok {
		t.Fatalf("packet type = %T, want *packet.SetPlayerGameType", pk)
	}
	if gameType.GameType != packet.GameTypeCreative {
		t.Fatalf("game type = %d, want %d", gameType.GameType, packet.GameTypeCreative)
	}
	select {
	case pk := <-s.packets:
		t.Fatalf("ability packet sent without delay: %T", pk)
	default:
	}
	if err := task.Wait(context.Background()); err != nil {
		t.Fatalf("resend abilities: %v", err)
	}

	pk = <-s.packets
	abilities, ok := pk.(*packet.UpdateAbilities)
	if !ok {
		t.Fatalf("packet type = %T, want *packet.UpdateAbilities", pk)
	}
	want := uint32(protocol.AbilityMayFly | protocol.AbilityInvulnerable | protocol.AbilityInstantBuild | protocol.AbilityBuild | protocol.AbilityMine | protocol.AbilityDoorsAndSwitches | protocol.AbilityOpenContainers | protocol.AbilityAttackPlayers | protocol.AbilityAttackMobs)
	if got := abilities.AbilityData.Layers[0].Values; got != want {
		t.Fatalf("ability values = %b, want %b", got, want)
	}
}

func TestCloseConnectionCancelsDelayedAbilityResend(t *testing.T) {
	s, c, w := abilityTestSession(t, time.Hour)
	var task *world.Task
	if err := w.Do(func(*world.Tx) {
		s.SendGameMode(c)
		task = abilityResendTask(s)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("send game mode: %v", err)
	}
	<-s.packets // SetPlayerGameType.

	s.CloseConnection()
	s.abilityResendMu.Lock()
	resend := s.abilityResend
	s.abilityResendMu.Unlock()
	if resend != nil {
		t.Fatal("closed session retained delayed ability task")
	}
	if err := task.Wait(context.Background()); !errors.Is(err, world.ErrTaskCancelled) {
		t.Fatalf("delayed ability resend error = %v, want %v", err, world.ErrTaskCancelled)
	}
	select {
	case pk := <-s.packets:
		t.Fatalf("ability packet sent after close: %T", pk)
	default:
	}
}

func TestNewAbilityResendCancelsStaleTask(t *testing.T) {
	s, c, w := abilityTestSession(t, time.Hour)
	var first, second *world.Task
	if err := w.Do(func(*world.Tx) {
		s.SendGameMode(c)
		first = abilityResendTask(s)
		s.SendGameMode(c)
		second = abilityResendTask(s)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("send game modes: %v", err)
	}
	if first == second {
		t.Fatal("replacement reused stale ability task")
	}
	if err := first.Wait(context.Background()); !errors.Is(err, world.ErrTaskCancelled) {
		t.Fatalf("stale ability resend error = %v, want %v", err, world.ErrTaskCancelled)
	}
}

func TestSpectatorAbilityResendDoesNotDeadlockStartFlying(t *testing.T) {
	s, c, w := abilityTestSession(t, time.Millisecond)
	c.mode = world.GameModeSpectator
	c.session = s
	c.startedFlying = make(chan struct{})

	var task *world.Task
	if err := w.Do(func(*world.Tx) {
		s.SendGameMode(c)
		task = abilityResendTask(s)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("send game mode: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := task.Wait(ctx); err != nil {
		t.Fatalf("spectator ability resend: %v", err)
	}
	select {
	case <-c.startedFlying:
	case <-ctx.Done():
		t.Fatal("StartFlying re-entry deadlocked delayed ability resend")
	}
}

func abilityResendTask(s *Session) *world.Task {
	s.abilityResendMu.Lock()
	defer s.abilityResendMu.Unlock()
	return s.abilityResend
}

func abilityTestSession(t *testing.T, delay time.Duration) (*Session, *abilityTestControllable, *world.World) {
	t.Helper()
	s := &Session{
		conn:               abilityTestConn{},
		packets:            make(chan packet.Packet, 16),
		closeBackground:    make(chan struct{}),
		abilityResendDelay: delay,
	}
	c := &abilityTestControllable{mode: world.GameModeCreative}
	c.handle = world.EntitySpawnOpts{}.New(abilityTestType{c: c}, abilityTestConfig{})
	s.ent = c.handle
	s.entityRuntimeIDs = map[*world.EntityHandle]uint64{c.handle: selfEntityRuntimeID}
	s.entities = map[uint64]*world.EntityHandle{selfEntityRuntimeID: c.handle}
	w := world.Config{Synchronous: true}.New()
	if err := w.Do(func(tx *world.Tx) { tx.AddEntity(c.handle) }).Wait(context.Background()); err != nil {
		t.Fatalf("add controllable: %v", err)
	}
	t.Cleanup(func() {
		s.CloseConnection()
		_ = w.Close()
	})
	return s, c, w
}

type abilityTestControllable struct {
	Controllable
	handle        *world.EntityHandle
	mode          world.GameMode
	session       *Session
	startedFlying chan struct{}
	startFlying   sync.Once
}

func (c *abilityTestControllable) Close() error               { return nil }
func (c *abilityTestControllable) H() *world.EntityHandle     { return c.handle }
func (c *abilityTestControllable) Position() mgl64.Vec3       { return mgl64.Vec3{} }
func (c *abilityTestControllable) Rotation() cube.Rotation    { return cube.Rotation{} }
func (c *abilityTestControllable) GameMode() world.GameMode   { return c.mode }
func (*abilityTestControllable) Flying() bool                 { return false }
func (*abilityTestControllable) FlightSpeed() float64         { return 0.05 }
func (*abilityTestControllable) VerticalFlightSpeed() float64 { return 0.05 }
func (c *abilityTestControllable) StartFlying() {
	c.startFlying.Do(func() {
		c.session.SendGameMode(c)
		close(c.startedFlying)
	})
}

type abilityTestType struct{ c *abilityTestControllable }

func (t abilityTestType) Open(*world.Tx, *world.EntityHandle, *world.EntityData) world.Entity {
	return t.c
}
func (abilityTestType) EncodeEntity() string                        { return "test:abilities" }
func (abilityTestType) BBox(world.Entity) cube.BBox                 { return cube.BBox{} }
func (abilityTestType) DecodeNBT(map[string]any, *world.EntityData) {}
func (abilityTestType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type abilityTestConfig struct{}

func (abilityTestConfig) Apply(*world.EntityData) {}

type abilityTestConn struct{ Conn }

func (abilityTestConn) Close() error { return nil }

func TestSendRecipesExposesSpecialRecipes(t *testing.T) {
	id := uuid.MustParse("442d85ed-8272-4543-a6f1-418f90ded05d")
	stick, apple := item.NewStack(item.Stick{}, 1), item.NewStack(item.Apple{}, 1)
	recipe.Register(recipe.NewShaped([]recipe.Item{stick}, apple, recipe.Shape{1, 1}, "crafting_table"))
	recipe.Register(recipe.NewUserDataShapeless([]recipe.Item{stick}, apple, "crafting_table"))
	recipe.Register(recipe.NewMulti(id))

	s := &Session{
		br:              world.DefaultBlockRegistry,
		recipes:         make(map[uint32]recipe.Recipe),
		packets:         make(chan packet.Packet, 1),
		closeBackground: make(chan struct{}),
	}
	s.sendRecipes()
	raw := <-s.packets
	pk, ok := raw.(*packet.CraftingData)
	if !ok {
		t.Fatalf("packet type = %T, want *packet.CraftingData", raw)
	}
	if len(pk.ShapedRecipes) == 0 || !pk.ShapedRecipes[len(pk.ShapedRecipes)-1].AssumeSymmetry {
		t.Fatal("shaped recipe is not visible through symmetry metadata")
	}
	if len(pk.ShulkerBoxRecipes) == 0 {
		t.Fatal("user-data shapeless recipe missing from CraftingData")
	}
	if len(pk.MultiRecipes) == 0 || pk.MultiRecipes[len(pk.MultiRecipes)-1].UUID != id {
		t.Fatalf("multi recipes = %#v, want UUID %s", pk.MultiRecipes, id)
	}
}

func TestCraftingAcceptsUserDataShapelessRecipes(t *testing.T) {
	s := &Session{recipes: map[uint32]recipe.Recipe{
		1: recipe.NewUserDataShapeless(nil, item.Stack{}, "crafting_table"),
	}}
	h := &ItemStackRequestHandler{}
	const want = "times crafted must be at least 1"
	if err := h.handleCraft(&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 1}, s, nil); err == nil || err.Error() != want {
		t.Fatalf("normal craft error = %v, want %q", err, want)
	}
	if err := h.handleAutoCraft(&protocol.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 1}, s, nil); err == nil || err.Error() != want {
		t.Fatalf("auto craft error = %v, want %q", err, want)
	}
}

func TestMultiRecipeCraftsServerValidatedResult(t *testing.T) {
	s := craftingTestSession()
	first := item.NewStack(item.Bow{}, 1).WithDurability(10)
	second := item.NewStack(item.Bow{}, 1).WithDurability(20)
	_ = s.ui.SetItem(craftingGridSmallOffset, first)
	_ = s.ui.SetItem(craftingGridSmallOffset+1, second)
	s.recipes[1] = recipe.NewMulti(uuid.MustParse("00000000-0000-0000-0000-000000000001"))

	h := craftingTestHandler()
	err := h.handleRequest(protocol.ItemStackRequest{RequestID: -1, Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
		&protocol.CraftResultsDeprecatedStackRequestAction{TimesCrafted: 1, ResultItems: []protocol.StackRequestItem{{Identifier: "minecraft:bow", Count: 1}}},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: craftingSlot(craftingGridSmallOffset, item_id(first))}},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: craftingSlot(craftingGridSmallOffset+1, item_id(second))}},
	}}, s, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, _ := s.ui.Item(craftingResult)
	if _, ok := result.Item().(item.Bow); !ok || result.Durability() != 49 {
		t.Fatalf("multi repair result = %v, want bow with 49 durability", result)
	}
	for _, slot := range []int{craftingGridSmallOffset, craftingGridSmallOffset + 1} {
		if input, _ := s.ui.Item(slot); !input.Empty() {
			t.Fatalf("multi recipe input slot %d was not consumed", slot)
		}
	}
}

func TestUserDataShapelessPreservesServerSourceNBT(t *testing.T) {
	s := craftingTestSession()
	sourceBox := block.NewShulkerBox()
	sourceBox.Type = block.RedShulkerBox()
	_ = sourceBox.Inventory(nil, cube.Pos{}).SetItem(0, item.NewStack(item.Apple{}, 3))
	source := item.NewStack(sourceBox, 1).WithCustomName("Supplies")
	dye := item.NewStack(item.Dye{Colour: item.ColourBlue()}, 1)
	_ = s.ui.SetItem(craftingGridSmallOffset, source)
	_ = s.ui.SetItem(craftingGridSmallOffset+1, dye)

	expectedBox := block.NewShulkerBox()
	expectedBox.Type = block.RedShulkerBox()
	outputBox := block.NewShulkerBox()
	outputBox.Type = block.BlueShulkerBox()
	s.recipes[1] = recipe.NewUserDataShapeless([]recipe.Item{item.NewStack(expectedBox, 1), dye}, item.NewStack(outputBox, 1), "crafting_table")

	h := craftingTestHandler()
	err := h.handleRequest(protocol.ItemStackRequest{RequestID: -1, Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
		&protocol.CraftResultsDeprecatedStackRequestAction{TimesCrafted: 1, ResultItems: []protocol.StackRequestItem{{
			Identifier: "minecraft:blue_shulker_box",
			Count:      1,
			NBTData:    map[string]any{"CustomName": "injected"},
		}}},
	}}, s, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, _ := s.ui.Item(craftingResult)
	box, ok := result.Item().(block.ShulkerBox)
	if !ok {
		t.Fatalf("result item = %T, want block.ShulkerBox", result.Item())
	}
	if name, _ := box.EncodeItem(); name != "minecraft:blue_shulker_box" {
		t.Fatalf("result item = %s, want blue shulker box", name)
	}
	contents, _ := box.Inventory(nil, cube.Pos{}).Item(0)
	if contents.Count() != 3 || result.CustomName() != "Supplies" {
		t.Fatalf("preserved result = %v with contents %v", result, contents)
	}
}

func TestMultiRecipeRejectsUntrustedConsumption(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		result   protocol.StackRequestItem
		input    []item.Stack
		consumes []int
	}{
		{
			name:     "unrelated payment",
			id:       repairMultiRecipe,
			result:   protocol.StackRequestItem{Identifier: "minecraft:bow", Count: 1},
			input:    []item.Stack{item.NewStack(item.Bow{}, 1), item.NewStack(item.Bow{}, 1), item.NewStack(item.Apple{}, 1)},
			consumes: []int{2},
		},
		{
			name:     "one of two repair inputs",
			id:       repairMultiRecipe,
			result:   protocol.StackRequestItem{Identifier: "minecraft:bow", Count: 1},
			input:    []item.Stack{item.NewStack(item.Bow{}, 1), item.NewStack(item.Bow{}, 1)},
			consumes: []int{0},
		},
		{
			name:     "retained valuable source",
			id:       repairMultiRecipe,
			result:   protocol.StackRequestItem{Identifier: "minecraft:bow", Count: 1},
			input:    []item.Stack{item.NewStack(item.Bow{}, 1).WithCustomName("valuable"), item.NewStack(item.Bow{}, 1), item.NewStack(item.Apple{}, 1)},
			consumes: []int{1, 2},
		},
		{
			name:     "duplicate slot",
			id:       repairMultiRecipe,
			result:   protocol.StackRequestItem{Identifier: "minecraft:bow", Count: 1},
			input:    []item.Stack{item.NewStack(item.Bow{}, 1), item.NewStack(item.Bow{}, 1)},
			consumes: []int{0, 0},
		},
		{
			name:     "arbitrary grid item",
			id:       repairMultiRecipe,
			result:   protocol.StackRequestItem{Identifier: "minecraft:bow", Count: 1},
			input:    []item.Stack{item.NewStack(item.Bow{}, 1), item.NewStack(item.Bow{}, 1), item.NewStack(item.Apple{}, 1)},
			consumes: []int{0, 1, 2},
		},
		{
			name:     "excessive output count",
			id:       repairMultiRecipe,
			result:   protocol.StackRequestItem{Identifier: "minecraft:bow", Count: 2},
			input:    []item.Stack{item.NewStack(item.Bow{}, 1), item.NewStack(item.Bow{}, 1)},
			consumes: []int{0, 1},
		},
		{
			name:     "unrestricted UUID output",
			id:       "00000000-0000-0000-0000-0000000000c8",
			result:   protocol.StackRequestItem{Identifier: "minecraft:diamond", Count: 64},
			input:    []item.Stack{item.NewStack(item.Apple{}, 1)},
			consumes: []int{0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := craftingTestSession()
			for slot, stack := range test.input {
				_ = s.ui.SetItem(craftingGridSmallOffset+slot, stack)
			}
			err := craftingTestHandler().handleRequest(multiCraftingRequest(s, test.id, test.result, test.consumes...), s, nil, nil)
			requireRejectedCraft(t, s, err, test.input)
		})
	}
	t.Run("excessive consumed count", func(t *testing.T) {
		s := craftingTestSession()
		input := []item.Stack{item.NewStack(item.Paper{}, 2), item.NewStack(item.Gunpowder{}, 1)}
		for slot, stack := range input {
			_ = s.ui.SetItem(craftingGridSmallOffset+slot, stack)
		}
		req := multiCraftingRequest(s, fireworkMultiRecipe, protocol.StackRequestItem{Identifier: "minecraft:firework_rocket", Count: 3}, 0, 1)
		req.Actions[2].(*protocol.ConsumeStackRequestAction).Count = 2
		err := craftingTestHandler().handleRequest(req, s, nil, nil)
		requireRejectedCraft(t, s, err, input)
	})
}

func TestUserDataShapelessRejectsExtraGridItem(t *testing.T) {
	s := craftingTestSession()
	box := block.NewShulkerBox()
	box.Type = block.RedShulkerBox()
	input := []item.Stack{
		item.NewStack(box, 1).WithCustomName("valuable"),
		item.NewStack(item.Dye{Colour: item.ColourBlue()}, 1),
		item.NewStack(item.Apple{}, 1),
	}
	for slot, stack := range input {
		_ = s.ui.SetItem(craftingGridSmallOffset+slot, stack)
	}
	output := block.NewShulkerBox()
	output.Type = block.BlueShulkerBox()
	s.recipes[1] = recipe.NewUserDataShapeless([]recipe.Item{item.NewStack(box, 1), input[1]}, item.NewStack(output, 1), "crafting_table")

	err := craftingTestHandler().handleRequest(protocol.ItemStackRequest{RequestID: -1, Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
	}}, s, nil, nil)
	requireRejectedCraft(t, s, err, input)
}

func TestMultiRecipeValidRepresentatives(t *testing.T) {
	t.Run("firework", func(t *testing.T) {
		s := craftingTestSession()
		input := []item.Stack{
			item.NewStack(item.Paper{}, 1),
			item.NewStack(item.Gunpowder{}, 1),
			item.NewStack(item.FireworkStar{}, 1),
		}
		result := craftValidMulti(t, s, fireworkMultiRecipe, protocol.StackRequestItem{
			Identifier: "minecraft:firework_rocket",
			Count:      3,
			NBTData:    map[string]any{"Fireworks": map[string]any{"Flight": uint8(3)}},
		}, input)
		firework, ok := result.Item().(item.Firework)
		if !ok || result.Count() != 3 || firework.Duration != time.Second || len(firework.Explosions) != 1 {
			t.Fatalf("firework result = %#v x%d", result.Item(), result.Count())
		}
	})
	t.Run("map clone", func(t *testing.T) {
		s := craftingTestSession()
		input := []item.Stack{
			item.NewStack(craftingNamedItem("minecraft:filled_map"), 1).WithCustomName("trusted map"),
			item.NewStack(craftingNamedItem("minecraft:empty_map"), 1),
		}
		result := craftValidMulti(t, s, mapCloningCartographyMultiRecipe, protocol.StackRequestItem{Identifier: "minecraft:filled_map", Count: 2}, input)
		if result.Count() != 2 || result.CustomName() != "trusted map" {
			t.Fatalf("map clone result = %v", result)
		}
	})
	t.Run("book clone", func(t *testing.T) {
		s := craftingTestSession()
		input := []item.Stack{
			item.NewStack(item.WrittenBook{Title: "Trusted", Author: "Server", Pages: []string{"page"}, Generation: item.OriginalGeneration()}, 1),
			item.NewStack(item.BookAndQuill{}, 1),
		}
		result := craftValidMulti(t, s, bookCloningMultiRecipe, protocol.StackRequestItem{Identifier: "minecraft:written_book", Count: 2}, input)
		book, ok := result.Item().(item.WrittenBook)
		if !ok || result.Count() != 2 || book.Title != "Trusted" || book.Generation.Uint8() != 1 {
			t.Fatalf("book clone result = %#v x%d", result.Item(), result.Count())
		}
	})
	t.Run("banner duplicate", func(t *testing.T) {
		s := craftingTestSession()
		patterned := block.Banner{Colour: item.ColourRed(), Patterns: []block.BannerPatternLayer{{Type: block.BorderBannerPattern(), Colour: item.ColourBlack()}}}
		input := []item.Stack{item.NewStack(patterned, 1), item.NewStack(block.Banner{Colour: item.ColourRed()}, 1)}
		_, meta := patterned.EncodeItem()
		result := craftValidMulti(t, s, bannerDuplicateMultiRecipe, protocol.StackRequestItem{Identifier: "minecraft:banner", MetadataValue: uint32(meta), Count: 2}, input)
		banner, ok := result.Item().(block.Banner)
		if !ok || result.Count() != 2 || len(banner.Patterns) != 1 {
			t.Fatalf("banner duplicate result = %#v x%d", result.Item(), result.Count())
		}
	})
	t.Run("banner add pattern", func(t *testing.T) {
		s := craftingTestSession()
		base := block.Banner{Colour: item.ColourWhite()}
		input := []item.Stack{
			item.NewStack(base, 1),
			item.NewStack(item.Dye{Colour: item.ColourRed()}, 1),
			item.NewStack(item.BannerPattern{Type: item.CreeperBannerPattern()}, 1),
		}
		output := base
		output.Patterns = []block.BannerPatternLayer{{Type: block.CreeperBannerPattern(), Colour: item.ColourRed()}}
		_, meta := base.EncodeItem()
		result := craftValidMulti(t, s, bannerAddPatternMultiRecipe, protocol.StackRequestItem{
			Identifier:    "minecraft:banner",
			MetadataValue: uint32(meta),
			Count:         1,
			NBTData:       output.EncodeNBT(),
		}, input)
		banner, ok := result.Item().(block.Banner)
		if !ok || len(banner.Patterns) != 1 || banner.Patterns[0].Type != block.CreeperBannerPattern() {
			t.Fatalf("banner pattern result = %#v", result.Item())
		}
	})
}

type craftingNamedItem string

func (i craftingNamedItem) EncodeItem() (string, int16) { return string(i), 0 }

func craftValidMulti(t *testing.T, s *Session, id string, result protocol.StackRequestItem, input []item.Stack) item.Stack {
	t.Helper()
	for slot, stack := range input {
		_ = s.ui.SetItem(craftingGridSmallOffset+slot, stack)
	}
	consumes := make([]int, len(input))
	for slot := range consumes {
		consumes[slot] = slot
	}
	if err := craftingTestHandler().handleRequest(multiCraftingRequest(s, id, result, consumes...), s, nil, nil); err != nil {
		t.Fatal(err)
	}
	crafted, _ := s.ui.Item(craftingResult)
	return crafted
}

func multiCraftingRequest(s *Session, id string, result protocol.StackRequestItem, consumes ...int) protocol.ItemStackRequest {
	s.recipes[1] = recipe.NewMulti(uuid.MustParse(id))
	actions := []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
		&protocol.CraftResultsDeprecatedStackRequestAction{TimesCrafted: 1, ResultItems: []protocol.StackRequestItem{result}},
	}
	for _, inputSlot := range consumes {
		slot := craftingGridSmallOffset + inputSlot
		stack, _ := s.ui.Item(slot)
		actions = append(actions, &protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{
			Count:  1,
			Source: craftingSlot(slot, item_id(stack)),
		}})
	}
	return protocol.ItemStackRequest{RequestID: -1, Actions: actions}
}

func requireRejectedCraft(t *testing.T, s *Session, err error, input []item.Stack) {
	t.Helper()
	if err == nil {
		t.Fatal("craft succeeded, want rejection")
	}
	for slot, want := range input {
		got, _ := s.ui.Item(craftingGridSmallOffset + slot)
		if !got.Equal(want) {
			t.Fatalf("input slot %d = %v, want resynchronised %v", slot, got, want)
		}
	}
	if result, _ := s.ui.Item(craftingResult); !result.Empty() {
		t.Fatalf("rejected craft produced %v", result)
	}
	pk, ok := (<-s.packets).(*packet.ItemStackResponse)
	if !ok || len(pk.Responses) != 1 || pk.Responses[0].Status != protocol.ItemStackResponseStatusError {
		t.Fatalf("rejected craft response = %#v", pk)
	}
}

func craftingTestSession() *Session {
	return &Session{
		br:              world.DefaultBlockRegistry,
		recipes:         make(map[uint32]recipe.Recipe),
		ui:              inventory.New(51, nil),
		inv:             inventory.New(36, nil),
		offHand:         inventory.New(1, nil),
		packets:         make(chan packet.Packet, 4),
		closeBackground: make(chan struct{}),
	}
}

func craftingTestHandler() *ItemStackRequestHandler {
	return &ItemStackRequestHandler{
		changes:         make(map[byte]map[byte]changeInfo),
		responseChanges: make(map[int32]map[*inventory.Inventory]map[byte]responseChange),
	}
}

func craftingSlot(slot int, stackID int32) protocol.StackRequestSlotInfo {
	return protocol.StackRequestSlotInfo{
		Container:      protocol.FullContainerName{ContainerID: protocol.ContainerCraftingInput},
		Slot:           byte(slot),
		StackNetworkID: stackID,
	}
}
