package session

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/text"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"golang.org/x/text/language"
)

func TestAvailableCommandsTranslatedDescriptions(t *testing.T) {
	native := cmd.New("native", "Native fallback", nil, commandSoftEnumFixture{}).
		WithDescriptionTranslation(chat.Translate(commandTestDescription{fallback: "%commands.gamemode.description"}, 0, "Native fallback"))
	localized := cmd.New("localized", "English fallback", nil, commandSoftEnumFixture{}).
		WithDescriptionTranslation(chat.Translate(commandTestDescription{russian: "Русское описание", fallback: "English fallback"}, 0, "English fallback"))
	plain := cmd.New("plain", "%Plain description", nil, commandSoftEnumFixture{})
	commands := map[string]cmd.Command{"native": native, "localized": localized, "plain": plain}
	for _, tt := range []struct {
		name string
		src  cmd.Source
		want string
	}{
		{"russian", commandLocaleSource{locale: language.Russian}, "Русское описание"},
		{"english", commandLocaleSource{locale: language.AmericanEnglish}, "English fallback"},
		{"unsupported", commandLocaleSource{locale: language.German}, "English fallback"},
		{"console", nil, "English fallback"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pk, _ := BuildAvailableCommands(commands, tt.src, make(map[string]struct{}))
			var wire bytes.Buffer
			wire.Write(encodePacket(pk))
			var decoded packet.AvailableCommands
			decoded.Marshal(protocol.NewReader(&wire, 0, true))
			got := make(map[string]string)
			for _, command := range decoded.Commands {
				got[command.Name] = command.Description
			}
			if got["localized"] != tt.want || got["native"] != "commands.gamemode.description" || got["plain"] != "%Plain description" {
				t.Fatalf("descriptions = %v", got)
			}
		})
	}
	// A translated copy must not change the original command's plain metadata.
	if !plain.DescriptionTranslation().Zero() || localized.Description() != "English fallback" {
		t.Fatal("translation changed plain command descriptions")
	}
	// CommandOutput uses the same translation, retaining the '%' marker for
	// native message substitution and the English fallback for console logs.
	s := testSession("command-descriptions")
	output := &cmd.Output{}
	output.Printt(native.DescriptionTranslation())
	output.Printt(localized.DescriptionTranslation())
	s.SendCommandOutput(output, language.Russian)
	pk := (s.testPacket()).(*v2193.CommandOutputPacket)
	if text.Clean(pk.OutputMessages[0].Message) != "%commands.gamemode.description" || text.Clean(pk.OutputMessages[1].Message) != "Русское описание" {
		t.Fatalf("translated output = %#v", pk.OutputMessages)
	}
	if text.Clean(output.Messages()[1].String()) != "English fallback" {
		t.Fatal("console output lost its fallback")
	}
}

type commandLocaleSource struct {
	cmd.Source
	locale language.Tag
}

func (s commandLocaleSource) Locale() language.Tag { return s.locale }

type commandTestDescription struct{ russian, fallback string }

func (d commandTestDescription) Resolve(locale language.Tag) string {
	if locale == language.Russian && d.russian != "" {
		return d.russian
	}
	return d.fallback
}

