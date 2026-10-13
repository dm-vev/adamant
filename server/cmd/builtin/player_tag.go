package builtin

import (
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

func registerPlayerTagCommands(_ serverAdapter) {
	cmd.Register(cmd.New("tag", "Manages entity tags.", nil, tagAddCommand{}, tagRemoveCommand{}, tagListCommand{}))
	cmd.Register(cmd.New("attribute", "Queries and changes entity attributes.", nil, attributeGetCommand{}, attributeBaseGetCommand{}, attributeBaseSetCommand{}, attributeBaseResetCommand{}, attributeModifierAddCommand{}, attributeModifierRemoveCommand{}, attributeModifierValueCommand{}))
}

type tagTarget interface {
	CommandTags() []string
	SetCommandTags([]string)
}
type tagAddCommand struct {
	opGate
	Targets []cmd.Target   `cmd:"targets"`
	Add     cmd.SubCommand `cmd:"add"`
	Name    string         `cmd:"name"`
}
type tagRemoveCommand struct {
	opGate
	Targets []cmd.Target   `cmd:"targets"`
	Remove  cmd.SubCommand `cmd:"remove"`
	Name    string         `cmd:"name"`
}
type tagListCommand struct {
	opGate
	Targets []cmd.Target   `cmd:"targets"`
	List    cmd.SubCommand `cmd:"list"`
}

func (c tagAddCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	changeCommandTags(c.Targets, c.Name, true, o)
}
func (c tagRemoveCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	changeCommandTags(c.Targets, c.Name, false, o)
}
func (c tagListCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if len(c.Targets) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	for _, target := range c.Targets {
		if tags, ok := target.(tagTarget); ok {
			values := tags.CommandTags()
			o.Printf("%s has %d tag(s): %s", commandTargetName(target), len(values), strings.Join(values, ", "))
		}
	}
}
func changeCommandTags(targets []cmd.Target, name string, add bool, o *cmd.Output) {
	if len(targets) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	if name == "" {
		o.Errort(cmd.MessageParameterInvalid, name)
		return
	}
	changed := 0
	for _, target := range targets {
		tags, ok := target.(tagTarget)
		if !ok {
			continue
		}
		current := tags.CommandTags()
		index := slices.Index(current, name)
		if add && index < 0 {
			tags.SetCommandTags(append(current, name))
			changed++
		}
		if !add && index >= 0 {
			tags.SetCommandTags(slices.Delete(current, index, index+1))
			changed++
		}
	}
	if changed == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	o.Printf("Updated tags for %d target(s).", changed)
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
	opGate
	Target    []cmd.Target          `cmd:"target"`
	Attribute string                `cmd:"attribute"`
	Get       cmd.SubCommand        `cmd:"get"`
	Scale     cmd.Optional[float64] `cmd:"scale"`
}
type attributeBaseGetCommand struct {
	opGate
	Target    []cmd.Target   `cmd:"target"`
	Attribute string         `cmd:"attribute"`
	Base      cmd.SubCommand `cmd:"base"`
	Get       cmd.SubCommand `cmd:"get"`
}
type attributeBaseSetCommand struct {
	opGate
	Target    []cmd.Target   `cmd:"target"`
	Attribute string         `cmd:"attribute"`
	Base      cmd.SubCommand `cmd:"base"`
	Set       cmd.SubCommand `cmd:"set"`
	Value     float64        `cmd:"value"`
}
type attributeBaseResetCommand struct {
	opGate
	Target    []cmd.Target   `cmd:"target"`
	Attribute string         `cmd:"attribute"`
	Base      cmd.SubCommand `cmd:"base"`
	Reset     cmd.SubCommand `cmd:"reset"`
}
type attributeModifierAddCommand struct {
	opGate
	Target    []cmd.Target   `cmd:"target"`
	Attribute string         `cmd:"attribute"`
	Modifier  cmd.SubCommand `cmd:"modifier"`
	Add       cmd.SubCommand `cmd:"add"`
	ID        string         `cmd:"id"`
	Amount    float64        `cmd:"amount"`
	Operation string         `cmd:"operation"`
}
type attributeModifierRemoveCommand struct {
	opGate
	Target    []cmd.Target   `cmd:"target"`
	Attribute string         `cmd:"attribute"`
	Modifier  cmd.SubCommand `cmd:"modifier"`
	Remove    cmd.SubCommand `cmd:"remove"`
	ID        string         `cmd:"id"`
}
type attributeModifierValueCommand struct {
	opGate
	Target    []cmd.Target   `cmd:"target"`
	Attribute string         `cmd:"attribute"`
	Modifier  cmd.SubCommand `cmd:"modifier"`
	Value     cmd.SubCommand `cmd:"value"`
	Get       cmd.SubCommand `cmd:"get"`
	ID        string         `cmd:"id"`
}

func (c attributeGetCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	scale, loaded := c.Scale.Load()
	if !loaded {
		scale = 1
	}
	v, ok := attributeValue(target, c.Attribute)
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, c.Attribute)
		return
	}
	o.Printf("%s has %s: %g", commandTargetName(target), c.Attribute, v*scale)
}
func (c attributeBaseGetCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	v, ok := attributeBase(target, c.Attribute)
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, c.Attribute)
		return
	}
	o.Printf("%s base for %s is %g", c.Attribute, commandTargetName(target), v)
}
func (c attributeBaseSetCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	if !setAttributeBase(target, c.Attribute, c.Value) {
		o.Errort(cmd.MessageParameterInvalid, c.Attribute)
		return
	}
	o.Printf("Set %s base to %g.", c.Attribute, c.Value)
}
func (c attributeBaseResetCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	v, ok := defaultAttributeBase(target, c.Attribute)
	if !ok || !setAttributeBase(target, c.Attribute, v) {
		o.Errort(cmd.MessageParameterInvalid, c.Attribute)
		return
	}
	o.Printf("Reset %s base.", c.Attribute)
}
func (c attributeModifierAddCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	if c.ID == "" || !slices.Contains([]string{"add_value", "add_multiplied_base", "add_multiplied_total"}, c.Operation) || !setModifier(target, c.Attribute, c.ID, c.Amount, c.Operation, false) {
		o.Errort(cmd.MessageParameterInvalid, c.Attribute)
		return
	}
	o.Printf("Added modifier %s.", c.ID)
}
func (c attributeModifierRemoveCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	if !setModifier(target, c.Attribute, c.ID, 0, "", true) {
		o.Errort(cmd.MessageParameterInvalid, c.ID)
		return
	}
	o.Printf("Removed modifier %s.", c.ID)
}
func (c attributeModifierValueCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	v, ok := modifierValue(target, c.Attribute, c.ID)
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, c.ID)
		return
	}
	o.Printf("Modifier %s value is %g.", c.ID, v)
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
