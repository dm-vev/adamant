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
	gameMaster
	Give      cmd.SubCommand       `cmd:"give"`
	Targets   []cmd.Target         `cmd:"targets"`
	Effect    string               `cmd:"effect"`
	Duration  cmd.Optional[string] `cmd:"seconds"`
	Amplifier cmd.Optional[int]    `cmd:"amplifier"`
	Hide      cmd.Optional[bool]   `cmd:"hideParticles"`
}
type clearEffectCommand struct {
	gameMaster
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
	cmd.Register(newCommand("effect", nil, effectCommand{}, clearEffectCommand{}))
	cmd.Register(newCommand("enchant", nil, enchantCommand{worldOpCommand: worldOpCommand{srv}}))
}

func effectType(name string) effect.Type {
	t, _ := effect.ByName(strings.ToLower(name))
	return t
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
	name = "minecraft:" + strings.TrimPrefix(strings.ToLower(name), "minecraft:")
	for _, e := range item.Enchantments() {
		if item.JavaEnchantmentID(e) == name {
			return e
		}
	}
	return nil
}

// Run ports EnchantCommand.enchant: the enchantment goes on the main-hand item
// of each living target. A target that cannot take it fails the command with
// the reason only if it is the sole target, and the command fails with
// "Nothing happened" if no target took it.
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
	if level < 0 {
		o.Errort(cmd.MessageParameterInvalid, level)
		return
	}
	if level > e.MaxLevel() {
		o.Errorf("%d is higher than the maximum level of %d supported by that enchantment", level, e.MaxLevel())
		return
	}
	if len(c.Targets) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	single := len(c.Targets) == 1
	var enchanted []cmd.Target
	for _, target := range c.Targets {
		if _, living := target.(entity.Living); !living {
			if single {
				o.Errorf("%s is not a valid entity for this command", commandTargetDisplayName(target))
				return
			}
			continue
		}
		// An entity whose main hand /enchant can change.
		holder, ok := target.(interface {
			HeldItems() (main, off item.Stack)
			SetHeldItems(main, off item.Stack)
		})
		var main, off item.Stack
		if ok {
			main, off = holder.HeldItems()
		}
		if main.Empty() {
			if single {
				o.Errorf("%s is not holding any item", commandTargetDisplayName(target))
				return
			}
			continue
		}
		next, ok := enchantedMainHand(main, e, level)
		if !ok {
			if single {
				o.Errorf("%s cannot support that enchantment", stackDisplayName(main))
				return
			}
			continue
		}
		holder.SetHeldItems(next, off)
		enchanted = append(enchanted, target)
	}
	name := enchantmentFullName(e, level)
	switch len(enchanted) {
	case 0:
		o.Errorf("Nothing happened")
	case 1:
		o.Printf("Applied enchantment %s to %s's item", name, commandTargetDisplayName(enchanted[0]))
	default:
		o.Printf("Applied enchantment %s to %d entities", name, len(enchanted))
	}
}

// enchantedMainHand returns the stack with the enchantment put on it, as
// Enchantment.canEnchant, EnchantmentHelper.isEnchantmentCompatible and
// ItemStack.enchant do. An enchantment the stack has is not compatible with
// itself, as Enchantment.areCompatible refuses equal ones, and a level of 0
// changes nothing.
func enchantedMainHand(stack item.Stack, e item.EnchantmentType, level int) (item.Stack, bool) {
	if !e.CompatibleWithItem(stack.Item()) {
		return stack, false
	}
	for _, existing := range stack.Enchantments() {
		other := existing.Type()
		if other.Name() == e.Name() || !other.CompatibleWithEnchantment(e) || !e.CompatibleWithEnchantment(other) {
			return stack, false
		}
	}
	if level == 0 {
		return stack, true
	}
	return stack.WithEnchantments(item.NewEnchantment(e, level)), true
}

// enchantmentFullName is Enchantment.getFullname: the name with its level in
// Roman numerals, left out for the only level of an enchantment.
func enchantmentFullName(e item.EnchantmentType, level int) string {
	if level == 1 && e.MaxLevel() == 1 {
		return e.Name()
	}
	numerals := [...]string{"I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X"}
	if level >= 1 && level <= len(numerals) {
		return e.Name() + " " + numerals[level-1]
	}
	return fmt.Sprintf("%s enchantment.level.%d", e.Name(), level)
}

// commandTargetDisplayName is Entity.getDisplayName: the name of a player, the
// custom name of a mob, or the name of the type of any other entity.
func commandTargetDisplayName(target cmd.Target) string {
	if p, ok := target.(*player.Player); ok {
		return p.Name()
	}
	if named, ok := target.(interface{ NameTag() string }); ok && named.NameTag() != "" {
		return named.NameTag()
	}
	return wordsOf(commandTargetName(target))
}

// stackDisplayName is ItemStack.getHoverName: the custom name of a stack, or
// the name of its item.
func stackDisplayName(stack item.Stack) string {
	if name := stack.CustomName(); name != "" {
		return name
	}
	id, _ := stack.Item().EncodeItem()
	return wordsOf(id)
}

// wordsOf turns an identifier such as minecraft:iron_sword into Iron Sword.
func wordsOf(id string) string {
	words := strings.Split(strings.TrimPrefix(id, "minecraft:"), "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
