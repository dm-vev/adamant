package player

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// Experience returns the amount of experience the player has.
func (p *Player) Experience() int {
	return p.experience.Experience()
}

// EnchantmentSeed is a seed used to calculate random enchantments with enchantment tables.
func (p *Player) EnchantmentSeed() int64 {
	return p.enchantSeed
}

// ResetEnchantmentSeed resets the enchantment seed to a new random value.
func (p *Player) ResetEnchantmentSeed() {
	p.enchantSeed = rand.Int64()
}

// AddExperience adds experience to the player.
func (p *Player) AddExperience(amount int) int {
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleExperienceGain(ctx, &amount); ctx.Cancelled() {
		return 0
	}
	before := p.experience.Level()
	level, _ := p.experience.Add(amount)
	if level/5 > before/5 {
		p.PlaySound(sound.LevelUp{})
	} else if amount > 0 {
		p.PlaySound(sound.Experience{})
	}
	p.client().SendExperience(p.ExperienceLevel(), p.ExperienceProgress())
	return amount
}

// RemoveExperience removes experience from the player.
func (p *Player) RemoveExperience(amount int) {
	p.experience.Add(-amount)
	p.client().SendExperience(p.ExperienceLevel(), p.ExperienceProgress())
}

// ExperienceLevel returns the experience level of the player.
func (p *Player) ExperienceLevel() int {
	return p.experience.Level()
}

// SetExperienceLevel sets the experience level of the player. The level must have a value between 0 and 2,147,483,647,
// otherwise the method panics.
func (p *Player) SetExperienceLevel(level int) {
	p.experience.SetLevel(level)
	p.client().SendExperience(p.ExperienceLevel(), p.ExperienceProgress())
}

// ExperienceProgress returns the experience progress of the player.
func (p *Player) ExperienceProgress() float64 {
	return p.experience.Progress()
}

// SetExperienceProgress sets the experience progress of the player. The progress must have a value between 0.0 and 1.0, otherwise
// the method panics.
func (p *Player) SetExperienceProgress(progress float64) {
	p.experience.SetProgress(progress)
	p.client().SendExperience(p.ExperienceLevel(), p.ExperienceProgress())
}

// CanCollectExperience checks if the player can collect experience, which is true if the player is not dead,
// is in a game mode that allows interaction and if 100ms have passed since the last experience collection.
func (p *Player) CanCollectExperience() bool {
	if p.Dead() || !p.GameMode().AllowsInteraction() {
		return false
	}
	if last := p.lastXPPickup; last != nil && time.Since(*last) < time.Millisecond*100 {
		return false
	}
	return true
}

// CollectExperience makes the player collect the experience points passed, adding it to the experience manager. A bool
// is returned indicating whether the player was able to collect the experience or not, due to the 100ms delay between
// experience collection or if the player was dead or in a game mode that doesn't allow collection.
func (p *Player) CollectExperience(value int) bool {
	if !p.CanCollectExperience() {
		return false
	}
	value = p.mendItems(value)
	now := time.Now()
	p.lastXPPickup = &now
	if value > 0 {
		return p.AddExperience(value) > 0
	}

	p.PlaySound(sound.Experience{})
	return true
}

// mendItems handles the mending enchantment when collecting experience, it then returns the leftover experience.
func (p *Player) mendItems(xp int) int {
	mendingItems := make([]item.Stack, 0, 6)
	held, offHand := p.HeldItems()
	if _, ok := offHand.Enchantment(enchantment.Mending); ok && offHand.Durability() < offHand.MaxDurability() {
		mendingItems = append(mendingItems, offHand)
	}
	if _, ok := held.Enchantment(enchantment.Mending); ok && held.Durability() < held.MaxDurability() {
		mendingItems = append(mendingItems, held)
	}
	for _, i := range p.Armour().Items() {
		if i.Durability() == i.MaxDurability() {
			continue
		}
		if _, ok := i.Enchantment(enchantment.Mending); ok {
			mendingItems = append(mendingItems, i)
		}
	}
	length := len(mendingItems)
	if length == 0 {
		return xp
	}
	foundItem := mendingItems[rand.IntN(length)]
	repairAmount := math.Min(float64(foundItem.MaxDurability()-foundItem.Durability()), float64(xp*2))
	repairedItem := foundItem.WithDurability(foundItem.Durability() + int(repairAmount))
	if repairAmount >= 2 {
		// mending removes 1 experience point for every 2 durability points. If the repaired durability is less than 2,
		// then no experience is removed.
		xp -= int(math.Ceil(repairAmount / 2))
	}
	if offHand.Equal(foundItem) {
		p.SetHeldItems(held, repairedItem)
	} else if held.Equal(foundItem) {
		p.SetHeldItems(repairedItem, offHand)
	} else if slot, ok := p.Armour().Inventory().First(foundItem); ok {
		_ = p.Armour().Inventory().SetItem(slot, repairedItem)
	}
	return xp
}
