package session

import (
	"maps"
	"math"
	"slices"
	"strings"

	bedrocktext "github.com/df-mc/dragonfly/protocol/bedrock/text"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	nettext "github.com/dm-vev/adamant/net/bedrock/text"
	"github.com/go-gl/mathgl/mgl64"
	"golang.org/x/text/language"
)

// SendCommandOutput sends the output of a command to the player. It will be shown to the caller of the
// command, which might be the player or a websocket server. Each message is written with
// bedrocktext.Message: a translation as its key and parameters, which the client translates.
func (s *Session) SendCommandOutput(o client.CommandOutput) {
	if s == Nop {
		return
	}
	messages := make([]mc.CommandOutputMessage, 0, len(o.Messages))
	successes := uint32(0)
	for _, m := range o.Messages {
		om := mc.CommandOutputMessage{Success: m.Success}
		om.Message, om.Parameters, _ = bedrocktext.Message(m.Text)
		if m.Success {
			successes++
		}
		messages = append(messages, om)
	}

	s.writePacket(&mc.CommandOutputPacket{
		CommandOrigin:  s.commandOriginSnapshot(),
		OutputType:     mc.CommandOutputTypeAllOutput,
		SuccessCount:   successes,
		OutputMessages: messages,
	})
}

// setCommandOrigin records the latest command origin so command output can be correlated with the request.
func (s *Session) setCommandOrigin(origin mc.CommandOrigin) {
	copied := origin
	s.commandOrigin.Store(&copied)
}

// commandOriginSnapshot returns the most recently stored command origin.
func (s *Session) commandOriginSnapshot() mc.CommandOrigin {
	if origin := s.commandOrigin.Load(); origin != nil {
		return *origin
	}
	return mc.CommandOrigin{}
}

