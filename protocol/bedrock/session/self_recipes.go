package session

import (
	"maps"

	"github.com/df-mc/dragonfly/server/item"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// sendRecipes sends the current crafting recipes to the session.
func (s *Session) sendRecipes() {
	d := s.tables().recipeData()
	s.recipes = maps.Clone(d.recipes)
	s.javaRecipeIDs = maps.Clone(d.javaIDs)
	s.writePacket(d.packet)
}

// ViewRecipesUnlocked sends newly unlocked Java recipes that have a
// corresponding recipe in the current Bedrock crafting data.
func (s *Session) ViewRecipesUnlocked(javaIDs []string) {
	s.unlockRecipes(javaIDs, mc.UnlockedRecipesTypeNewlyUnlocked)
}

// SendInitiallyUnlockedRecipes sends persisted recipe unlocks after the player
// has joined and the crafting data has been sent.
func (s *Session) SendInitiallyUnlockedRecipes(javaIDs []string) {
	s.unlockRecipes(javaIDs, mc.UnlockedRecipesTypeInitiallyUnlocked)
}

// unlockRecipes sends the Bedrock recipes of the Java IDs passed, unlocked
// as unlockType says, if there are any.
func (s *Session) unlockRecipes(javaIDs []string, unlockType uint32) {
	packetIDs := make([]string, 0, len(javaIDs))
	seenPacketIDs := make(map[string]struct{}, len(javaIDs))
	for _, javaID := range javaIDs {
		ids, ok := s.javaRecipeIDs[javaID]
		if !ok {
			continue
		}
		for _, packetID := range ids {
			if _, seen := seenPacketIDs[packetID]; seen {
				continue
			}
			seenPacketIDs[packetID] = struct{}{}
			packetIDs = append(packetIDs, packetID)
		}
	}
	if len(packetIDs) != 0 {
		s.writePacket(&mc.UnlockedRecipesPacket{UnlockType: unlockType, Recipes: packetIDs})
	}
}

// sendArmourTrimData sends the armour trim data.
func (s *Session) sendArmourTrimData() {
	var trimPatterns []mc.TrimPattern
	var trimMaterials []mc.TrimMaterial

	for _, t := range item.SmithingTemplates() {
		if t == item.TemplateNetheriteUpgrade() {
			continue
		}
		name, _ := item.SmithingTemplate{Template: t}.EncodeItem()
		trimPatterns = append(trimPatterns, mc.TrimPattern{
			ItemName:  name,
			PatternID: t.String(),
		})
	}

	for _, i := range item.ArmourTrimMaterials() {
		if material, ok := i.(item.ArmourTrimMaterial); ok {
			name, _ := i.EncodeItem()

			trimMaterials = append(trimMaterials, mc.TrimMaterial{
				MaterialID: material.TrimMaterial(),
				Colour:     trimMaterialColour(material),
				ItemName:   name,
			})
		}
	}

	s.writePacket(&mc.TrimDataPacket{Patterns: trimPatterns, Materials: trimMaterials})
}
