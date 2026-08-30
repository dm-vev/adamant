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
	"github.com/df-mc/dragonfly/server/item/enchantment"
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
	if len(pk.UserDataShapelessRecipes) == 0 {
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
	expected := item.NewStack(item.Bow{}, 1).WithDurability(49)
	err := h.handleRequest(protocol.ItemStackRequest{RequestID: -1, Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
		&protocol.CraftResultsDeprecatedStackRequestAction{TimesCrafted: 1, ResultItems: []protocol.StackRequestItem{multiResultItem(expected)}},
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
		expected := item.NewStack(item.Firework{Duration: time.Second, Explosions: []item.FireworkExplosion{{}}}, 3)
		result := craftValidMulti(t, s, fireworkMultiRecipe, multiResultItem(expected), input)
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
		expected := input[0].Grow(1)
		result := craftValidMulti(t, s, mapCloningCartographyMultiRecipe, multiResultItem(expected), input)
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
		expected := item.NewStack(item.WrittenBook{Title: "Trusted", Author: "Server", Pages: []string{"page"}, Generation: item.CopyGeneration()}, 2)
		result := craftValidMulti(t, s, bookCloningMultiRecipe, multiResultItem(expected), input)
		book, ok := result.Item().(item.WrittenBook)
		if !ok || result.Count() != 2 || book.Title != "Trusted" || book.Generation.Uint8() != 1 {
			t.Fatalf("book clone result = %#v x%d", result.Item(), result.Count())
		}
	})
	t.Run("banner duplicate", func(t *testing.T) {
		s := craftingTestSession()
		patterned := block.Banner{Colour: item.ColourRed(), Patterns: []block.BannerPatternLayer{{Type: block.BorderBannerPattern(), Colour: item.ColourBlack()}}}
		input := []item.Stack{item.NewStack(patterned, 1), item.NewStack(block.Banner{Colour: item.ColourRed()}, 1)}
		result := craftValidMulti(t, s, bannerDuplicateMultiRecipe, multiResultItem(input[0].Grow(1)), input)
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

func TestBannerMultiRecipeDerivesPatternFromGrid(t *testing.T) {
	t.Run("border arrangement", func(t *testing.T) {
		s := largeCraftingTestSession()
		base := block.Banner{Colour: item.ColourWhite()}
		input := make([]item.Stack, 9)
		for slot := range input {
			input[slot] = item.NewStack(item.Dye{Colour: item.ColourRed()}, 1)
		}
		input[4] = item.NewStack(base, 1)
		expected := base
		expected.Patterns = []block.BannerPatternLayer{{Type: block.BorderBannerPattern(), Colour: item.ColourRed()}}
		result := craftValidMulti(t, s, bannerAddPatternMultiRecipe, multiResultItem(item.NewStack(expected, 1)), input)
		banner := result.Item().(block.Banner)
		if len(banner.Patterns) != 1 || banner.Patterns[0].Type != block.BorderBannerPattern() {
			t.Fatalf("banner result = %#v, want border", banner)
		}
	})

	t.Run("template pattern", func(t *testing.T) {
		s := craftingTestSession()
		base := block.Banner{Colour: item.ColourWhite()}
		input := []item.Stack{
			item.NewStack(base, 1),
			item.NewStack(item.Dye{Colour: item.ColourRed()}, 1),
			item.NewStack(item.BannerPattern{Type: item.CreeperBannerPattern()}, 1),
		}
		expected := base
		expected.Patterns = []block.BannerPatternLayer{{Type: block.CreeperBannerPattern(), Colour: item.ColourRed()}}
		craftValidMulti(t, s, bannerAddPatternMultiRecipe, multiResultItem(item.NewStack(expected, 1)), input)
	})
}

func TestBannerMultiRecipeRejectsForgedPatterns(t *testing.T) {
	base := block.Banner{Colour: item.ColourWhite()}
	red := item.NewStack(item.Dye{Colour: item.ColourRed()}, 1)
	patterned := func(pattern block.BannerPatternType) item.Stack {
		output := base
		output.Patterns = []block.BannerPatternLayer{{Type: pattern, Colour: item.ColourRed()}}
		return item.NewStack(output, 1)
	}
	tests := []struct {
		name   string
		large  bool
		input  []item.Stack
		result protocol.StackRequestItem
	}{
		{
			name:   "illager source",
			input:  []item.Stack{item.NewStack(block.Banner{Colour: item.ColourWhite(), Illager: true}, 1), red, item.NewStack(item.BannerPattern{Type: item.CreeperBannerPattern()}, 1)},
			result: multiResultItem(patterned(block.CreeperBannerPattern())),
		},
		{
			name:   "forged illager result",
			input:  []item.Stack{item.NewStack(base, 1), red, item.NewStack(item.BannerPattern{Type: item.CreeperBannerPattern()}, 1)},
			result: multiResultItem(item.NewStack(block.Banner{Colour: item.ColourWhite(), Illager: true, Patterns: []block.BannerPatternLayer{{Type: block.CreeperBannerPattern(), Colour: item.ColourRed()}}}, 1)),
		},
		{
			name:   "mojang without template",
			input:  []item.Stack{item.NewStack(base, 1), red},
			result: multiResultItem(patterned(block.MojangBannerPattern())),
		},
		{
			name:   "creeper without template",
			input:  []item.Stack{item.NewStack(base, 1), red},
			result: multiResultItem(patterned(block.CreeperBannerPattern())),
		},
		{
			name:   "wrong template",
			input:  []item.Stack{item.NewStack(base, 1), red, item.NewStack(item.BannerPattern{Type: item.CreeperBannerPattern()}, 1)},
			result: multiResultItem(patterned(block.SkullBannerPattern())),
		},
		{
			name:  "invalid arrangement",
			large: true,
			input: []item.Stack{
				item.NewStack(base, 1), red, red,
				{}, {}, {},
				{}, {}, {},
			},
			result: multiResultItem(patterned(block.StripeTopBannerPattern())),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := craftingTestSession()
			if test.large {
				s.openedContainerID.Store(1)
			}
			setCraftingInput(t, s, test.input)
			err := craftingTestHandler().handleRequest(multiCraftingRequest(s, bannerAddPatternMultiRecipe, test.result, nonEmptySlots(test.input)...), s, nil, nil)
			requireRejectedCraft(t, s, err, test.input)
		})
	}

	t.Run("arbitrary result NBT", func(t *testing.T) {
		s := craftingTestSession()
		input := []item.Stack{item.NewStack(base, 1), red, item.NewStack(item.BannerPattern{Type: item.CreeperBannerPattern()}, 1)}
		setCraftingInput(t, s, input)
		result := multiResultItem(patterned(block.CreeperBannerPattern()))
		result.NBTData["forged"] = int32(1)
		err := craftingTestHandler().handleRequest(multiCraftingRequest(s, bannerAddPatternMultiRecipe, result, 0, 1, 2), s, nil, nil)
		requireRejectedCraft(t, s, err, input)
	})

	t.Run("invalid duplicate order", func(t *testing.T) {
		s := craftingTestSession()
		patternedBanner := base
		patternedBanner.Patterns = []block.BannerPatternLayer{{Type: block.BorderBannerPattern(), Colour: item.ColourRed()}}
		input := []item.Stack{item.NewStack(base, 1), item.NewStack(patternedBanner, 1)}
		setCraftingInput(t, s, input)
		err := craftingTestHandler().handleRequest(multiCraftingRequest(s, bannerDuplicateMultiRecipe, multiResultItem(input[1].Grow(1)), 0, 1), s, nil, nil)
		requireRejectedCraft(t, s, err, input)
	})
}

func TestMapMultiRecipesApplyStateTransitions(t *testing.T) {
	registerCraftingMaps()
	tests := []struct {
		name     string
		id       string
		large    bool
		input    []item.Stack
		expected item.Stack
	}{
		{
			name:  "extend crafting",
			id:    mapExtendingMultiRecipe,
			large: true,
			input: func() []item.Stack {
				grid := make([]item.Stack, 9)
				for slot := range grid {
					grid[slot] = item.NewStack(item.Paper{}, 1)
				}
				grid[4] = craftingMapStack(0, 2, false, false)
				return grid
			}(),
			expected: craftingMapStack(0, 2, false, false).WithItem(craftingMapItem{Meta: 0, UUID: 42, Scale: 2, ScaleDirection: 1}),
		},
		{
			name:     "extend cartography",
			id:       mapExtendingCartographyMultiRecipe,
			input:    []item.Stack{craftingMapStack(2, 1, true, false), item.NewStack(item.Paper{}, 1)},
			expected: craftingMapStack(2, 1, true, false).WithItem(craftingMapItem{Meta: 2, UUID: 42, Scale: 1, DisplayPlayers: true, ScaleDirection: 1}),
		},
		{
			name:     "upgrade crafting",
			id:       mapUpgradingMultiRecipe,
			input:    []item.Stack{craftingMapStack(0, 2, false, false), item.NewStack(item.Compass{}, 1)},
			expected: craftingMapStack(2, 2, true, false),
		},
		{
			name:     "upgrade cartography",
			id:       mapUpgradingCartographyMultiRecipe,
			input:    []item.Stack{craftingMapStack(0, 2, false, false), item.NewStack(item.Compass{}, 1)},
			expected: craftingMapStack(2, 2, true, false),
		},
		{
			name:     "lock cartography",
			id:       mapLockingMultiRecipe,
			input:    []item.Stack{craftingMapStack(2, 2, true, false), item.NewStack(craftingNamedItem("minecraft:glass_pane"), 1)},
			expected: craftingMapStack(5, 2, true, true),
		},
		{
			name:     "clone crafting",
			id:       mapCloningMultiRecipe,
			input:    []item.Stack{craftingMapStack(5, 2, true, false), item.NewStack(craftingNamedItem("minecraft:empty_map"), 1), item.NewStack(craftingNamedItem("minecraft:empty_map"), 1)},
			expected: craftingMapStack(5, 2, true, false).Grow(2),
		},
		{
			name:     "clone cartography",
			id:       mapCloningCartographyMultiRecipe,
			input:    []item.Stack{craftingMapStack(0, 2, false, false), item.NewStack(craftingNamedItem("minecraft:empty_map"), 1)},
			expected: craftingMapStack(0, 2, false, false).Grow(1),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := craftingTestSession()
			if test.large {
				s.openedContainerID.Store(1)
			}
			result := craftValidMulti(t, s, test.id, multiResultItem(test.expected), test.input)
			if !result.Equal(test.expected) {
				t.Fatalf("map result = %v, want %v", result, test.expected)
			}
		})
	}
}

func TestMapMultiRecipesRejectImpossibleTransitions(t *testing.T) {
	registerCraftingMaps()
	tests := []struct {
		name  string
		id    string
		input []item.Stack
	}{
		{"extend locked", mapExtendingCartographyMultiRecipe, []item.Stack{craftingMapStack(5, 2, true, false), item.NewStack(item.Paper{}, 1)}},
		{"extend maximum scale", mapExtendingCartographyMultiRecipe, []item.Stack{craftingMapStack(0, 4, false, false), item.NewStack(item.Paper{}, 1)}},
		{"extend pending map", mapExtendingCartographyMultiRecipe, []item.Stack{item.NewStack(craftingMapItem{Meta: 0, UUID: 42, Scale: 2, ScaleDirection: 1}, 1), item.NewStack(item.Paper{}, 1)}},
		{"upgrade locator", mapUpgradingCartographyMultiRecipe, []item.Stack{craftingMapStack(2, 2, true, false), item.NewStack(item.Compass{}, 1)}},
		{"lock locked", mapLockingMultiRecipe, []item.Stack{craftingMapStack(5, 2, true, false), item.NewStack(craftingNamedItem("minecraft:glass_pane"), 1)}},
		{"map without state API", mapExtendingCartographyMultiRecipe, []item.Stack{item.NewStack(craftingNamedItem("minecraft:filled_map"), 1), item.NewStack(item.Paper{}, 1)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := craftingTestSession()
			setCraftingInput(t, s, test.input)
			err := craftingTestHandler().handleRequest(multiCraftingRequest(s, test.id, multiResultItem(test.input[0]), nonEmptySlots(test.input)...), s, nil, nil)
			requireRejectedCraft(t, s, err, test.input)
		})
	}

	t.Run("tampered transition result", func(t *testing.T) {
		s := craftingTestSession()
		input := []item.Stack{craftingMapStack(0, 2, false, false), item.NewStack(item.Compass{}, 1)}
		setCraftingInput(t, s, input)
		forged := craftingMapStack(2, 2, true, false).WithCustomName("forged")
		err := craftingTestHandler().handleRequest(multiCraftingRequest(s, mapUpgradingCartographyMultiRecipe, multiResultItem(forged), 0, 1), s, nil, nil)
		requireRejectedCraft(t, s, err, input)
	})

	t.Run("invalid extending arrangement", func(t *testing.T) {
		s := largeCraftingTestSession()
		input := make([]item.Stack, 9)
		for slot := range input {
			input[slot] = item.NewStack(item.Paper{}, 1)
		}
		input[0] = craftingMapStack(0, 2, false, false)
		setCraftingInput(t, s, input)
		err := craftingTestHandler().handleRequest(multiCraftingRequest(s, mapExtendingMultiRecipe, multiResultItem(input[0]), nonEmptySlots(input)...), s, nil, nil)
		requireRejectedCraft(t, s, err, input)
	})
}

func TestRepairMultiRecipeStripsDataAndRetainsCurses(t *testing.T) {
	first := item.NewStack(item.Bow{}, 1).
		WithDurability(10).
		WithCustomName("forged survivor").
		WithLore("forged lore").
		WithValue("forged", true).
		WithAnvilCost(7).
		AsUnbreakable().
		WithForcedEnchantments(
			item.NewEnchantment(enchantment.Power, 3),
			item.NewEnchantment(enchantment.CurseOfVanishing, 1),
		)
	second := item.NewStack(item.Bow{}, 1).
		WithDurability(20).
		WithForcedEnchantments(item.NewEnchantment(enchantment.Punch, 2))
	expected := item.NewStack(item.Bow{}, 1).
		WithDurability(49).
		WithForcedEnchantments(item.NewEnchantment(enchantment.CurseOfVanishing, 1))

	s := craftingTestSession()
	result := craftValidMulti(t, s, repairMultiRecipe, multiResultItem(expected), []item.Stack{first, second})
	if !result.Equal(expected) || result.CustomName() != "" || len(result.Lore()) != 0 || len(result.Values()) != 0 || result.AnvilCost() != 0 || result.Unbreakable() {
		t.Fatalf("repair result retained stripped data: %v", result)
	}
	if _, ok := result.Enchantment(enchantment.CurseOfVanishing); !ok || len(result.Enchantments()) != 1 {
		t.Fatalf("repair enchantments = %#v, want only vanishing curse", result.Enchantments())
	}
}

func TestRepairMultiRecipeStripsItemNBT(t *testing.T) {
	charged := item.Crossbow{Item: item.NewStack(item.Arrow{}, 1)}
	input := []item.Stack{item.NewStack(charged, 1).WithDurability(100), item.NewStack(charged, 1).WithDurability(100)}
	expected := item.NewStack(item.Crossbow{}, 1).WithDurability(223)
	result := craftValidMulti(t, craftingTestSession(), repairMultiRecipe, multiResultItem(expected), input)
	crossbow, ok := result.Item().(item.Crossbow)
	if !ok || !crossbow.Item.Empty() {
		t.Fatalf("repair result retained charged item: %#v", result.Item())
	}
}

func TestRepairMultiRecipeRejectsInvalidCountAndTampering(t *testing.T) {
	t.Run("stacked input", func(t *testing.T) {
		s := craftingTestSession()
		input := []item.Stack{item.NewStack(craftingStackableDurable{}, 2), item.NewStack(craftingStackableDurable{}, 1)}
		setCraftingInput(t, s, input)
		err := craftingTestHandler().handleRequest(multiCraftingRequest(s, repairMultiRecipe, multiResultItem(item.NewStack(craftingStackableDurable{}, 1)), 0, 1), s, nil, nil)
		requireRejectedCraft(t, s, err, input)
	})

	t.Run("result data", func(t *testing.T) {
		s := craftingTestSession()
		input := []item.Stack{item.NewStack(item.Bow{}, 1).WithDurability(10), item.NewStack(item.Bow{}, 1).WithDurability(20)}
		setCraftingInput(t, s, input)
		forged := item.NewStack(item.Bow{}, 1).WithDurability(49).WithCustomName("forged")
		err := craftingTestHandler().handleRequest(multiCraftingRequest(s, repairMultiRecipe, multiResultItem(forged), 0, 1), s, nil, nil)
		requireRejectedCraft(t, s, err, input)
	})
}

type craftingNamedItem string

func (i craftingNamedItem) EncodeItem() (string, int16) { return string(i), 0 }

type craftingStackableDurable struct{}

func (craftingStackableDurable) EncodeItem() (string, int16) { return "test:stackable_durable", 0 }
func (craftingStackableDurable) DurabilityInfo() item.DurabilityInfo {
	return item.DurabilityInfo{MaxDurability: 100, BrokenItem: func() item.Stack { return item.Stack{} }}
}

type craftingMapItem struct {
	Meta           int16
	UUID           int64
	Scale          int32
	DisplayPlayers bool
	ScaleDirection int32
	PendingMapLock bool
}

func (m craftingMapItem) EncodeItem() (string, int16) { return "minecraft:filled_map", m.Meta }

func (m craftingMapItem) EncodeNBT() map[string]any {
	data := map[string]any{"map_uuid": m.UUID, "map_scale": m.Scale}
	if m.DisplayPlayers {
		data["map_display_players"] = byte(1)
	}
	if m.ScaleDirection != 0 {
		data["map_scale_direction"] = m.ScaleDirection
	}
	if m.PendingMapLock {
		data["map_to_lock"] = byte(1)
	}
	return data
}

func (m craftingMapItem) DecodeNBT(data map[string]any) any {
	m.UUID = testNBTInt64(data["map_uuid"])
	m.Scale = int32(testNBTInt64(data["map_scale"]))
	m.DisplayPlayers = testNBTInt64(data["map_display_players"]) != 0
	m.ScaleDirection = int32(testNBTInt64(data["map_scale_direction"]))
	m.PendingMapLock = testNBTInt64(data["map_to_lock"]) != 0
	return m
}

var registerCraftingMapsOnce sync.Once

func registerCraftingMaps() {
	registerCraftingMapsOnce.Do(func() {
		world.RegisterItem(craftingMapItem{Meta: 0})
		world.RegisterItem(craftingMapItem{Meta: 2})
		world.RegisterItem(craftingMapItem{Meta: 5})
	})
}

func craftingMapStack(meta int16, scale int32, displayPlayers, pendingLock bool) item.Stack {
	return item.NewStack(craftingMapItem{Meta: meta, UUID: 42, Scale: scale, DisplayPlayers: displayPlayers, PendingMapLock: pendingLock}, 1)
}

func testNBTInt64(value any) int64 {
	switch value := value.(type) {
	case byte:
		return int64(value)
	case int16:
		return int64(value)
	case int32:
		return int64(value)
	case int64:
		return value
	}
	return 0
}

func craftValidMulti(t *testing.T, s *Session, id string, result protocol.StackRequestItem, input []item.Stack) item.Stack {
	t.Helper()
	setCraftingInput(t, s, input)
	consumes := nonEmptySlots(input)
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
		slot := int(s.craftingOffset()) + inputSlot
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
		got, _ := s.ui.Item(int(s.craftingOffset()) + slot)
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

func multiResultItem(stack item.Stack) protocol.StackRequestItem {
	name, meta := stack.Item().EncodeItem()
	return protocol.StackRequestItem{
		Identifier:    name,
		MetadataValue: uint32(meta),
		Count:         uint16(stack.Count()),
		NBTData:       item.WriteNBT(stack, false),
	}
}

func setCraftingInput(t *testing.T, s *Session, input []item.Stack) {
	t.Helper()
	for slot, stack := range input {
		if err := s.ui.SetItem(int(s.craftingOffset())+slot, stack); err != nil {
			t.Fatal(err)
		}
	}
}

func nonEmptySlots(input []item.Stack) []int {
	slots := make([]int, 0, len(input))
	for slot, stack := range input {
		if !stack.Empty() {
			slots = append(slots, slot)
		}
	}
	return slots
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

func largeCraftingTestSession() *Session {
	s := craftingTestSession()
	s.openedContainerID.Store(1)
	return s
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