// BuildAvailableCommands builds an AvailableCommands packet and the runnable command map for the Source
// passed. The input map may contain aliases. It returns the AvailableCommands packet and the runnable command map
// for the commands that the Source can execute.
func BuildAvailableCommands(
	commands map[string]cmd.Command,
	src cmd.Source,
	softEnums map[string]struct{},
) (*mc.AvailableCommandsPacket, map[string]map[int]cmd.Runnable) {
	m := make(map[string]map[int]cmd.Runnable, len(commands))

	pk := &mc.AvailableCommandsPacket{}

	var enums []commandEnum
	enumIndices := map[string]uint32{}

	var dynamicEnums []commandEnum
	dynamicEnumIndices := map[string]uint32{}

	suffixIndices := map[string]uint32{}

	// Keep command and enum indices stable between logins and full updates.
	for _, alias := range slices.Sorted(maps.Keys(commands)) {
		c := commands[alias]
		if c.Name() != alias || clientCommand(c.Name()) {
			// Aliases are encoded with their canonical command. Client-local
			// commands already have a grammar installed by the Bedrock client.
			continue
		}

		if run := c.Runnables(src); len(run) > 0 {
			m[alias] = run
		} else {
			continue
		}

		params := c.Params(src)
		overloads := make([]mc.CommandOverload, len(params))

		aliasesIndex := uint32(math.MaxUint32)
		if len(c.Aliases()) > 0 {
			aliasesIndex = uint32(len(enums))
			enumIndices[c.Name()+"Aliases"] = aliasesIndex
			enums = append(enums, commandEnum{Type: c.Name() + "Aliases", Options: c.Aliases()})
		}

		for i, params := range params {
			for _, paramInfo := range params {
				t, enum := valueToParamType(paramInfo, src)
				suffix := paramInfo.Suffix

				opt := byte(0)
				if options, ok := paramInfo.Value.(cmd.ParamOptioner); ok {
					opt = paramOptions(options.ParamOptions())
				}
				if suffix != "" {
					index, ok := suffixIndices[suffix]
					if !ok {
						index = uint32(len(pk.Suffixes))
						suffixIndices[suffix] = index
						pk.Suffixes = append(pk.Suffixes, suffix)
					}
					t = mc.CommandArgSuffixed | index
				} else {
					t |= mc.CommandArgValid
					if len(enum.Options) > 0 || enum.Type != "" {
						dynamic := enum.Dynamic
						if !dynamic {
							_, dynamic = softEnums[enum.Type]
						} else {
							softEnums[enum.Type] = struct{}{}
						}
						if !dynamic {
							index, ok := enumIndices[enum.Type]
							if !ok {
								index = uint32(len(enums))
								enumIndices[enum.Type] = index
								enums = append(enums, enum)
							}
							t |= mc.CommandArgEnum | index
						} else {
							index, ok := dynamicEnumIndices[enum.Type]
							if !ok {
								index = uint32(len(dynamicEnums))
								dynamicEnumIndices[enum.Type] = index
								dynamicEnums = append(dynamicEnums, enum)
							}
							t |= mc.CommandArgSoftEnum | index
						}
					}
				}
				overloads[i].Parameters = append(overloads[i].Parameters, mc.CommandParameter{
					Name:     paramInfo.Name,
					Type:     t,
					Optional: paramInfo.Optional,
					Options:  opt,
				})
			}
		}
		pk.Commands = append(pk.Commands, mc.Command{
			Name:            c.Name(),
			Description:     commandDescription(c, src),
			AliasesOffset:   aliasesIndex,
			PermissionLevel: mc.CommandPermissionLevelAny,
			Overloads:       overloads,
		})
	}

	pk.DynamicEnums = make([]mc.DynamicEnum, 0, len(dynamicEnums))
	for _, e := range dynamicEnums {
		pk.DynamicEnums = append(pk.DynamicEnums, mc.DynamicEnum{Type: e.Type, Values: e.Options})
	}

	enumValueIndices := make(map[string]uint32, len(enums)*3)
	pk.EnumValues = make([]string, 0, len(enumValueIndices))

	pk.Enums = make([]mc.CommandEnum, 0, len(enums))
	for enumIndex, enum := range enums {
		protoEnum := mc.CommandEnum{Type: enum.Type}
		for _, opt := range enum.Options {
			index, ok := enumValueIndices[opt]
			if !ok {
				index = uint32(len(pk.EnumValues))
				enumValueIndices[opt] = index
				pk.EnumValues = append(pk.EnumValues, opt)
			}
			protoEnum.ValueIndices = append(protoEnum.ValueIndices, index)
			if enum.Constraints != nil {
				if constraints := enumConstraints(enum.Constraints(opt)); len(constraints) > 0 {
					pk.Constraints = append(pk.Constraints, mc.CommandEnumConstraint{
						EnumValueIndex: index,
						EnumIndex:      uint32(enumIndex),
						Constraints:    constraints,
					})
				}
			}
		}
		pk.Enums = append(pk.Enums, protoEnum)
	}
	return pk, m
}

// commandDescription resolves server translations for the source's locale.
// Native translation keys are sent without the message translation marker,
// matching BDS; the client translates these using its own language resources.
func commandDescription(c cmd.Command, src cmd.Source) string {
	description := c.DescriptionTranslation()
	if description.Zero() {
		return c.Description()
	}
	locale := language.English
	if localized, ok := src.(interface{ Locale() language.Tag }); ok {
		locale = localized.Locale()
	}
	message, _, _ := bedrocktext.Message(description.F().Text(locale))
	resolved := strings.TrimPrefix(message, nettext.Reset)
	return strings.TrimPrefix(resolved, "%")
}

// clientCommand identifies commands registered by ClientCommands::setupStandard
// with SyncLocal. BDS omits help from AvailableCommands; replacing the client's
// help signature conflicts with its existing command-name grammar.
func clientCommand(name string) bool { return name == "help" }

// sendAvailableCommands sends all available commands of the server. Once sent, they will be visible in the
// /help list and will be auto-completed.
func (s *Session) sendAvailableCommands(co Controllable, softEnums map[string]struct{}) map[string]map[int]cmd.Runnable {
	commands := cmd.Commands()
	pk, m := BuildAvailableCommands(commands, co, softEnums)
	s.writePacket(pk)
	return m
}

type commandEnum struct {
	Type        string
	Options     []string
	Dynamic     bool
	Constraints func(string) []cmd.EnumConstraint
}

