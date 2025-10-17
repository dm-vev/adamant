package enchantment

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const waterLiquidType = "water"

// Impaling is a trident enchantment that deals additional damage to targets in water or rain.
var Impaling impaling

type impaling struct{}

// Name ...
func (impaling) Name() string {
	return "Impaling"
}

// MaxLevel ...
func (impaling) MaxLevel() int {
	return 5
}

// Cost ...
func (impaling) Cost(level int) (int, int) {
	minCost := 1 + (level-1)*8
	return minCost, minCost + 20
}

// Rarity ...
func (impaling) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityRare
}

// CompatibleWithEnchantment ...
func (impaling) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (impaling) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Trident)
	return ok
}

// Damage returns the additional damage dealt by impaling when striking the provided entity in the given world.
func (impaling) Damage(level int, target world.Entity, tx *world.Tx) float64 {
	if level <= 0 || target == nil || tx == nil {
		return 0
	}
	if !impalingApplies(target, tx) {
		return 0
	}
	return float64(level) * 2.5
}

func impalingApplies(target world.Entity, tx *world.Tx) bool {
	pos := cube.PosFromVec3(target.Position())
	if liquid, ok := tx.Liquid(pos); ok && isWaterLiquid(liquid) {
		return true
	}
	head := cube.PosFromVec3(target.Position().Add(mgl64.Vec3{0, 1, 0}))
	if liquid, ok := tx.Liquid(head); ok && isWaterLiquid(liquid) {
		return true
	}
	return tx.RainingAt(pos)
}

func isWaterLiquid(liquid world.Liquid) bool {
	return liquid != nil && liquid.LiquidType() == waterLiquidType
}
