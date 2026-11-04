package entity

import (
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
)

// effectHolder is implemented by the behaviours of mobs with status effects.
type effectHolder interface {
	effectManager() *EffectManager
}

// encodeMobEffects writes the status effects of a mob as Bedrock's
// ActiveEffects. Mobs without effects write an empty list, so that effects
// kept in the native NBT of the entity do not come back.
func encodeMobEffects(data *world.EntityData) map[string]any {
	holder, ok := data.Data.(effectHolder)
	if !ok {
		return nil
	}
	effects := holder.effectManager().Effects()
	list := make([]any, 0, len(effects))
	for _, e := range effects {
		if entry, ok := encodeEffectEntry(e); ok {
			list = append(list, entry)
		}
	}
	return map[string]any{"ActiveEffects": list}
}

// encodeEffectEntry writes an effect as an entry of Bedrock's ActiveEffects
// or an area effect cloud's mobEffects.
func encodeEffectEntry(e effect.Effect) (map[string]any, bool) {
	id, ok := effect.ID(e.Type())
	if !ok {
		return nil, false
	}
	ticks := int32(e.Duration() / (time.Second / 20))
	if e.Infinite() {
		ticks = -1
	}
	return map[string]any{
		"Id": uint8(id), "Amplifier": uint8(e.Level() - 1), "Ambient": boolByte(e.Ambient()),
		"ShowParticles": boolByte(!e.ParticlesHidden()), "DisplayOnScreenTextureAnimation": uint8(0),
		"Duration": ticks, "DurationEasy": ticks, "DurationNormal": ticks, "DurationHard": ticks,
	}, true
}

// decodeMobEffects restores the ActiveEffects of a mob. They start on the
// first tick of the mob, like effects a mob spawns with.
func decodeMobEffects(data *world.EntityData, m map[string]any) {
	holder, ok := data.Data.(effectHolder)
	if !ok {
		return
	}
	manager := holder.effectManager()
	for _, raw := range nbtconv.Slice(m, "ActiveEffects") {
		if e, ok := decodeEffectEntry(raw); ok {
			manager.restore(e)
		}
	}
}

// decodeEffectEntry reads a lasting effect written by encodeEffectEntry.
func decodeEffectEntry(raw any) (effect.Effect, bool) {
	entry, ok := raw.(map[string]any)
	if !ok {
		return effect.Effect{}, false
	}
	t, ok := effect.ByID(int(nbtconv.Uint8(entry, "Id")))
	if !ok {
		return effect.Effect{}, false
	}
	lasting, ok := t.(effect.LastingType)
	if !ok {
		return effect.Effect{}, false
	}
	level := int(nbtconv.Uint8(entry, "Amplifier")) + 1
	ticks := nbtconv.Int32(entry, "Duration")
	var e effect.Effect
	switch {
	case ticks < 0:
		e = effect.NewInfinite(lasting, level)
	case nbtconv.Bool(entry, "Ambient"):
		e = effect.NewAmbient(lasting, level, time.Duration(ticks)*time.Second/20)
	default:
		e = effect.New(lasting, level, time.Duration(ticks)*time.Second/20)
	}
	if _, shown := entry["ShowParticles"]; shown && !nbtconv.Bool(entry, "ShowParticles") {
		e = e.WithoutParticles()
	}
	return e, true
}
