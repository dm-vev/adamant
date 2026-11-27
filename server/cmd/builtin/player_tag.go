package builtin

import (
	"slices"
	"strings"

	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
)

func registerPlayerTagCommands(_ serverAdapter) {
	cmd.Register(newCommand("tag", nil, tagAddCommand{}, tagRemoveCommand{}, tagListCommand{}))
	cmd.Register(newCommand("attribute", nil, attributeGetCommand{}, attributeBaseGetCommand{}, attributeBaseSetCommand{}, attributeBaseResetCommand{}, attributeModifierAddCommand{}, attributeModifierRemoveCommand{}, attributeModifierValueCommand{}))
}

type tagTarget interface {
	CommandTags() []string
	SetCommandTags([]string)
}
type tagAddCommand struct {
	gameMaster
	Targets cmd.Entities   `cmd:"targets"`
	Add     cmd.SubCommand `cmd:"add"`
	Name    string         `cmd:"name"`
}
type tagRemoveCommand struct {
	gameMaster
	Targets cmd.Entities   `cmd:"targets"`
	Remove  cmd.SubCommand `cmd:"remove"`
	Name    string         `cmd:"name"`
}
type tagListCommand struct {
	gameMaster
	Targets cmd.Entities   `cmd:"targets"`
	List    cmd.SubCommand `cmd:"list"`
}

type tagListEntry struct {
	ok         bool
	targetName string
	tags       []string
}

func (c tagAddCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	changeCommandTags(src, o, tx, c.Targets, c.Name, true)
}
func (c tagRemoveCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	changeCommandTags(src, o, tx, c.Targets, c.Name, false)
}
func (c tagListCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	cmd.ForEach(src, o, tx, c.Targets,
		func(_ *world.Tx, target cmd.Target) tagListEntry {
			tags, ok := target.(tagTarget)
			if !ok {
				return tagListEntry{}
			}
			return tagListEntry{ok: true, targetName: commandTargetName(target), tags: slices.Clone(tags.CommandTags())}
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[tagListEntry]) {
			if len(rs) == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			total := 0
			for _, r := range rs {
				if r.Value.ok {
					total += len(r.Value.tags)
					o.Printf("%s has %d tag(s): %s", r.Value.targetName, len(r.Value.tags), strings.Join(r.Value.tags, ", "))
				}
			}
			o.SetResult(total)
		},
	)
}
func changeCommandTags(src cmd.Source, o *cmd.Output, tx *world.Tx, targets cmd.Entities, name string, add bool) {
	if name == "" {
		o.Errort(cmd.MessageParameterInvalid, name)
		return
	}
	cmd.ForEach(src, o, tx, targets,
		func(_ *world.Tx, target cmd.Target) int {
			tags, ok := target.(tagTarget)
			if !ok {
				return 0
			}
			current := tags.CommandTags()
			index := slices.Index(current, name)
			if add && index < 0 {
				tags.SetCommandTags(append(current, name))
				return 1
			}
			if !add && index >= 0 {
				tags.SetCommandTags(slices.Delete(current, index, index+1))
				return 1
			}
			return 0
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Updated tags for %d target(s).", tally.NonZero)
		},
	)
}
func commandTargetName(target cmd.Target) string {
	if p, ok := target.(*player.Player); ok {
		return p.Name()
	}
	if e, ok := target.(world.Entity); ok {
		return e.H().Type().EncodeEntity()
	}
	return "entity"
}

