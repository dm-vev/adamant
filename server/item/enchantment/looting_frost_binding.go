package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

var Looting looting
var FrostWalker frostWalker
var CurseOfBinding curseOfBinding

type looting struct{}
type frostWalker struct{}
type curseOfBinding struct{}

func (looting) Name() string                                        { return "Looting" }
func (looting) MaxLevel() int                                       { return 3 }
func (looting) Cost(level int) (int, int)                           { n := 15 + (level-1)*9; return n, n + 50 }
func (looting) Rarity() item.EnchantmentRarity                      { return item.EnchantmentRarityRare }
func (looting) CompatibleWithEnchantment(item.EnchantmentType) bool { return true }
func (looting) CompatibleWithItem(i world.Item) bool {
	t, ok := i.(item.Tool)
	return ok && t.ToolType() == item.TypeSword
}

func (frostWalker) Name() string                   { return "Frost Walker" }
func (frostWalker) MaxLevel() int                  { return 2 }
func (frostWalker) Cost(level int) (int, int)      { n := level * 10; return n, n + 15 }
func (frostWalker) Rarity() item.EnchantmentRarity { return item.EnchantmentRarityRare }
func (frostWalker) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	_, ok := t.(depthStrider)
	return !ok
}
func (frostWalker) CompatibleWithItem(i world.Item) bool {
	b, ok := i.(item.BootsType)
	return ok && b.Boots()
}
func (frostWalker) Treasure() bool { return true }

func (curseOfBinding) Name() string                                        { return "Curse of Binding" }
func (curseOfBinding) MaxLevel() int                                       { return 1 }
func (curseOfBinding) Cost(int) (int, int)                                 { return 25, 50 }
func (curseOfBinding) Rarity() item.EnchantmentRarity                      { return item.EnchantmentRarityVeryRare }
func (curseOfBinding) CompatibleWithEnchantment(item.EnchantmentType) bool { return true }
func (curseOfBinding) CompatibleWithItem(i world.Item) bool                { _, ok := i.(item.Armour); return ok }
func (curseOfBinding) Treasure() bool                                      { return true }
func (curseOfBinding) Curse() bool                                         { return true }
