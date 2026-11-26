package session

import (
	"cmp"
	"slices"
	"strconv"

	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/world"
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

// stringValue converts a string enum value to the wire integer index, or p.def
// if unknown.
func (p actorProperty) stringValue(v string) int32 {
	for i, name := range p.enum {
		if name == v {
			return int32(i)
		}
	}
	return p.def
}

// int32Value validates an integer/enum property value, returning p.def if out
// of range.
func (p actorProperty) int32Value(v int32) int32 {
	if v >= 0 && (p.enum != nil && int(v) < len(p.enum) || p.enum == nil && v <= p.max) {
		return v
	}
	return p.def
}

// sendActorProperties declares the properties of the registered actor types
// that have any, as BDS does before a player spawns, in the order BDS sends
// them (see actorTypesInOrder).
func (s *Session) sendActorProperties(w *world.World) {
	sent := make(map[string]bool)
	for _, id := range actorTypesInOrder(w.EntityRegistry(), bdsActorPropertyRank) {
		props, ok := actorPropertyTypes[id]
		if !ok || sent[id] {
			continue
		}
		sent[id] = true
		declarations := make([]any, len(props))
		for i, p := range props {
			declarations[i] = p.declaration()
		}
		s.writePacket(&mc.SyncActorPropertyPacket{PropertyData: map[string]any{"type": id, "properties": declarations}})
	}
}

// entityProperties returns the values of all properties of the type of an
// entity.
func entityProperties(e world.Entity) mc.EntityProperties {
	t := e.H().Type()
	if t == nil {
		return mc.EntityProperties{}
	}
	return entityPropertiesWith(e, entityTypeMetaOf(t), metaCapsOf(e))
}

func entityPropertiesWith(e world.Entity, meta *entityTypeMeta, caps metaCaps) mc.EntityProperties {
	var beh any
	var behCaps metaCaps
	if caps.hi&capBehaviour != 0 {
		beh = e.(behaviourHolder).Behaviour()
		behCaps = metaCapsOf(beh)
	}
	hasPatch := mayHaveMetaPatch(e, caps, beh, behCaps)
	var props []actorProperty
	if meta != nil {
		props = meta.props
	}
	if len(props) == 0 && !hasPatch {
		return mc.EntityProperties{}
	}
	var intProps []mc.IntegerEntityProperty
	var isDefault bool
	if len(props) > 0 {
		intProps, isDefault = baseIntegerProperties(e, meta, caps, beh, behCaps)
	}
	if !hasPatch {
		return mc.EntityProperties{IntegerProperties: intProps}
	}
	patch, ok := collectEntityMetaPatch(e, caps, beh, behCaps)
	if !ok || (len(patch.CustomStrings) == 0 && len(patch.CustomInts) == 0 && len(patch.CustomFloats) == 0) {
		return mc.EntityProperties{IntegerProperties: intProps}
	}
	return applyMetaPatchProperties(props, intProps, isDefault, &patch)
}

func baseIntegerProperties(e world.Entity, meta *entityTypeMeta, caps metaCaps, beh any, behCaps metaCaps) ([]mc.IntegerEntityProperty, bool) {
	props := meta.props
	if len(props) > 8 {
		out := make([]mc.IntegerEntityProperty, len(props))
		copy(out, meta.defaultProps)
		fillActorPropertiesWith(e, props, out, caps)
		if beh != nil {
			fillActorPropertiesWith(beh, props, out, behCaps)
		}
		return out, false
	}
	var stackBuf [8]mc.IntegerEntityProperty
	buf := stackBuf[:len(props)]
	copy(buf, meta.defaultProps)
	fillActorPropertiesWith(e, props, buf, caps)
	if beh != nil {
		fillActorPropertiesWith(beh, props, buf, behCaps)
	}
	if slices.Equal(buf, meta.defaultProps) {
		return meta.defaultProps, true
	}
	out := make([]mc.IntegerEntityProperty, len(props))
	copy(out, buf)
	return out, false
}

func applyMetaPatchProperties(props []actorProperty, intProps []mc.IntegerEntityProperty, isDefault bool, patch *world.ActorMetaPatch) mc.EntityProperties {
	ints := intProps
	ensureMutableInts := func() {
		if isDefault {
			ints = slices.Clone(intProps)
			isDefault = false
		}
	}
	for k, v := range patch.CustomStrings {
		if i := actorPropertyIndex(props, k); i >= 0 {
			ensureMutableInts()
			if props[i].enum != nil {
				ints[i].Value = props[i].stringValue(v)
			} else if props[i].boolean {
				ints[i].Value = int32(boolByte(v == "true" || v == "1"))
			}
		}
	}
	var extraInts []string
	for k, v := range patch.CustomInts {
		if i := actorPropertyIndex(props, k); i >= 0 {
			ensureMutableInts()
			if props[i].boolean {
				ints[i].Value = int32(boolByte(v != 0))
			} else {
				ints[i].Value = props[i].int32Value(v)
			}
			continue
		}
		extraInts = append(extraInts, k)
	}
	if len(extraInts) > 0 {
		ensureMutableInts()
		ints = mergeExtraIntProperties(ints, uint32(len(props)), extraInts, patch.CustomInts)
	}
	var floats []mc.FloatEntityProperty
	if len(patch.CustomFloats) > 0 {
		floats = buildCustomFloatProperties(patch.CustomFloats)
	}
	return mc.EntityProperties{IntegerProperties: ints, FloatProperties: floats}
}

func actorPropertyIndex(props []actorProperty, name string) int {
	for i, p := range props {
		if p.name == name {
			return i
		}
	}
	return -1
}

func mergeExtraIntProperties(ints []mc.IntegerEntityProperty, baseNext uint32, keys []string, src map[string]int32) []mc.IntegerEntityProperty {
	used := make(map[uint32]bool, len(ints)+len(keys))
	for _, p := range ints {
		used[p.Index] = true
	}
	var named []string
	for _, k := range keys {
		if idx, err := strconv.ParseUint(k, 10, 32); err == nil {
			u := uint32(idx)
			val := src[k]
			if used[u] {
				for i := range ints {
					if ints[i].Index == u {
						ints[i].Value = val
						break
					}
				}
			} else {
				used[u] = true
				ints = append(ints, mc.IntegerEntityProperty{Index: u, Value: val})
			}
			continue
		}
		named = append(named, k)
	}
	slices.Sort(named)
	nextIdx := baseNext
	for _, k := range named {
		for used[nextIdx] {
			nextIdx++
		}
		used[nextIdx] = true
		ints = append(ints, mc.IntegerEntityProperty{Index: nextIdx, Value: src[k]})
	}
	slices.SortFunc(ints, func(a, b mc.IntegerEntityProperty) int { return cmp.Compare(a.Index, b.Index) })
	return ints
}

func buildCustomFloatProperties(src map[string]float32) []mc.FloatEntityProperty {
	used := make(map[uint32]bool, len(src))
	floats := make([]mc.FloatEntityProperty, 0, len(src))
	var named []string
	for k, v := range src {
		if idx, err := strconv.ParseUint(k, 10, 32); err == nil {
			u := uint32(idx)
			used[u] = true
			floats = append(floats, mc.FloatEntityProperty{Index: u, Value: v})
			continue
		}
		named = append(named, k)
	}
	slices.Sort(named)
	var nextIdx uint32
	for _, k := range named {
		for used[nextIdx] {
			nextIdx++
		}
		used[nextIdx] = true
		floats = append(floats, mc.FloatEntityProperty{Index: nextIdx, Value: src[k]})
	}
	slices.SortFunc(floats, func(a, b mc.FloatEntityProperty) int { return cmp.Compare(a.Index, b.Index) })
	return floats
}

type creakingState interface {
	CreakingState() (state int32, swayTicks int32)
}

func setActorPropertyString(props []actorProperty, out []mc.IntegerEntityProperty, name, v string) {
	for i, p := range props {
		if p.name == name {
			out[i].Value = p.stringValue(v)
			return
		}
	}
}

func setActorPropertyBool(props []actorProperty, out []mc.IntegerEntityProperty, name string, v bool) {
	for i, p := range props {
		if p.name == name {
			out[i].Value = int32(boolByte(v))
			return
		}
	}
}

func setActorPropertyInt32(props []actorProperty, out []mc.IntegerEntityProperty, name string, v int32) {
	for i, p := range props {
		if p.name == name {
			out[i].Value = p.int32Value(v)
			return
		}
	}
}

// fillActorPropertiesWith writes the property values that an entity or its
// behaviour reports directly into out without intermediate map allocation.
func fillActorPropertiesWith(e any, props []actorProperty, out []mc.IntegerEntityProperty, caps metaCaps) {
	if caps.hi&maskPropCaps == 0 {
		return
	}
	if caps.hi&capClimateVariant != 0 {
		setActorPropertyString(props, out, "minecraft:climate_variant", e.(climateVariantMeta).ClimateVariant())
	}
	if caps.hi&capSoundVariant != 0 {
		name := e.(soundVariantMeta).SoundVariant()
		switch name {
		case "classic":
			name = "default"
		case "angry":
			name = "mad"
		}
		setActorPropertyString(props, out, "minecraft:sound_variant", name)
	}
	if caps.hi&capCreakingState != 0 {
		state, swayTicks := e.(creakingState).CreakingState()
		setActorPropertyInt32(props, out, "minecraft:creaking_state", state)
		setActorPropertyInt32(props, out, "minecraft:creaking_swaying_ticks", swayTicks)
	}
	if caps.hi&capPropEntity == 0 {
		return
	}
	switch e := e.(type) {
	case *entity.Bee:
		setActorPropertyBool(props, out, "minecraft:has_nectar", e.HasNectar())
	case *entity.Armadillo:
		setActorPropertyString(props, out, "minecraft:armadillo_state", e.BedrockState())
	case *entity.CopperGolem:
		setActorPropertyInt32(props, out, "minecraft:chest_interaction", int32(e.State()))
		setActorPropertyBool(props, out, "minecraft:has_flower", !e.Antenna().Empty())
		setActorPropertyInt32(props, out, "minecraft:oxidation_level", int32(e.WeatherState()))
	case *entity.HappyGhast:
		setActorPropertyBool(props, out, "minecraft:can_move", !e.StaysStill())
	case *entity.SulfurCube:
		setActorPropertyString(props, out, "minecraft:sulfur_cube_archetype", e.Archetype())
	case *entity.ZombieNautilus:
		if e.Variant() == "warm" {
			setActorPropertyString(props, out, "minecraft:variant", "coral")
		}
	}
}
