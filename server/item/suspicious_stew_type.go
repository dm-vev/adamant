package item

import (
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
)

// StewType represents a type of suspicious stew.
type StewType struct {
	stewType
	duration time.Duration
}

// NightVisionPoppyStew returns suspicious stew night vision effect.
func NightVisionPoppyStew() StewType {
	return StewType{stewType: 0}
}

// JumpBoostStew returns suspicious stew jump boost effect.
func JumpBoostStew() StewType {
	return StewType{stewType: 1}
}

// WeaknessStew returns suspicious stew weakness effect.
func WeaknessStew() StewType {
	return StewType{stewType: 2}
}

// BlindnessBluetStew returns suspicious stew blindness effect.
func BlindnessBluetStew() StewType {
	return StewType{stewType: 3}
}

// PoisonStew returns suspicious stew poison effect.
func PoisonStew() StewType {
	return StewType{stewType: 4}
}

// SaturationDandelionStew returns suspicious stew saturation effect.
func SaturationDandelionStew() StewType {
	return StewType{stewType: 5}
}

// SaturationOrchidStew returns suspicious stew saturation effect.
func SaturationOrchidStew() StewType {
	return StewType{stewType: 6}
}

// FireResistanceStew returns suspicious stew fire resistance effect.
func FireResistanceStew() StewType {
	return StewType{stewType: 7}
}

// RegenerationStew returns suspicious stew regeneration effect.
func RegenerationStew() StewType {
	return StewType{stewType: 8}
}

// WitherStew returns suspicious stew wither effect.
func WitherStew() StewType {
	return StewType{stewType: 9}
}

// NightVisionTorchflowerStew returns suspicious stew night vision effect.
func NightVisionTorchflowerStew() StewType {
	return StewType{stewType: 10}
}

// BlindnessEyeblossomStew returns suspicious stew blindness effect.
func BlindnessEyeblossomStew() StewType {
	return StewType{stewType: 11}
}

// NauseaStew returns suspicious stew nausea effect.
func NauseaStew() StewType {
	return StewType{stewType: 12}
}

// StewTypes ...
func StewTypes() []StewType {
	return []StewType{NightVisionPoppyStew(), JumpBoostStew(), WeaknessStew(), BlindnessBluetStew(), PoisonStew(), SaturationDandelionStew(), SaturationOrchidStew(), FireResistanceStew(), RegenerationStew(), WitherStew(), NightVisionTorchflowerStew(), BlindnessEyeblossomStew(), NauseaStew()}
}

// StewTypeFromJavaEffect returns the suspicious stew giving the Java mob
// effect passed, for loot and trades that set a stew effect.
func StewTypeFromJavaEffect(id string) (StewType, bool) {
	switch strings.TrimPrefix(id, "minecraft:") {
	case "night_vision":
		return NightVisionPoppyStew(), true
	case "jump_boost":
		return JumpBoostStew(), true
	case "weakness":
		return WeaknessStew(), true
	case "blindness":
		return BlindnessBluetStew(), true
	case "poison":
		return PoisonStew(), true
	case "saturation":
		return SaturationDandelionStew(), true
	case "fire_resistance":
		return FireResistanceStew(), true
	case "regeneration":
		return RegenerationStew(), true
	case "wither":
		return WitherStew(), true
	case "nausea":
		return NauseaStew(), true
	}
	return StewType{}, false
}

type stewType uint8

// Uint8 returns the stew as a uint8.
func (s stewType) Uint8() uint8 {
	return uint8(s)
}

// WithDuration returns a stew type with a custom Java effect duration.
func (s StewType) WithDuration(duration time.Duration) StewType {
	if duration <= 0 {
		panic("stew effect duration must be positive")
	}
	s.duration = duration
	return s
}

// Effects returns suspicious stew effects.
func (s StewType) Effects() []effect.Effect {
	var effects []effect.Effect
	switch s.Uint8() {
	case 0, 10:
		effects = append(effects, effect.New(effect.NightVision, 1, stewDuration(time.Second*5, s.duration)))
	case 1:
		effects = append(effects, effect.New(effect.JumpBoost, 1, stewDuration(time.Second*5, s.duration)))
	case 2:
		effects = append(effects, effect.New(effect.Weakness, 1, stewDuration(time.Second*7, s.duration)))
	case 3, 11:
		effects = append(effects, effect.New(effect.Blindness, 1, stewDuration(time.Second*6, s.duration)))
	case 4:
		effects = append(effects, effect.New(effect.Poison, 1, stewDuration(time.Second*11, s.duration)))
	case 5, 6:
		effects = append(effects, effect.New(effect.Saturation, 1, time.Second*3/10))
	case 7:
		effects = append(effects, effect.New(effect.FireResistance, 1, stewDuration(time.Second*3, s.duration)))
	case 8:
		effects = append(effects, effect.New(effect.Regeneration, 1, stewDuration(time.Second*7, s.duration)))
	case 9:
		effects = append(effects, effect.New(effect.Wither, 1, stewDuration(time.Second*7, s.duration)))
	case 12:
		effects = append(effects, effect.New(effect.Nausea, 1, stewDuration(time.Second*7, s.duration)))
	default:
		panic("should never happen")
	}

	return effects
}
func stewDuration(defaultDuration, override time.Duration) time.Duration {
	if override > 0 {
		return override
	}
	return defaultDuration
}
