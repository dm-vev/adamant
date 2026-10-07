package item

import (
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
)

// OminousBottle applies Bad Omen when consumed. Amplifier is the Java
// minecraft:ominous_bottle_amplifier component, whose valid range is 0–4.
type OminousBottle struct {
	Amplifier int
}

// MaxCount returns the maximum stack size for the item.
func (OminousBottle) MaxCount() int { return 64 }

// AlwaysConsumable allows drinking without needing a hunger deficit.
func (OminousBottle) AlwaysConsumable() bool { return true }

// ConsumeDuration returns Java's default 1.6 second drink duration.
func (OminousBottle) ConsumeDuration() time.Duration { return time.Second * 8 / 5 }

// Consume applies Bad Omen for Java's 120,000-tick duration and returns an
// empty glass bottle.
func (b OminousBottle) Consume(_ *world.Tx, c Consumer) Stack {
	c.AddEffect(effect.New(effect.BadOmen, b.Amplifier+1, 100*time.Minute).WithoutParticles())
	return NewStack(GlassBottle{}, 1)
}

// EncodeItem returns the Bedrock item identifier.
func (OminousBottle) EncodeItem() (name string, meta int16) { return "minecraft:ominous_bottle", 0 }
