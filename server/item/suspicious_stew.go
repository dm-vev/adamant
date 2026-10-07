package item

import (
	"time"

	"github.com/df-mc/dragonfly/server/world"
)

const suspiciousStewDurationNBT = "java26_stew_effect_duration"

// NewSuspiciousStewStack returns a one-count stew stack with the specified
// Java effect duration.
func NewSuspiciousStewStack(stew StewType) Stack {
	return NewStack(SuspiciousStew{Type: stew}, 1)
}

// SuspiciousStew is a food item that can give the player a status effect that depends on the flower used to craft it.
type SuspiciousStew struct {
	defaultFood

	// Type specifies the type of effect will be given to the player.
	Type StewType
}

// MaxCount returns the maximum stack size for suspicious stew.
func (SuspiciousStew) MaxCount() int { return 1 }

// AlwaysConsumable reports that stew can always be consumed.
func (SuspiciousStew) AlwaysConsumable() bool { return true }

// EncodeItem returns the Bedrock item identifier and effect variant.
func (s SuspiciousStew) EncodeItem() (name string, meta int16) {
	return "minecraft:suspicious_stew", int16(s.Type.Uint8())
}

// EncodeNBT persists a custom Java stew effect duration.
func (s SuspiciousStew) EncodeNBT() map[string]any {
	if s.Type.duration <= 0 {
		return nil
	}
	return map[string]any{suspiciousStewDurationNBT: int64(s.Type.duration)}
}

// DecodeNBT restores a custom Java stew effect duration.
func (s SuspiciousStew) DecodeNBT(data map[string]any) any {
	var duration int64
	switch value := data[suspiciousStewDurationNBT].(type) {
	case int64:
		duration = value
	case int32:
		duration = int64(value)
	case int:
		duration = int64(value)
	}
	if duration > 0 {
		s.Type = s.Type.WithDuration(time.Duration(duration))
	}
	return s
}

// Consume applies the stew effect and returns a bowl.
func (s SuspiciousStew) Consume(_ *world.Tx, c Consumer) Stack {
	for _, effect := range s.Type.Effects() {
		c.AddEffect(effect)
	}
	c.Saturate(6, 7.2)
	return NewStack(Bowl{}, 1)
}