type attributeGetCommand struct {
	gameMaster
	Target    cmd.OneEntity         `cmd:"target"`
	Attribute string                `cmd:"attribute"`
	Get       cmd.SubCommand        `cmd:"get"`
	Scale     cmd.Optional[float64] `cmd:"scale"`
}
type attributeBaseGetCommand struct {
	gameMaster
	Target    cmd.OneEntity  `cmd:"target"`
	Attribute string         `cmd:"attribute"`
	Base      cmd.SubCommand `cmd:"base"`
	Get       cmd.SubCommand `cmd:"get"`
}
type attributeBaseSetCommand struct {
	gameMaster
	Target    cmd.OneEntity  `cmd:"target"`
	Attribute string         `cmd:"attribute"`
	Base      cmd.SubCommand `cmd:"base"`
	Set       cmd.SubCommand `cmd:"set"`
	Value     float64        `cmd:"value"`
}
type attributeBaseResetCommand struct {
	gameMaster
	Target    cmd.OneEntity  `cmd:"target"`
	Attribute string         `cmd:"attribute"`
	Base      cmd.SubCommand `cmd:"base"`
	Reset     cmd.SubCommand `cmd:"reset"`
}
type attributeModifierAddCommand struct {
	gameMaster
	Target    cmd.OneEntity  `cmd:"target"`
	Attribute string         `cmd:"attribute"`
	Modifier  cmd.SubCommand `cmd:"modifier"`
	Add       cmd.SubCommand `cmd:"add"`
	ID        string         `cmd:"id"`
	Amount    float64        `cmd:"amount"`
	Operation string         `cmd:"operation"`
}
type attributeModifierRemoveCommand struct {
	gameMaster
	Target    cmd.OneEntity  `cmd:"target"`
	Attribute string         `cmd:"attribute"`
	Modifier  cmd.SubCommand `cmd:"modifier"`
	Remove    cmd.SubCommand `cmd:"remove"`
	ID        string         `cmd:"id"`
}
type attributeModifierValueCommand struct {
	gameMaster
	Target    cmd.OneEntity  `cmd:"target"`
	Attribute string         `cmd:"attribute"`
	Modifier  cmd.SubCommand `cmd:"modifier"`
	Value     cmd.SubCommand `cmd:"value"`
	Get       cmd.SubCommand `cmd:"get"`
	ID        string         `cmd:"id"`
}

type attrValueRes struct {
	ok         bool
	val        float64
	targetName string
}

func (c attributeGetCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	scale, loaded := c.Scale.Load()
	if !loaded {
		scale = 1
	}
	attr := c.Attribute
	cmd.ForEach(src, o, tx, c.Target,
		func(_ *world.Tx, target cmd.Target) attrValueRes {
			v, ok := attributeValue(target, attr)
			return attrValueRes{ok: ok, val: v, targetName: commandTargetName(target)}
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[attrValueRes]) {
			if len(rs) == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			if !rs[0].Value.ok {
				o.Errort(cmd.MessageParameterInvalid, attr)
				return
			}
			scaled := rs[0].Value.val * scale
			o.SetResult(int(scaled))
			o.Printf("%s has %s: %g", rs[0].Value.targetName, attr, scaled)
		},
	)
}
func (c attributeBaseGetCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	attr := c.Attribute
	cmd.ForEach(src, o, tx, c.Target,
		func(_ *world.Tx, target cmd.Target) attrValueRes {
			v, ok := attributeBase(target, attr)
			return attrValueRes{ok: ok, val: v, targetName: commandTargetName(target)}
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[attrValueRes]) {
			if len(rs) == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			if !rs[0].Value.ok {
				o.Errort(cmd.MessageParameterInvalid, attr)
				return
			}
			o.SetResult(int(rs[0].Value.val))
			o.Printf("%s base for %s is %g", attr, rs[0].Value.targetName, rs[0].Value.val)
		},
	)
}
func (c attributeBaseSetCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	attr, val := c.Attribute, c.Value
	cmd.ForEach(src, o, tx, c.Target,
		func(_ *world.Tx, target cmd.Target) bool {
			return setAttributeBase(target, attr, val)
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[bool]) {
			if len(rs) == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			if !rs[0].Value {
				o.Errort(cmd.MessageParameterInvalid, attr)
				return
			}
			o.SetResult(1)
			o.Printf("Set %s base to %g.", attr, val)
		},
	)
}
func (c attributeBaseResetCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	attr := c.Attribute
	cmd.ForEach(src, o, tx, c.Target,
		func(_ *world.Tx, target cmd.Target) bool {
			v, ok := defaultAttributeBase(target, attr)
			return ok && setAttributeBase(target, attr, v)
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[bool]) {
			if len(rs) == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			if !rs[0].Value {
				o.Errort(cmd.MessageParameterInvalid, attr)
				return
			}
			o.SetResult(1)
			o.Printf("Reset %s base.", attr)
		},
	)
}
func (c attributeModifierAddCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	attr, id, amount, op := c.Attribute, c.ID, c.Amount, c.Operation
	if id == "" || !slices.Contains([]string{"add_value", "add_multiplied_base", "add_multiplied_total"}, op) {
		o.Errort(cmd.MessageParameterInvalid, attr)
		return
	}
	cmd.ForEach(src, o, tx, c.Target,
		func(_ *world.Tx, target cmd.Target) bool {
			return setModifier(target, attr, id, amount, op, false)
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[bool]) {
			if len(rs) == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			if !rs[0].Value {
				o.Errort(cmd.MessageParameterInvalid, attr)
				return
			}
			o.SetResult(1)
			o.Printf("Added modifier %s.", id)
		},
	)
}
func (c attributeModifierRemoveCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	attr, id := c.Attribute, c.ID
	cmd.ForEach(src, o, tx, c.Target,
		func(_ *world.Tx, target cmd.Target) bool {
			return setModifier(target, attr, id, 0, "", true)
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[bool]) {
			if len(rs) == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			if !rs[0].Value {
				o.Errort(cmd.MessageParameterInvalid, id)
				return
			}
			o.SetResult(1)
			o.Printf("Removed modifier %s.", id)
		},
	)
}
func (c attributeModifierValueCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	attr, id := c.Attribute, c.ID
	cmd.ForEach(src, o, tx, c.Target,
		func(_ *world.Tx, target cmd.Target) attrValueRes {
			v, ok := modifierValue(target, attr, id)
			return attrValueRes{ok: ok, val: v}
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[attrValueRes]) {
			if len(rs) == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			if !rs[0].Value.ok {
				o.Errort(cmd.MessageParameterInvalid, id)
				return
			}
			o.SetResult(int(rs[0].Value.val))
			o.Printf("Modifier %s value is %g.", id, rs[0].Value.val)
		},
	)
}