// valueToParamType finds the command argument type of the value passed and returns it, in addition to creating
// an enum if applicable.
func valueToParamType(i cmd.ParamInfo, source cmd.Source) (t uint32, enum commandEnum) {
	switch i.Value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return mc.CommandArgTypeInt, enum
	case float32, float64:
		return mc.CommandArgTypeFloat, enum
	case string:
		return mc.CommandArgTypeString, enum
	case cmd.Varargs:
		return mc.CommandArgTypeRawText, enum
	case cmd.Target, []cmd.Target:
		return mc.CommandArgTypeTarget, enum
	case bool:
		return 0, commandEnum{
			Type:    "Boolean",
			Options: []string{"true", "false"},
		}
	case mgl64.Vec3:
		return mc.CommandArgTypePosition, enum
	case cmd.SubCommand:
		return 0, commandEnum{
			Type:    "SubCommand" + i.Name,
			Options: []string{i.Name},
		}
	}
	if enum, ok := i.Value.(cmd.Enum); ok {
		ce := commandEnum{
			Type:    enum.Type(),
			Options: enum.Options(source),
		}
		if dyn, ok := enum.(cmd.DynamicEnum); ok && dyn.Dynamic() {
			ce.Dynamic = true
		}
		if constrained, ok := enum.(cmd.ConstrainedEnum); ok {
			ce.Constraints = constrained.Constraints
		}
		return 0, ce
	}
	return mc.CommandArgTypeRValue, enum
}

// resendCommands resends all commands that a Session has access to if the map of runnable commands passed does not
// match with the commands that the Session is currently allowed to execute.
// True is returned if the commands were resent.
func (s *Session) resendCommands(
	before map[string]map[int]cmd.Runnable,
	co Controllable,
	softEnums map[string]struct{},
) (map[string]map[int]cmd.Runnable, bool) {
	commands := cmd.Commands()
	m := make(map[string]map[int]cmd.Runnable, len(commands))

	for alias, c := range commands {
		if c.Name() == alias && !clientCommand(c.Name()) {
			if run := c.Runnables(co); len(run) > 0 {
				m[alias] = run
			}
		}
	}
	if len(before) != len(m) {
		return s.sendAvailableCommands(co, softEnums), true
	}
	// First check for commands that were newly added.
	for name, r := range m {
		for k := range r {
			if _, ok := before[name][k]; !ok {
				return s.sendAvailableCommands(co, softEnums), true
			}
		}
	}
	// Then check for commands that a player could execute before, but no longer can.
	for name, r := range before {
		for k := range r {
			if _, ok := m[name][k]; !ok {
				return s.sendAvailableCommands(co, softEnums), true
			}
		}
	}
	return m, false
}

// enums returns a map of all enums exposed to the Session and records the values those enums currently hold.
func (s *Session) enums(co Controllable) (map[string]cmd.Enum, map[string][]string) {
	enums, enumValues := make(map[string]cmd.Enum), make(map[string][]string)
	for alias, c := range cmd.Commands() {
		if c.Name() == alias && !clientCommand(c.Name()) {
			for _, params := range c.Params(co) {
				for _, paramInfo := range params {
					if enum, ok := paramInfo.Value.(cmd.Enum); ok {
						enums[enum.Type()] = enum
						enumValues[enum.Type()] = enum.Options(co)
					}
				}
			}
		}
	}
	return enums, enumValues
}

// resendEnums checks the options of the enums passed against the values that were previously recorded. If they do not
// match, and the enum is in softEnums, the enum is resent via UpdateSoftEnum. If the enum is not yet in softEnums,
// it is added and the full AvailableCommands packet is resent.
func (s *Session) resendEnums(
	enums map[string]cmd.Enum,
	before map[string][]string,
	softEnums map[string]struct{},
	r map[string]map[int]cmd.Runnable,
	c Controllable,
) map[string]map[int]cmd.Runnable {
	for name, enum := range enums {
		valuesBefore := before[name]
		values := enum.Options(c)
		before[name] = values

		changed := false
		if len(valuesBefore) != len(values) {
			changed = true
		} else {
			for k, v := range values {
				if valuesBefore[k] != v {
					changed = true
					break
				}
			}
		}
		if !changed {
			continue
		}

		if _, dynamic := softEnums[name]; dynamic {
			s.writePacket(&mc.UpdateSoftEnumPacket{EnumType: name, Options: values, ActionType: mc.SoftEnumActionSet})
		} else {
			softEnums[name] = struct{}{}
			r = s.sendAvailableCommands(c, softEnums)
		}
	}
	return r
}
