package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// ambientSound is the minecraft:ambient_sound_interval of an actor: clients
// play its event every interval seconds plus a random part of the range.
type ambientSound struct {
	interval, spread float32
	event            string
}

// defaultAmbientSound is what the dedicated server sends of actors without
// an ambient sound of their own, projectiles included.
var defaultAmbientSound = ambientSound{8, 16, "ambient"}

// ambientSounds are the ambient sounds the dedicated server 1.26.52 sends
// of the actors whose sounds differ from the default.
var ambientSounds = map[string]ambientSound{
	"minecraft:allay":        {5, 5, "ambient"},
	"minecraft:bee":          {2, 3, "ambient.pollinate"},
	"minecraft:warden":       {2, 4, "ambient"},
	"minecraft:ghast":        {4, 16, "ambient"},
	"minecraft:happy_ghast":  {5, 16, "ambient"},
	"minecraft:horse":        {12, 16, "ambient"},
	"minecraft:axolotl":      {6, 16, "ambient"},
	"minecraft:camel":        {6, 16, "ambient"},
	"minecraft:camel_husk":   {6, 16, "ambient"},
	"minecraft:cat":          {6, 16, "ambient"},
	"minecraft:chicken":      {6, 16, "ambient"},
	"minecraft:cow":          {6, 16, "ambient"},
	"minecraft:dolphin":      {6, 16, "ambient"},
	"minecraft:fox":          {6, 16, "ambient"},
	"minecraft:frog":         {6, 16, "ambient"},
	"minecraft:glow_squid":   {6, 16, "ambient"},
	"minecraft:goat":         {6, 16, "ambient"},
	"minecraft:llama":        {6, 16, "ambient"},
	"minecraft:mooshroom":    {6, 16, "ambient"},
	"minecraft:ocelot":       {6, 16, "ambient"},
	"minecraft:panda":        {6, 16, "ambient"},
	"minecraft:parrot":       {6, 16, "ambient"},
	"minecraft:pig":          {6, 16, "ambient"},
	"minecraft:polar_bear":   {6, 16, "ambient"},
	"minecraft:rabbit":       {6, 16, "ambient"},
	"minecraft:sheep":        {6, 16, "ambient"},
	"minecraft:sniffer":      {6, 16, "ambient"},
	"minecraft:squid":        {6, 16, "ambient"},
	"minecraft:strider":      {6, 16, "ambient"},
	"minecraft:trader_llama": {6, 16, "ambient"},
	"minecraft:turtle":       {6, 16, "ambient"},
	"minecraft:wolf":         {6, 16, "ambient"},
}

// noFallDamage are the actors the dedicated server sends a fall damage
// multiplier of zero of.
var noFallDamage = map[string]bool{
	"minecraft:bat": true, "minecraft:bee": true, "minecraft:blaze": true, "minecraft:breeze": true, "minecraft:cat": true,
	"minecraft:chicken": true, "minecraft:copper_golem": true, "minecraft:ender_dragon": true, "minecraft:ghast": true,
	"minecraft:happy_ghast": true, "minecraft:iron_golem": true, "minecraft:magma_cube": true, "minecraft:ocelot": true,
	"minecraft:parrot": true, "minecraft:snow_golem": true,
}

// networkID returns the identifier clients know the entity type by.
func networkID(t world.EntityType) string {
	if v, ok := t.(NetworkEncodeableEntity); ok {
		return v.NetworkEncodeEntity()
	}
	return t.EncodeEntity()
}

// addAmbientSoundMetadata writes the ambient sound and fall damage
// multiplier clients use of an actor other than a player. A sleeping fox
// makes its sleeping sound instead.
func addAmbientSoundMetadata(e world.Entity, m protocol.EntityMetadata) {
	id := networkID(e.H().Type())
	if id == "minecraft:player" {
		return
	}
	sound, ok := ambientSounds[id]
	if !ok {
		sound = defaultAmbientSound
	}
	if s, ok := e.(interface{ Sleeping() bool }); ok && id == "minecraft:fox" && s.Sleeping() {
		sound = ambientSound{8, 16, "sleep"}
	}
	m[protocol.EntityDataKeyAmbientSoundInterval] = sound.interval
	m[protocol.EntityDataKeyAmbientSoundIntervalRange] = sound.spread
	m[protocol.EntityDataKeyAmbientSoundEventName] = sound.event
	multiplier := float32(1)
	if noFallDamage[id] {
		multiplier = 0
	}
	m[protocol.EntityDataKeyFallDamageMultiplier] = multiplier
}