func attributeID(name string) string {
	if strings.Contains(name, ":") {
		return name
	}
	return "minecraft:" + name
}
func attributeValue(target cmd.Target, name string) (float64, bool) {
	if v, ok := target.(interface{ MaxHealth() float64 }); ok && attributeID(name) == "minecraft:max_health" {
		return v.MaxHealth(), true
	}
	if v, ok := target.(interface{ Speed() float64 }); ok && attributeID(name) == "minecraft:movement_speed" {
		return v.Speed(), true
	}
	if v, ok := target.(interface{ NativeAttributeValue(string) (float64, bool) }); ok {
		return v.NativeAttributeValue(attributeID(name))
	}
	return attributeBase(target, name)
}
func attributeBase(target cmd.Target, name string) (float64, bool) {
	id := attributeID(name)
	switch id {
	case "minecraft:max_health":
		if v, ok := target.(interface{ MaxHealth() float64 }); ok {
			return v.MaxHealth(), true
		}
	case "minecraft:movement_speed":
		if v, ok := target.(interface{ Speed() float64 }); ok {
			return v.Speed(), true
		}
	case "minecraft:scale":
		if p, ok := target.(*player.Player); ok {
			return p.Scale(), true
		}
	}
	if e, ok := target.(interface{ NativeAttribute(string) (float64, bool) }); ok {
		return e.NativeAttribute(id)
	}
	return 0, false
}
func setAttributeBase(target cmd.Target, name string, v float64) bool {
	id := attributeID(name)
	if id == "minecraft:max_health" && v <= 0 {
		return false
	}
	switch id {
	case "minecraft:max_health":
		if l, ok := target.(interface{ SetMaxHealth(float64) }); ok {
			l.SetMaxHealth(v)
			return true
		}
	case "minecraft:movement_speed":
		if s, ok := target.(interface{ SetSpeed(float64) }); ok {
			s.SetSpeed(v)
			return true
		}
	case "minecraft:scale":
		if p, ok := target.(*player.Player); ok {
			p.SetScale(v)
			return true
		}
	default:
		if e, ok := target.(interface{ SetNativeAttribute(string, float64) bool }); ok {
			return e.SetNativeAttribute(id, v)
		}
	}
	return false
}
func defaultAttributeBase(target cmd.Target, name string) (float64, bool) {
	switch attributeID(name) {
	case "minecraft:max_health":
		return 20, true
	case "minecraft:movement_speed":
		return .1, true
	case "minecraft:scale":
		return 1, true
	case "minecraft:attack_damage":
		return 1, true
	case "minecraft:armor", "minecraft:knockback_resistance":
		return 0, true
	}
	return 0, false
}
func setModifier(target cmd.Target, attr, id string, amount float64, operation string, remove bool) bool {
	e, ok := target.(interface {
		SetCommandAttributeModifier(string, string, float64, string, bool) bool
	})
	return ok && e.SetCommandAttributeModifier(attributeID(attr), id, amount, operation, remove)
}
func modifierValue(target cmd.Target, attr, id string) (float64, bool) {
	e, ok := target.(interface {
		CommandAttributeModifier(string, string) (float64, bool)
	})
	if !ok {
		return 0, false
	}
	return e.CommandAttributeModifier(attributeID(attr), id)
}
