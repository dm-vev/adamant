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

func (b *AllayBehaviour) effectManager() *EffectManager           { return b.effects }
func (b *AquaticMobBehaviour) effectManager() *EffectManager      { return b.effects }
func (b *ArmadilloBehaviour) effectManager() *EffectManager       { return b.effects }
func (b *batBehaviour) effectManager() *EffectManager             { return b.effects }
func (b *beeBehaviour) effectManager() *EffectManager             { return b.effects }
func (b *breezeState) effectManager() *EffectManager              { return b.effects }
func (b *CatBehaviour) effectManager() *EffectManager             { return b.effects }
func (b *ChickenBehaviour) effectManager() *EffectManager         { return b.effects }
func (b *CopperGolemBehaviour) effectManager() *EffectManager     { return b.effects }
func (b *CowBehaviour) effectManager() *EffectManager             { return b.effects }
func (b *creakingState) effectManager() *EffectManager            { return b.effects }
func (b *foxState) effectManager() *EffectManager                 { return b.effects }
func (b *frogBehaviour) effectManager() *EffectManager            { return b.effects }
func (b *GoatBehaviour) effectManager() *EffectManager            { return b.effects }
func (b *golemState) effectManager() *EffectManager               { return b.effects }
func (b *happyGhastBehaviour) effectManager() *EffectManager      { return b.effects }
func (b *hostileState) effectManager() *EffectManager             { return b.effects }
func (b *MooshroomBehaviour) effectManager() *EffectManager       { return b.effects }
func (b *MountBehaviour) effectManager() *EffectManager           { return b.effects }
func (b *netherBeastState) effectManager() *EffectManager         { return b.effects }
func (b *netherState) effectManager() *EffectManager              { return b.effects }
func (b *oceanBehaviour) effectManager() *EffectManager           { return b.effects }
func (b *oceanWildlifeBehaviour) effectManager() *EffectManager   { return b.effects }
func (b *ocelotBehaviour) effectManager() *EffectManager          { return b.effects }
func (b *pandaState) effectManager() *EffectManager               { return b.effects }
func (b *parrotBehaviour) effectManager() *EffectManager          { return b.effects }
func (b *PigBehaviour) effectManager() *EffectManager             { return b.effects }
func (b *piglinState) effectManager() *EffectManager              { return b.effects }
func (b *polarBearState) effectManager() *EffectManager           { return b.effects }
func (b *RabbitBehaviour) effectManager() *EffectManager          { return b.effects }
func (b *SheepBehaviour) effectManager() *EffectManager           { return b.effects }
func (b *ShulkerBehaviour) effectManager() *EffectManager         { return b.effects }
func (b *SnifferBehaviour) effectManager() *EffectManager         { return b.effects }
func (b *speciesState) effectManager() *EffectManager             { return b.effects }
func (b *striderState) effectManager() *EffectManager             { return b.effects }
func (b *SulfurCubeBehaviour) effectManager() *EffectManager      { return b.effects }
func (b *VillagerBehaviour) effectManager() *EffectManager        { return b.effects }
func (b *wanderingTraderBehaviour) effectManager() *EffectManager { return b.effects }
func (b *wardenState) effectManager() *EffectManager              { return b.effects }
func (b *phantomState) effectManager() *EffectManager             { return b.effects }
func (b *witherState) effectManager() *EffectManager              { return b.effects }
func (b *WitchBehaviour) effectManager() *EffectManager           { return b.effects }
func (b *WolfBehaviour) effectManager() *EffectManager            { return b.effects }
func (b *zombifiedPiglinState) effectManager() *EffectManager     { return b.effects }

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
		id, ok := effect.ID(e.Type())
		if !ok {
			continue
		}
		ticks := int32(e.Duration() / (time.Second / 20))
		if e.Infinite() {
			ticks = -1
		}
		list = append(list, map[string]any{
			"Id": uint8(id), "Amplifier": uint8(e.Level() - 1), "Ambient": boolByte(e.Ambient()),
			"ShowParticles": boolByte(!e.ParticlesHidden()), "DisplayOnScreenTextureAnimation": uint8(0),
			"Duration": ticks, "DurationEasy": ticks, "DurationNormal": ticks, "DurationHard": ticks,
		})
	}
	return map[string]any{"ActiveEffects": list}
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
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		t, ok := effect.ByID(int(nbtconv.Uint8(entry, "Id")))
		if !ok {
			continue
		}
		lasting, ok := t.(effect.LastingType)
		if !ok {
			continue
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
		manager.restore(e)
	}
}
