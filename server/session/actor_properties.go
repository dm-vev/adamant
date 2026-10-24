package session

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// actorProperty is an actor property as BDS declares it in SyncActorProperty.
// An actor sends the value of every property of its type, indexed by the
// position of the property in the list of the type.
type actorProperty struct {
	name string
	// enum holds the values of an enum property, which is sent as the index
	// of its value.
	enum []string
	// boolean is set for properties sent as 0 or 1.
	boolean bool
	// max is the largest value of an integer property, which starts at 0.
	max int32
	def int32
}

var (
	climateVariantProperty = actorProperty{name: "minecraft:climate_variant", enum: []string{"temperate", "warm", "cold"}}
	armadilloStates        = []string{"unrolled", "rolled_up", "rolled_up_peeking", "rolled_up_relaxing", "rolled_up_unrolling"}
)

// actorPropertyTypes holds the properties BDS 1.26.50 declares, in the order
// it sends them.
var actorPropertyTypes = map[string][]actorProperty{
	"minecraft:armadillo": {{name: "minecraft:armadillo_state", enum: armadilloStates}},
	"minecraft:bee":       {{name: "minecraft:has_nectar", boolean: true}},
	"minecraft:cat":       {{name: "minecraft:sound_variant", enum: []string{"default", "royal"}}},
	"minecraft:chicken":   {climateVariantProperty, {name: "minecraft:sound_variant", enum: []string{"default", "picky"}}},
	"minecraft:copper_golem": {
		{name: "minecraft:chest_interaction", enum: []string{"none", "take", "take_fail", "put", "put_fail"}},
		{name: "minecraft:has_flower", boolean: true},
		{name: "minecraft:oxidation_level", enum: []string{"unoxidized", "exposed", "weathered", "oxidized"}},
	},
	"minecraft:cow": {climateVariantProperty, {name: "minecraft:sound_variant", enum: []string{"default", "moody"}}},
	"minecraft:creaking": {
		{name: "minecraft:creaking_state", enum: []string{"neutral", "hostile_observed", "hostile_unobserved", "twitching", "crumbling"}},
		{name: "minecraft:creaking_swaying_ticks", max: 6},
	},
	"minecraft:egg":         {climateVariantProperty},
	"minecraft:happy_ghast": {{name: "minecraft:can_move", boolean: true, def: 1}},
	"minecraft:pig":         {climateVariantProperty, {name: "minecraft:sound_variant", enum: []string{"default", "big", "mini"}}},
	"minecraft:sulfur_cube": {{name: "minecraft:sulfur_cube_archetype", enum: []string{
		"none", "bouncy", "regular", "slow_bouncy", "slow_flat", "fast_flat", "light", "fast_sliding", "slow_sliding", "sticky", "high_resistance", "explosive", "hot",
	}}},
	"minecraft:wolf":            {{name: "minecraft:sound_variant", enum: []string{"default", "big", "cute", "grumpy", "mad", "puglin", "sad"}}},
	"minecraft:zombie_nautilus": {{name: "minecraft:variant", enum: []string{"default", "coral"}}},
}

// bedrockSoundVariants renames the sound variants that Bedrock calls
// differently.
var bedrockSoundVariants = map[string]string{"classic": "default", "angry": "mad"}

// declaration returns the property as SyncActorProperty carries it.
func (p actorProperty) declaration() map[string]any {
	switch {
	case p.enum != nil:
		values := make([]any, len(p.enum))
		for i, v := range p.enum {
			values[i] = v
		}
		return map[string]any{"name": p.name, "type": int32(3), "enum": values}
	case p.boolean:
		return map[string]any{"name": p.name, "type": int32(2)}
	}
	return map[string]any{"name": p.name, "type": int32(0), "min": int32(0), "max": p.max}
}

// value converts the value of the property as an entity reports it to the
// value sent to clients. Unknown values leave the default.
func (p actorProperty) value(v any) int32 {
	switch v := v.(type) {
	case string:
		for i, name := range p.enum {
			if name == v {
				return int32(i)
			}
		}
	case bool:
		return int32(boolByte(v))
	case int32:
		if v >= 0 && (p.enum != nil && int(v) < len(p.enum) || p.enum == nil && v <= p.max) {
			return v
		}
	}
	return p.def
}

// sendActorProperties declares the properties of the registered actor types
// that have any, as BDS does before a player spawns.
func (s *Session) sendActorProperties(w *world.World) {
	sent := make(map[string]bool)
	for _, t := range w.EntityRegistry().Types() {
		id := networkID(t)
		props, ok := actorPropertyTypes[id]
		if !ok || sent[id] {
			continue
		}
		sent[id] = true
		declarations := make([]any, len(props))
		for i, p := range props {
			declarations[i] = p.declaration()
		}
		s.writePacket(&packet.SyncActorProperty{PropertyData: map[string]any{"type": id, "properties": declarations}})
	}
}

// entityProperties returns the values of all properties of the type of an
// entity.
func entityProperties(e world.Entity) protocol.EntityProperties {
	props, ok := actorPropertyTypes[networkID(e.H().Type())]
	if !ok {
		return protocol.EntityProperties{}
	}
	values := make(map[string]any)
	actorPropertyValues(e, values)
	if beh, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
		actorPropertyValues(beh.Behaviour(), values)
	}
	integers := make([]protocol.IntegerEntityProperty, len(props))
	for i, p := range props {
		v := p.def
		if reported, ok := values[p.name]; ok {
			v = p.value(reported)
		}
		integers[i] = protocol.IntegerEntityProperty{Index: uint32(i), Value: v}
	}
	return protocol.EntityProperties{IntegerProperties: integers}
}

type creakingState interface {
	CreakingState() (state int32, swayTicks int32)
}

// actorPropertyValues adds the property values that an entity or its
// behaviour reports to values, by property name.
func actorPropertyValues(e any, values map[string]any) {
	if c, ok := e.(interface{ ClimateVariant() string }); ok {
		values["minecraft:climate_variant"] = c.ClimateVariant()
	}
	if sv, ok := e.(interface{ SoundVariant() string }); ok {
		name := sv.SoundVariant()
		if renamed, ok := bedrockSoundVariants[name]; ok {
			name = renamed
		}
		values["minecraft:sound_variant"] = name
	}
	if c, ok := e.(creakingState); ok {
		state, swayTicks := c.CreakingState()
		values["minecraft:creaking_state"], values["minecraft:creaking_swaying_ticks"] = state, swayTicks
	}
	switch e := e.(type) {
	case *entity.Bee:
		values["minecraft:has_nectar"] = e.HasNectar()
	case *entity.Armadillo:
		values["minecraft:armadillo_state"] = e.BedrockState()
	case *entity.CopperGolem:
		values["minecraft:chest_interaction"] = int32(e.State())
		values["minecraft:has_flower"] = !e.Antenna().Empty()
		values["minecraft:oxidation_level"] = int32(e.WeatherState())
	case *entity.SulfurCube:
		values["minecraft:sulfur_cube_archetype"] = e.Archetype()
	case *entity.ZombieNautilus:
		if e.Variant() == "warm" {
			values["minecraft:variant"] = "coral"
		}
	}
}
