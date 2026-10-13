package builtin

import (
	"fmt"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	_ "github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

type effectCommand struct {
	opGate
	Give      cmd.SubCommand       `cmd:"give"`
	Targets   []cmd.Target         `cmd:"targets"`
	Effect    string               `cmd:"effect"`
	Duration  cmd.Optional[string] `cmd:"seconds"`
	Amplifier cmd.Optional[int]    `cmd:"amplifier"`
	Hide      cmd.Optional[bool]   `cmd:"hideParticles"`
}
type clearEffectCommand struct {
	opGate
	Clear   cmd.SubCommand             `cmd:"clear"`
	Targets cmd.Optional[[]cmd.Target] `cmd:"targets"`
	Effect  cmd.Optional[string]       `cmd:"effect"`
}
type enchantCommand struct {
	worldOpCommand
	Targets     []cmd.Target      `cmd:"targets"`
	Enchantment string            `cmd:"enchantment"`
	Level       cmd.Optional[int] `cmd:"level"`
}

func registerPlayerEffectCommands(srv serverAdapter) {
	cmd.Register(cmd.New("effect", "Applies or clears effects.", nil, effectCommand{}, clearEffectCommand{}))
	cmd.Register(cmd.New("enchant", "Adds an enchantment to the held item.", nil, enchantCommand{worldOpCommand: worldOpCommand{srv}}))
}

func effectType(name string) effect.Type {
	name = strings.TrimPrefix(strings.ToLower(name), "minecraft:")
	names := map[string]effect.Type{
		"speed": effect.Speed, "slowness": effect.Slowness, "haste": effect.Haste, "mining_fatigue": effect.MiningFatigue,
		"strength": effect.Strength, "instant_health": effect.InstantHealth, "instant_damage": effect.InstantDamage,
		"jump_boost": effect.JumpBoost, "nausea": effect.Nausea, "regeneration": effect.Regeneration,
		"resistance": effect.Resistance, "fire_resistance": effect.FireResistance, "water_breathing": effect.WaterBreathing,
		"invisibility": effect.Invisibility, "blindness": effect.Blindness, "night_vision": effect.NightVision,
		"hunger": effect.Hunger, "weakness": effect.Weakness, "poison": effect.Poison, "wither": effect.Wither,
		"health_boost": effect.HealthBoost, "absorption": effect.Absorption, "saturation": effect.Saturation,
		"levitation": effect.Levitation, "fatal_poison": effect.FatalPoison, "conduit_power": effect.ConduitPower,
		"slow_falling": effect.SlowFalling, "bad_omen": effect.BadOmen, "hero_of_the_village": effect.HeroOfTheVillage,
		"darkness": effect.Darkness, "raid_omen": effect.RaidOmen, "trial_omen": effect.TrialOmen,
	}
	return names[name]
}

func targetsOf(targets []cmd.Target) []entity.Living {
	out := make([]entity.Living, 0, len(targets))
	for _, target := range targets {
		if l, ok := target.(entity.Living); ok {
			out = append(out, l)
		}
	}
	return out
}
func (c effectCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	t := effectType(c.Effect)
	lt, ok := t.(effect.LastingType)
	if !ok || t == nil {
		o.Errort(cmd.MessageParameterInvalid, c.Effect)
		return
	}
	living := targetsOf(c.Targets)
	if len(living) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	seconds, present := c.Duration.Load()
	dur := time.Duration(30) * time.Second
	infinite := false
	if present {
		if strings.EqualFold(seconds, "infinite") {
			infinite = true
		} else {
			var n int
			if _, err := fmt.Sscanf(seconds, "%d", &n); err != nil || n < 1 {
				o.Errort(cmd.MessageParameterInvalid, seconds)
				return
			}
			dur = time.Duration(n) * time.Second
		}
	}
	level, ok := c.Amplifier.Load()
	if !ok {
		level = 0
	}
	level++
	hide, _ := c.Hide.Load()
	for _, l := range living {
		eff := effect.New(lt, level, dur)
		if infinite {
			eff = effect.NewInfinite(lt, level)
		}
		if hide {
			eff = eff.WithoutParticles()
		}
		l.AddEffect(eff)
	}
	o.Printf("Applied effect %s to %d targets.", c.Effect, len(living))
}
func (c clearEffectCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	targets, ok := c.Targets.Load()
	if !ok {
		if p, yes := src.(*player.Player); yes {
			targets = []cmd.Target{p}
		}
	}
	living := targetsOf(targets)
	if len(living) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	var t effect.Type
	if name, ok := c.Effect.Load(); ok {
		t = effectType(name)
		if t == nil {
			o.Errort(cmd.MessageParameterInvalid, name)
			return
		}
	}
	for _, l := range living {
		if t != nil {
			l.RemoveEffect(t)
		} else {
			for _, e := range l.Effects() {
				l.RemoveEffect(e.Type())
			}
		}
	}
	o.Printf("Cleared effects from %d targets.", len(living))
}

func enchantmentType(name string) item.EnchantmentType {
	name = strings.TrimPrefix(strings.ToLower(name), "minecraft:")
	for _, e := range item.Enchantments() {
		if strings.ToLower(strings.ReplaceAll(e.Name(), " ", "_")) == name {
			return e
		}
	}
	return nil
}
func (c enchantCommand) Allow(src cmd.Source) bool { return c.worldOpCommand.Allow(src) }
func (c enchantCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	e := enchantmentType(c.Enchantment)
	if e == nil {
		o.Errort(cmd.MessageParameterInvalid, c.Enchantment)
		return
	}
	level, ok := c.Level.Load()
	if !ok {
		level = 1
	}
	if level < 1 || level > e.MaxLevel() {
		o.Errort(cmd.MessageParameterInvalid, level)
		return
	}
	changed := 0
	for _, target := range c.Targets {
		p, ok := target.(*player.Player)
		if !ok {
			continue
		}
		main, off := p.HeldItems()
		if main.Empty() || !e.CompatibleWithItem(main.Item()) {
			continue
		}
		next := main.WithEnchantments(item.NewEnchantment(e, level))
		if len(next.Enchantments()) == len(main.Enchantments()) {
			continue
		}
		p.SetHeldItems(next, off)
		changed++
	}
	if changed == 0 {
		o.Errort(cmd.MessageParameterInvalid, c.Enchantment)
		return
	}
	o.Printf("Enchanted items for %d targets.", changed)
}
