package entity

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// ticksRequiredToFreeze is Java's Entity.getTicksRequiredToFreeze.
const ticksRequiredToFreeze = 140

// FreezeDamageSource is used for damage caused by freezing in powder snow.
type FreezeDamageSource struct{}

func (FreezeDamageSource) ReducedByArmour() bool     { return false }
func (FreezeDamageSource) ReducedByResistance() bool { return true }
func (FreezeDamageSource) Fire() bool                { return false }
func (FreezeDamageSource) IgnoreTotem() bool         { return false }

// EnterPowderSnow ports InsideBlockEffectType.FREEZE once per tick for a
// living entity inside powder snow: it freezes one tick further.
func EnterPowderSnow(e world.Entity, data *world.EntityData) {
	if data.InPowderSnow {
		return
	}
	data.InPowderSnow = true
	if CanFreeze(e) {
		data.FrozenTicks = min(ticksRequiredToFreeze, data.FrozenTicks+1)
	}
}

// TickFreezing ports the freezing step of LivingEntity.aiStep, run after the
// entity's movement: out of powder snow it thaws two ticks per tick, and a
// fully frozen entity takes freeze damage every 40 ticks.
func TickFreezing(tx *world.Tx, e world.Entity, data *world.EntityData) {
	data.WasInPowderSnow = data.InPowderSnow
	if data.FrozenTicks == 0 && !data.InPowderSnow {
		return
	}
	canFreeze := CanFreeze(e)
	if !data.InPowderSnow || !canFreeze {
		data.FrozenTicks = max(0, data.FrozenTicks-2)
	}
	data.InPowderSnow = false
	if tx.World().CurrentTick()%40 != 0 || data.FrozenTicks < ticksRequiredToFreeze || !canFreeze {
		return
	}
	living, ok := e.(Living)
	if !ok {
		return
	}
	damage := 1.0
	switch e.H().Type().EncodeEntity() {
	case "minecraft:strider", "minecraft:blaze", "minecraft:magma_cube":
		// FREEZE_HURTS_EXTRA_TYPES take five times the damage.
		damage = 5
	}
	living.Hurt(damage, FreezeDamageSource{})
}

// FreezingStrength returns Java's Entity.getPercentFrozen, which Bedrock shows
// as the freezing overlay strength.
func FreezingStrength(data *world.EntityData) float32 {
	return float32(min(data.FrozenTicks, ticksRequiredToFreeze)) / ticksRequiredToFreeze
}

// CanFreeze ports Entity.canFreeze and its LivingEntity, Skeleton and
// SulfurCube overrides: spectators, freeze-immune entity types and entities
// wearing leather armour do not freeze.
func CanFreeze(e world.Entity) bool {
	if g, ok := e.(interface{ GameMode() world.GameMode }); ok && g.GameMode() == world.GameModeSpectator {
		return false
	}
	switch e.H().Type().EncodeEntity() {
	// FREEZE_IMMUNE_ENTITY_TYPES; skeletons convert into strays instead.
	case "minecraft:stray", "minecraft:polar_bear", "minecraft:snow_golem", "minecraft:wither", "minecraft:skeleton":
		return false
	}
	if s, ok := e.(*SulfurCube); ok && !s.b().bodyItem.Empty() {
		return false
	}
	var armour []item.Stack
	switch a := e.(type) {
	case interface{ Armour() *inventory.Armour }:
		armour = a.Armour().Items()
	case interface {
		EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack)
	}:
		head, chest, legs, feet := a.EquipmentArmour()
		armour = []item.Stack{head, chest, legs, feet}
	}
	if body, ok := e.(interface{ BodyArmor() item.Stack }); ok {
		armour = append(armour, body.BodyArmor())
	}
	for _, stack := range armour {
		if freezeImmuneWearable(stack) {
			return false
		}
	}
	return true
}

// freezeImmuneWearable reports whether stack is in the
// freeze_immune_wearables item tag: leather armour and leather horse armour.
func freezeImmuneWearable(stack item.Stack) bool {
	var tier item.ArmourTier
	switch it := stack.Item().(type) {
	case item.Helmet:
		tier = it.Tier
	case item.Chestplate:
		tier = it.Tier
	case item.Leggings:
		tier = it.Tier
	case item.Boots:
		tier = it.Tier
	case item.LeatherHorseArmour:
		return true
	default:
		return false
	}
	_, leather := tier.(item.ArmourTierLeather)
	return leather
}

// EnterPowderSnow marks the entity as inside powder snow this tick.
func (e *Ent) EnterPowderSnow() { EnterPowderSnow(e.living(), e.data) }

// FrozenTicks returns how far the entity froze in powder snow.
func (e *Ent) FrozenTicks() int { return e.data.FrozenTicks }

// SetFrozenTicks sets how far the entity froze, as Entity.setTicksFrozen.
func (e *Ent) SetFrozenTicks(ticks int) {
	if e.data.FrozenTicks != ticks {
		e.data.FrozenTicks = max(0, ticks)
		e.updateState()
	}
}

// living returns the entity wrapping e, which carries its armour and type
// specific behaviour.
func (e *Ent) living() world.Entity {
	if e.tx != nil {
		if wrapper, ok := e.handle.Entity(e.tx); ok {
			return wrapper
		}
	}
	return e
}

// FreezingStrength returns the freezing overlay strength from 0 to 1.
func (e *Ent) FreezingStrength() float32 { return FreezingStrength(e.data) }
