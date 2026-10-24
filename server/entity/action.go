package entity

import (
	"github.com/df-mc/dragonfly/server/world"
	"time"
)

// SwingArmAction is a world.EntityAction that makes an entity or player swing its arm.
type SwingArmAction struct{ action }

// HurtAction is a world.EntityAction that makes an entity display the animation for being hurt. The entity will be
// shown as red for a short duration.
type HurtAction struct{ action }

// CriticalHitAction is a world.EntityAction that makes an entity display critical hit particles. This will show stars
// around the entity.
type CriticalHitAction struct {
	action
	// Count is the count of particles around the entity.
	Count int
}

// EnchantedHitAction is a world.Action that makes an entity display enchanted hit particles. This will show circles
// around the entity.
type EnchantedHitAction struct {
	action
	// Count is the count of particles around the entity.
	Count int
}

// DeathAction is a world.EntityAction that makes an entity display the death animation. After this animation, the
// entity disappears from viewers watching it.
type DeathAction struct{ action }

// DragonDeathAction is a world.EntityAction that makes an ender dragon start
// the beams of light of its death.
type DragonDeathAction struct{ action }

// WitchMagicAction is a world.EntityAction that makes a witch display the
// magic particles around its hat.
type WitchMagicAction struct{ action }

// SilverfishMergeAction is a world.EntityAction that makes a silverfish
// display the particles of it hiding in a block.
type SilverfishMergeAction struct{ action }

// EatAction is a world.EntityAction that makes an entity display the eating particles at its mouth to viewers with the
// item in its hand being eaten.
type EatAction struct{ action }

// ArrowShakeAction makes an arrow entity display a shaking animation for the given duration.
type ArrowShakeAction struct {
	// Duration is the duration of the shake.
	Duration time.Duration

	action
}

// PickedUpAction is a world.EntityAction that makes an item get picked up by a collector. After this animation, the
// item disappears from viewers watching it.
type PickedUpAction struct {
	// Collector is the entity that collected the item.
	Collector world.Entity

	action
}

// FireworkExplosionAction is a world.EntityAction that makes a Firework rocket display an explosion particle.
type FireworkExplosionAction struct{ action }

// TotemUseAction is a world.EntityAction that displays the totem use particles and animation.
type TotemUseAction struct{ action }

// FishHookBubbleAction triggers the bubbling animation for a fishing hook.
type FishHookBubbleAction struct{ action }

// FishHookBiteAction triggers the hook time animation indicating a bite occurred.
type FishHookBiteAction struct{ action }

// FishHookTeaseAction triggers the tease animation when a fish is attracted to the hook.
type FishHookTeaseAction struct{ action }

// LoveHeartsAction shows the burst of hearts of an animal falling in love or
// breeding.
type LoveHeartsAction struct{ action }

// TameAction shows the hearts of a successful taming attempt, or the smoke of
// a failed one.
type TameAction struct {
	action
	// Succeeded is set if the taming succeeded.
	Succeeded bool
}

// DrinkAction shows a mob finishing a drink of a potion, or of milk.
type DrinkAction struct {
	action
	// Milk is set if the mob drank milk.
	Milk bool
}

// EatGrassAction shows a sheep lowering its head to eat grass.
type EatGrassAction struct{ action }

// action implements the Action interface. Structures in this package may embed it to gets its functionality
// out of the box.
type action struct{}

func (action) EntityAction() {}

// WolfShakeAction makes a wet wolf shake itself dry.
type WolfShakeAction struct{ action }

// VibrationDetectedAction makes the tendrils of a warden click as it hears a
// vibration.
type VibrationDetectedAction struct{ action }

// SquidInkAction makes a hurt squid squirt a cloud of ink.
type SquidInkAction struct{ action }
