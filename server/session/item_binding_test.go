package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

type bindingTestControllable struct {
	Controllable
	mode world.GameMode
}

func (c bindingTestControllable) GameMode() world.GameMode { return c.mode }

func TestBindingBlocksArmourRemovalOutsideCreative(t *testing.T) {
	bound := item.NewStack(item.Boots{Tier: item.ArmourTierLeather{}}, 1).WithEnchantments(item.NewEnchantment(enchantment.CurseOfBinding, 1))
	from := v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: protocol.ContainerArmor}, Slot: 3}
	to := v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: protocol.ContainerInventory}, Slot: 0}
	if err := ensureBindingAllowsRemoval(bound, from, to, bindingTestControllable{mode: world.GameModeSurvival}); err == nil {
		t.Fatal("survival removal of bound armour was allowed")
	}
	if err := ensureBindingAllowsRemoval(bound, from, to, bindingTestControllable{mode: world.GameModeCreative}); err != nil {
		t.Fatalf("creative removal of bound armour rejected: %v", err)
	}
}
