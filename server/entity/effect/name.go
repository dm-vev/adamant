package effect

import "strings"

// names holds the effects by their names: the identifiers of Java's
// mob_effect registry without the minecraft namespace, and fatal_poison,
// Bedrock's effect that Java has none of.
var names = map[string]Type{
	"speed": Speed, "slowness": Slowness, "haste": Haste, "mining_fatigue": MiningFatigue,
	"strength": Strength, "instant_health": InstantHealth, "instant_damage": InstantDamage,
	"jump_boost": JumpBoost, "nausea": Nausea, "regeneration": Regeneration,
	"resistance": Resistance, "fire_resistance": FireResistance, "water_breathing": WaterBreathing,
	"invisibility": Invisibility, "blindness": Blindness, "night_vision": NightVision,
	"hunger": Hunger, "weakness": Weakness, "poison": Poison, "wither": Wither,
	"health_boost": HealthBoost, "absorption": Absorption, "saturation": Saturation,
	"levitation": Levitation, "fatal_poison": FatalPoison, "conduit_power": ConduitPower,
	"slow_falling": SlowFalling, "bad_omen": BadOmen, "hero_of_the_village": HeroOfTheVillage,
	"darkness": Darkness, "raid_omen": RaidOmen, "trial_omen": TrialOmen,
	"wind_charged": WindCharged, "weaving": Weaving, "oozing": Oozing,
	"infested": Infested, "breath_of_the_nautilus": BreathOfTheNautilus,
}

// typeNames is the inverse of names.
var typeNames = func() map[Type]string {
	m := make(map[Type]string, len(names))
	for name, t := range names {
		m[t] = name
	}
	return m
}()

// Name returns the name of an effect type, such as "speed" or
// "hero_of_the_village", and false if the type has none, as effects
// registered by plugins do.
func Name(t Type) (string, bool) {
	name, ok := typeNames[t]
	return name, ok
}

// ByName returns the effect type of a name, which may carry the minecraft
// namespace, and false if no effect has that name. Names are matched as they
// are written, in lower case.
func ByName(name string) (Type, bool) {
	t, ok := names[strings.TrimPrefix(name, "minecraft:")]
	return t, ok
}
