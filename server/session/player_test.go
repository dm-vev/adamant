package session

import (
	"context"
	"errors"
	"testing"
	"time"

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
		task = s.abilityResend
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
		task = s.abilityResend
	}).Wait(context.Background()); err != nil {
		t.Fatalf("send game mode: %v", err)
	}
	<-s.packets // SetPlayerGameType.

	s.CloseConnection()
	if err := task.Wait(context.Background()); !errors.Is(err, world.ErrTaskCancelled) {
		t.Fatalf("delayed ability resend error = %v, want %v", err, world.ErrTaskCancelled)
	}
	select {
	case pk := <-s.packets:
		t.Fatalf("ability packet sent after close: %T", pk)
	default:
	}
}

func abilityTestSession(t *testing.T, delay time.Duration) (*Session, *abilityTestControllable, *world.World) {
	t.Helper()
	s := &Session{
		conn:               abilityTestConn{},
		packets:            make(chan packet.Packet, 2),
		closeBackground:    make(chan struct{}),
		abilityResendDelay: delay,
	}
	c := &abilityTestControllable{mode: world.GameModeCreative}
	c.handle = world.EntitySpawnOpts{}.New(abilityTestType{c: c}, abilityTestConfig{})
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
	handle *world.EntityHandle
	mode   world.GameMode
}

func (c *abilityTestControllable) Close() error               { return nil }
func (c *abilityTestControllable) H() *world.EntityHandle     { return c.handle }
func (c *abilityTestControllable) Position() mgl64.Vec3       { return mgl64.Vec3{} }
func (c *abilityTestControllable) Rotation() cube.Rotation    { return cube.Rotation{} }
func (c *abilityTestControllable) GameMode() world.GameMode   { return c.mode }
func (*abilityTestControllable) Flying() bool                 { return false }
func (*abilityTestControllable) FlightSpeed() float64         { return 0.05 }
func (*abilityTestControllable) VerticalFlightSpeed() float64 { return 0.05 }

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