func TestAvailableCommandsStableWireRegistry(t *testing.T) {
	alpha := cmd.New("alpha", "Enum and suffix arguments", []string{"a"}, commandRegistryFixture{})
	zulu := cmd.New("zulu", "Soft enum arguments", nil, commandSoftEnumFixture{})
	commands := map[string]cmd.Command{"zulu": zulu, "a": alpha, "alpha": alpha}
	var first []byte
	for range 100 {
		pk, _ := BuildAvailableCommands(commands, nil, make(map[string]struct{}))
		var encoded bytes.Buffer
		encoded.Write(encodePacket(pk))
		wire := append([]byte(nil), encoded.Bytes()...)
		if first == nil {
			first = wire
		} else if !bytes.Equal(first, wire) {
			t.Fatal("unchanged commands produced a different command/enum registry")
		}
		var decoded packet.AvailableCommands
		decoded.Marshal(protocol.NewReader(&encoded, 0, true))
		if encoded.Len() != 0 {
			t.Fatalf("command packet has %d unread bytes", encoded.Len())
		}
		if len(decoded.Commands) != 2 || decoded.Commands[0].Name != "alpha" || decoded.Commands[1].Name != "zulu" {
			t.Fatalf("commands = %#v, want sorted canonical names without alias duplicates", decoded.Commands)
		}
		if len(decoded.EnumValues) <= 256 {
			t.Fatal("fixture did not exercise a large enum table")
		}
		for _, enum := range decoded.Enums {
			for _, index := range enum.ValueIndices {
				if int(index) >= len(decoded.EnumValues) {
					t.Fatalf("enum %s references missing value %d", enum.Type, index)
				}
			}
		}
		alpha := decoded.Commands[0]
		if alpha.AliasesOffset == math.MaxUint32 || int(alpha.AliasesOffset) >= len(decoded.Enums) {
			t.Fatalf("invalid alias enum index: %d", alpha.AliasesOffset)
		}
		var aliases []string
		for _, index := range decoded.Enums[alpha.AliasesOffset].ValueIndices {
			aliases = append(aliases, decoded.EnumValues[index])
		}
		if !slices.Equal(aliases, []string{"a", "alpha"}) {
			t.Fatalf("alias values = %v", aliases)
		}
		params := alpha.Overloads[0].Parameters
		for _, param := range params[:3] {
			index := param.Type & 0xfffff
			if param.Type&(protocol.CommandArgValid|protocol.CommandArgEnum) != protocol.CommandArgValid|protocol.CommandArgEnum || int(index) >= len(decoded.Enums) {
				t.Fatalf("invalid enum reference for %s: %#x", param.Name, param.Type)
			}
		}
		choices := decoded.Enums[params[1].Type&0xfffff]
		if len(choices.ValueIndices) != 260 || decoded.EnumValues[choices.ValueIndices[259]] != "choice259" {
			t.Fatal("large enum lost completion values")
		}
		if params[3].Type&protocol.CommandArgSuffixed == 0 || decoded.Suffixes[params[3].Type&0xfffff] != "s" {
			t.Fatal("integer suffix was not preserved")
		}
		soft := decoded.Commands[1].Overloads[0].Parameters[0]
		if soft.Type&(protocol.CommandArgValid|protocol.CommandArgSoftEnum) != protocol.CommandArgValid|protocol.CommandArgSoftEnum || !slices.Equal(decoded.DynamicEnums[soft.Type&0xfffff].Values, []string{"one", "two"}) {
			t.Fatal("soft enum was not preserved")
		}
	}
}

func TestClientHelpDoesNotTriggerCommandOrEnumRefresh(t *testing.T) {
	cmd.Register(cmd.New("help", "Server help", []string{"?"}, commandRegistryFixture{}))
	s := testSession("command-refresh")
	c := &abilityTestControllable{}
	soft := make(map[string]struct{})
	before := s.sendAvailableCommands(c, soft)
	s.testPacket()
	if _, changed := s.resendCommands(before, c, soft); changed {
		t.Error("client-local help caused an unchanged command registry to be resent")
	}
	enums, _ := s.enums(c)
	if _, exists := enums["RegistryChoices"]; exists {
		t.Error("client-local help enum leaked into the network update cache")
	}
}

type commandRegistryFixture struct {
	Set     cmd.SubCommand `cmd:"set"`
	Choice  commandChoices `cmd:"choice"`
	Enabled bool           `cmd:"enabled"`
	Time    int            `cmd:"time,s"`
}

func (commandRegistryFixture) Run(cmd.Source, *cmd.Output, *world.Tx) {}

type commandChoices string

func (commandChoices) Type() string { return "RegistryChoices" }
func (commandChoices) Options(cmd.Source) []string {
	choices := make([]string, 260)
	for i := range choices {
		choices[i] = fmt.Sprintf("choice%03d", i)
	}
	return choices
}

type commandSoftEnumFixture struct{ Choice commandSoftChoices }

func (commandSoftEnumFixture) Run(cmd.Source, *cmd.Output, *world.Tx) {}

type commandSoftChoices string

func (commandSoftChoices) Type() string                { return "RegistrySoftChoices" }
func (commandSoftChoices) Options(cmd.Source) []string { return []string{"one", "two"} }
func (commandSoftChoices) Dynamic() bool               { return true }
