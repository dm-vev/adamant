package player

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
)

// Food returns the current food level of a player. The level returned is guaranteed to always be between 0
// and 20. Every half drumstick is one level.
func (p *Player) Food() int {
	return p.hunger.Food()
}

// SetFood sets the food level of a player. The level passed must be in a range of 0-20. If the level passed
// is negative, the food level will be set to 0. If the level exceeds 20, the food level will be set to 20.
func (p *Player) SetFood(level int) {
	p.hunger.SetFood(level)
	p.sendFood()
}

// AddFood adds a number of points to the food level of the player. If the new food level is negative or if
// it exceeds 20, it will be set to 0 or 20 respectively.
func (p *Player) AddFood(points int) {
	p.hunger.AddFood(points)
	p.sendFood()
}

// Saturate saturates the player's food bar with the amount of food points and saturation points passed. The
// total saturation of the player will never exceed its total food level.
func (p *Player) Saturate(food int, saturation float64) {
	p.hunger.saturate(food, saturation)
	p.sendFood()
}

// sendFood sends the current food properties to the client.
func (p *Player) sendFood() {
	p.sentFood = p.hunger.snapshot()
	p.client().SendFood(p.sentFood.foodLevel, p.sentFood.saturationLevel, p.sentFood.exhaustionLevel)
}

// AddEffect adds an entity.Effect to the Player. If the effect is instant, it is applied to the Player
// immediately. If not, the effect is applied to the player every time the Tick method is called.
// AddEffect will overwrite any effects present if the level of the effect is higher than the existing one, or
// if the effects' levels are equal and the new effect has a longer duration.
func (p *Player) AddEffect(e effect.Effect) {
	_, had := p.effects.Effect(e.Type())
	switch res := p.effects.Add(e, p); {
	case res.Type() == nil:
		// The player is immune to the effect.
	case had:
		p.client().SendEffectModification(res)
	default:
		p.client().SendEffect(res)
	}
	p.updateState()
}

// RemoveEffect removes any effect that might currently be active on the Player.
func (p *Player) RemoveEffect(e effect.Type) {
	p.effects.Remove(e, p)
	p.client().SendEffectRemoval(e)
	p.updateState()
}

// Effect returns the effect instance and true if the Player has the effect. If not found, it will return an empty
// effect instance and false.
func (p *Player) Effect(e effect.Type) (effect.Effect, bool) {
	return p.effects.Effect(e)
}

// Effects returns any effect currently applied to the entity. The returned effects are guaranteed not to have
// expired when returned.
func (p *Player) Effects() []effect.Effect {
	return p.effects.Effects()
}

// BeaconAffected ...
func (*Player) BeaconAffected() bool {
	return true
}

// Exhaust adds the points passed to the player's exhaustion unless it is in
// creative or spectator mode, as Player.causeFoodExhaustion. Each tick in
// which the exhaustion exceeds 4, a point of saturation, or of food if
// saturation is 0, is lost.
func (p *Player) Exhaust(points float64) {
	if !p.GameMode().AllowsTakingDamage() {
		return
	}
	p.hunger.exhaust(points)
}

// tickFood ports ServerPlayer.tickRegeneration and FoodData.tick: in
// Peaceful the player heals and refills its food bar, exhaustion drains
// saturation and food, a full food bar heals the player and an empty one
// starves it.
func (p *Player) tickFood() {
	w := p.tx.World()
	naturalRegeneration := w.GameRuleBool("natural_health_regeneration")
	difficulty := w.Difficulty()
	peaceful := difficulty == world.DifficultyPeaceful
	if peaceful && naturalRegeneration {
		if p.tickCount%20 == 0 {
			if p.Health() < p.MaxHealth() {
				p.Heal(1, entity.FoodHealingSource{})
			}
			p.hunger.regeneratePeaceful()
		}
		if p.tickCount%10 == 0 && p.hunger.Food() < 20 {
			p.hunger.AddFood(1)
		}
	}

	if before, lose := p.hunger.drainExhaustion(peaceful); lose {
		after := before - 1
		ctx := NewEventContext(p.tx, p)
		if p.Handler().HandleFoodLoss(ctx, before, &after); !ctx.Cancelled() {
			p.hunger.SetFood(after)
		}
	}

	state := p.hunger.snapshot()
	hurt := p.Health() > 0 && p.Health() < p.MaxHealth()
	switch {
	case naturalRegeneration && state.saturationLevel > 0 && hurt && state.foodLevel >= 20:
		if p.hunger.advanceTimer(10) {
			spent := min(state.saturationLevel, 6)
			p.Heal(spent/6, entity.FoodHealingSource{QuickRegeneration: true})
			p.hunger.exhaust(spent)
		}
	case naturalRegeneration && state.foodLevel >= 18 && hurt:
		if p.hunger.advanceTimer(80) {
			p.Heal(1, entity.FoodHealingSource{})
			p.hunger.exhaust(6)
		}
	case state.foodLevel <= 0:
		if p.hunger.advanceTimer(80) && p.Health() > difficulty.StarvationHealthLimit() {
			p.Hurt(1, StarvationDamageSource{})
		}
	default:
		p.hunger.resetTimer()
	}

	if !p.hunger.canSprint() {
		p.StopSprinting()
	}
	if sent := p.hunger.snapshot(); sent.foodLevel != p.sentFood.foodLevel || sent.saturationLevel != p.sentFood.saturationLevel || sent.exhaustionLevel != p.sentFood.exhaustionLevel {
		p.sentFood = sent
		p.client().SendFood(sent.foodLevel, sent.saturationLevel, sent.exhaustionLevel)
	}
}
