package session

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// handleEnchant handles the enchantment of an item using the CraftRecipe stack request action.
func (h *ItemStackRequestHandler) handleEnchant(a *v2193.CraftRecipeStackRequestAction) error {
	return h.batchOp(func() error { return h.batch.Enchant(int(a.RecipeNetworkID)) })
}

// ViewEnchantmentOptions shows the client the three options of the
// enchanting table its player has open.
func (s *Session) ViewEnchantmentOptions(selected [3]menu.EnchantmentOption) {
	// Build the protocol variant of the enchantment options.
	options := make([]v2193.EnchantmentOption, 0, 3)
	for i := 0; i < 3; i++ {
		// First build the enchantment instances for each selected enchantment.
		enchants := make([]v2193.EnchantmentInstance, 0, len(selected[i].Enchantments))
		for _, enchant := range selected[i].Enchantments {
			id, _ := item.EnchantmentID(enchant.Type())
			enchants = append(enchants, v2193.EnchantmentInstance{
				Type:  byte(id),
				Level: byte(enchant.Level()),
			})
		}

		// Then build the enchantment option. We can use the slot as the RecipeNetworkID, since the IDs seem to be unique
		// to enchanting tables only. We also only need to set the middle index of Enchantments. The other two serve
		// an unknown purpose and can cause various unexpected issues.
		options = append(options, v2193.EnchantmentOption{
			Name:            enchantNames[rand.IntN(len(enchantNames))],
			Cost:            uint8(selected[i].Cost),
			RecipeNetworkID: uint32(i),
			Enchantments: v2193.ItemEnchantments{
				Slot:         int32(i),
				Enchantments: [3][]v2193.EnchantmentInstance{1: enchants},
			},
		})
	}

	// Send the enchantment options to the client.
	s.writePacket(&v2193.PlayerEnchantOptionsPacket{Options: options})
}
