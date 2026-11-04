package session

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// handleEnchant handles the enchantment of an item using the CraftRecipe stack request action.
func (h *ItemStackRequestHandler) handleEnchant(a *mc.CraftRecipeStackRequestAction) error {
	return h.batchOp(func() error { return h.batch.Enchant(int(a.RecipeNetworkID)) })
}

// ViewEnchantmentOptions shows the client the three options of the
// enchanting table its player has open. The enchantments of an option the
// client has no ID of, the Java-only ones, are left out of what it is sent
// of the option: an option of those alone is sent with its cost and no
// enchantment.
func (s *Session) ViewEnchantmentOptions(selected [3]menu.EnchantmentOption) {
	// Build the protocol variant of the enchantment options.
	options := make([]mc.EnchantmentOption, 0, 3)
	for i := 0; i < 3; i++ {
		// First build the enchantment instances for each selected enchantment.
		enchants := make([]mc.EnchantmentInstance, 0, len(selected[i].Enchantments))
		for _, enchant := range selected[i].Enchantments {
			id, ok := item.EnchantmentID(enchant.Type())
			if !ok {
				continue
			}
			enchants = append(enchants, mc.EnchantmentInstance{
				Type:  byte(id),
				Level: byte(enchant.Level()),
			})
		}

		// Then build the enchantment option. We can use the slot as the RecipeNetworkID, since the IDs seem to be unique
		// to enchanting tables only. We also only need to set the middle index of Enchantments. The other two serve
		// an unknown purpose and can cause various unexpected issues.
		options = append(options, mc.EnchantmentOption{
			Name:            enchantNames[rand.IntN(len(enchantNames))],
			Cost:            uint8(selected[i].Cost),
			RecipeNetworkID: uint32(i),
			Enchantments: mc.ItemEnchantments{
				Slot:         int32(i),
				Enchantments: [3][]mc.EnchantmentInstance{1: enchants},
			},
		})
	}

	// Send the enchantment options to the client.
	s.writePacket(&mc.PlayerEnchantOptionsPacket{Options: options})
}
