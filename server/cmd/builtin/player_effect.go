package builtin

import (
	"fmt"
	"strings"
	"time"

	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/entity/effect"
	"github.com/dm-vev/adamant/server/item"
	_ "github.com/dm-vev/adamant/server/item/enchantment"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
)

type effectCommand struct {
	gameMaster
	Give      cmd.SubCommand       `cmd:"give"`
	Targets   cmd.Entities         `cmd:"targets"`
	Effect    string               `cmd:"effect"`
	Duration  cmd.Optional[string] `cmd:"seconds"`
	Amplifier cmd.Optional[int]    `cmd:"amplifier"`
	Hide      cmd.Optional[bool]   `cmd:"hideParticles"`
}
type clearEffectCommand struct {
	gameMaster
	Clear   cmd.SubCommand             `cmd:"clear"`
	Targets cmd.Optional[cmd.Entities] `cmd:"targets"`
	Effect  cmd.Optional[string]       `cmd:"effect"`
}
type enchantCommand struct {
	worldOpCommand
	Targets     cmd.Entities      `cmd:"targets"`
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

func (c effectCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	t := effectType(c.Effect)
	lt, ok := t.(effect.LastingType)
	if !ok || t == nil {
		o.Errort(cmd.MessageParameterInvalid, c.Effect)
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
	eff := effect.New(lt, level, dur)
	if infinite {
		eff = effect.NewInfinite(lt, level)
	}
	if hide {
		eff = eff.WithoutParticles()
	}
	effectName := c.Effect
	cmd.ForEach(src, o, tx, c.Targets,
		func(_ *world.Tx, target cmd.Target) int {
			l, ok := target.(entity.Living)
			if !ok {
				return 0
			}
			l.AddEffect(eff)
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Applied effect %s to %d targets.", effectName, tally.NonZero)
		},
	)
}
func (c clearEffectCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	var t effect.Type
	if name, ok := c.Effect.Load(); ok {
		t = effectType(name)
		if t == nil {
			o.Errort(cmd.MessageParameterInvalid, name)
			return
		}
	}
	cmd.ForEach(src, o, tx, optionalEntities(c.Targets, src),
		func(_ *world.Tx, target cmd.Target) int {
			l, ok := target.(entity.Living)
			if !ok {
				return 0
			}
			if t != nil {
				l.RemoveEffect(t)
			} else {
				for _, e := range l.Effects() {
					l.RemoveEffect(e.Type())
				}
			}
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Cleared effects from %d targets.", tally.NonZero)
		},
	)
}

func enchantmentType(name string) item.EnchantmentType {
	name = "minecraft:" + strings.TrimPrefix(strings.ToLower(name), "minecraft:")
	for _, e := range item.Enchantments() {
		if item.CanonicalEnchantmentID(e) == name {
			return e
		}
	}
	return nil
}

const (
	enchantApplied uint8 = iota
	enchantNotLiving
	enchantNoItem
	enchantIncompatible
)

type enchantOutcome struct {
	status      uint8
	displayName string
	itemName    string
}

func applyEnchantTarget(target cmd.Target, e item.EnchantmentType, level int) enchantOutcome {
	display := commandTargetDisplayName(target)
	if _, living := target.(entity.Living); !living {
		return enchantOutcome{status: enchantNotLiving, displayName: display}
	}
	holder, ok := target.(interface {
		HeldItems() (main, off item.Stack)
		SetHeldItems(main, off item.Stack)
	})
	var main, off item.Stack
	if ok {
		main, off = holder.HeldItems()
	}
	if main.Empty() {
		return enchantOutcome{status: enchantNoItem, displayName: display}
	}
	next, ok := enchantedMainHand(main, e, level)
	if !ok {
		return enchantOutcome{status: enchantIncompatible, displayName: display, itemName: stackDisplayName(main)}
	}
	holder.SetHeldItems(next, off)
	return enchantOutcome{status: enchantApplied, displayName: display}
}

// Run ports EnchantCommand.enchant: the enchantment goes on the main-hand item
// of each living target. A target that cannot take it fails the command with
// the reason only if it is the sole target, and the command fails with Java's
// commands.enchant.failed ("Nothing changed. Targets either have no item in
// their hands or the enchantment could not be applied") if no target took it.
func (c enchantCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
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
	name := enchantmentFullName(e, level)
	cmd.ForEach(src, o, tx, c.Targets,
		func(_ *world.Tx, target cmd.Target) enchantOutcome {
			return applyEnchantTarget(target, e, level)
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[enchantOutcome]) {
			if len(rs) == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			if len(rs) == 1 && rs[0].Value.status != enchantApplied {
				switch rs[0].Value.status {
				case enchantNotLiving:
					o.Errorf("%s is not a valid entity for this command", rs[0].Value.displayName)
				case enchantNoItem:
					o.Errorf("%s is not holding any item", rs[0].Value.displayName)
				case enchantIncompatible:
					o.Errorf("%s cannot support that enchantment", rs[0].Value.itemName)
				}
				return
			}
			var applied []string
			for _, r := range rs {
				if r.Value.status == enchantApplied {
					applied = append(applied, r.Value.displayName)
				}
			}
			switch len(applied) {
			case 0:
				o.Errorf("Nothing changed. Targets either have no item in their hands or the enchantment could not be applied")
			case 1:
				o.SetResult(1)
				o.Printf("Applied enchantment %s to %s's item", name, applied[0])
			default:
				o.SetResult(len(applied))
				o.Printf("Applied enchantment %s to %d entities", name, len(applied))
			}
		},
	)
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
// custom name of a mob, or the name of the type of any other entity, by its
// Java identifier.
func commandTargetDisplayName(target cmd.Target) string {
	if p, ok := target.(*player.Player); ok {
		return p.Name()
	}
	if named, ok := target.(interface{ NameTag() string }); ok && named.NameTag() != "" {
		return named.NameTag()
	}
	return wordsOf(world.CanonicalEntityTypeID(commandTargetName(target)))
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
