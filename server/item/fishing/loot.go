package fishing

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/potion"
)

// Loot returns a pseudo-random fishing loot stack based on the provided luck and lure levels.
func Loot(luckLevel, lureLevel int) item.Stack {
	treasureChance := clamp01(0.05 + 0.01*float64(luckLevel) - 0.01*float64(lureLevel))
	junkChance := clamp01(0.1 - 0.025*float64(luckLevel) - 0.01*float64(lureLevel))

	r := rand.Float64()
	switch {
	case r < treasureChance:
		return randomTreasureLoot()
	case r < treasureChance+junkChance:
		return randomJunkLoot()
	default:
		return randomFishLoot()
	}
}

func randomFishLoot() item.Stack {
	r := rand.Float64()
	switch {
	case r < 0.60:
		return item.NewStack(item.Cod{}, 1)
	case r < 0.85:
		return item.NewStack(item.Salmon{}, 1)
	case r < 0.98:
		return item.NewStack(item.Pufferfish{}, 1)
	default:
		return item.NewStack(item.TropicalFish{}, 1)
	}
}

func randomTreasureLoot() item.Stack {
	switch rand.IntN(6) {
	case 0:
		return randomEnchantedBow()
	case 1:
		return randomEnchantedBook()
	case 2:
		return randomEnchantedRod()
	case 3:
		return item.NewStack(item.NameTag{}, 1)
	case 4:
		return item.NewStack(item.Saddle{}, 1)
	default:
		return item.NewStack(item.NautilusShell{}, 1)
	}
}

func randomJunkLoot() item.Stack {
	switch rand.IntN(11) {
	case 0:
		return item.NewStack(item.Bowl{}, 1)
	case 1:
		return item.NewStack(item.FishingRod{}, 1)
	case 2:
		return item.NewStack(item.Leather{}, 1)
	case 3:
		return item.NewStack(item.Boots{Tier: item.ArmourTierLeather{}}, 1)
	case 4:
		return item.NewStack(item.RottenFlesh{}, 1)
	case 5:
		return item.NewStack(item.Stick{}, 1)
	case 6:
		return item.NewStack(item.String{}, 2)
	case 7:
		return item.NewStack(item.Potion{Type: potion.Water()}, 1)
	case 8:
		return item.NewStack(item.Bone{}, 1)
	case 9:
		return item.NewStack(item.InkSac{}, 1)
	default:
		return item.NewStack(item.TripwireHook{}, 1)
	}
}

func randomEnchantedBow() item.Stack {
	bow := item.NewStack(item.Bow{}, 1)
	var enchants []item.Enchantment

	enchants = append(enchants, item.NewEnchantment(enchantment.Power, 3+rand.IntN(3)))
	if rand.Float64() < 0.25 {
		enchants = append(enchants, item.NewEnchantment(enchantment.Punch, 1+rand.IntN(2)))
	}
	if rand.Float64() < 0.25 {
		enchants = append(enchants, item.NewEnchantment(enchantment.Flame, 1))
	}
	if rand.Float64() < 0.6 {
		enchants = append(enchants, item.NewEnchantment(enchantment.Unbreaking, 3))
	}
	if rand.Float64() < 0.2 {
		enchants = append(enchants, item.NewEnchantment(enchantment.Mending, 1))
	}
	return bow.WithEnchantments(enchants...)
}

func randomEnchantedRod() item.Stack {
	rod := item.NewStack(item.FishingRod{}, 1)
	var enchants []item.Enchantment

	if rand.Float64() < 0.8 {
		enchants = append(enchants, item.NewEnchantment(enchantment.LuckOfTheSea, 1+rand.IntN(3)))
	}
	if rand.Float64() < 0.8 {
		enchants = append(enchants, item.NewEnchantment(enchantment.Lure, 1+rand.IntN(3)))
	}
	if rand.Float64() < 0.5 {
		enchants = append(enchants, item.NewEnchantment(enchantment.Unbreaking, 3))
	}
	if rand.Float64() < 0.25 {
		enchants = append(enchants, item.NewEnchantment(enchantment.Mending, 1))
	}
	if len(enchants) == 0 {
		enchants = append(enchants, item.NewEnchantment(enchantment.Lure, 1+rand.IntN(3)))
	}
	return rod.WithEnchantments(enchants...)
}

func randomEnchantedBook() item.Stack {
	book := item.NewStack(item.EnchantedBook{}, 1)
	choices := []item.EnchantmentType{
		enchantment.Protection,
		enchantment.FireProtection,
		enchantment.FeatherFalling,
		enchantment.Thorns,
		enchantment.Respiration,
		enchantment.DepthStrider,
		enchantment.FireAspect,
		enchantment.Sharpness,
		enchantment.Unbreaking,
		enchantment.Mending,
		enchantment.QuickCharge,
		enchantment.LuckOfTheSea,
		enchantment.Lure,
	}
	enchantType := choices[rand.IntN(len(choices))]
	level := 1
	if max := enchantType.MaxLevel(); max > 1 {
		level = 1 + rand.IntN(max)
	}
	return book.WithEnchantments(item.NewEnchantment(enchantType, level))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
