package builtin

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/sandertv/gophertunnel/minecraft/text"
	"golang.org/x/text/language"
)

func TestBuiltinDescriptionsMatchNativeTranslations(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_commands.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Descriptions map[string]string            `json:"command_descriptions"`
		Languages    map[string]map[string]string `json:"language_samples"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	Register(registerTestAdapter{})
	commands := cmd.Commands()
	for _, locale := range []string{"en_US", "ru_RU", "de_DE"} {
		t.Run(locale, func(t *testing.T) {
			pk, _ := session.BuildAvailableCommands(commands, &descriptionTestSource{locale: language.MustParse(locale)}, make(map[string]struct{}))
			var wire bytes.Buffer
			pk.Marshal(protocol.NewWriter(&wire, 0))
			var decoded packet.AvailableCommands
			decoded.Marshal(protocol.NewReader(&wire, 0, false))
			for _, command := range decoded.Commands {
				key, native := nativeCommandDescriptions[command.Name]
				if !native {
					continue
				}
				if command.Description != key || reference.Languages[locale][key] == "" {
					t.Errorf("%s description %q has no native translation in %s", command.Name, command.Description, locale)
				}
				// Resolve native aliases as well as canonical names: Adamant's
				// experience, whitelist, msg and tp have different BDS names.
				names := append([]string{command.Name}, commands[command.Name].Aliases()...)
				for _, name := range names {
					if expected, exists := reference.Descriptions[name]; exists && command.Description != expected {
						t.Errorf("%s description = %q, BDS %s = %q", command.Name, command.Description, name, expected)
					}
				}
			}
		})
	}
	russianData, err := descriptionFiles.ReadFile("locales/ru.json")
	if err != nil {
		t.Fatal(err)
	}
	var russian map[string]string
	if err := json.Unmarshal(russianData, &russian); err != nil {
		t.Fatal(err)
	}
	for key, russianText := range russian {
		name := strings.TrimSuffix(strings.TrimPrefix(key, "commands."), ".description")
		command, exists := commands[name]
		if !exists || command.DescriptionTranslation().Zero() {
			t.Fatalf("%s has no server description translation", name)
		}
		for _, locale := range []language.Tag{language.Russian, language.MustParse("ru-UA")} {
			expected := russianText
			if locale != language.Russian {
				expected, _ = descriptions.Lookup(locale, key)
			}
			resolved := text.Clean(command.DescriptionTranslation().Resolve(locale))
			if resolved != expected {
				t.Errorf("%s description for %s = %q, want %q", name, locale, resolved, expected)
			}
		}
		for _, locale := range []language.Tag{language.AmericanEnglish, language.German, language.Und} {
			expected, _ := descriptions.Lookup(locale, key)
			if resolved := text.Clean(command.DescriptionTranslation().Resolve(locale)); resolved != expected {
				t.Errorf("%s catalog text for %s = %q, want %q", name, locale, resolved, expected)
			}
		}
	}
	if got := reference.Languages["ru_RU"]["commands.gamemode.description"]; got != "Задает режим игры игрока." {
		t.Fatalf("unexpected native Russian gamemode description: %q", got)
	}
}

func TestHelpKeepsDescriptionTranslations(t *testing.T) {
	Register(registerTestAdapter{})
	help, _ := cmd.ByAlias("help")
	for _, tt := range []struct {
		args, name, prefix string
	}{
		{"gamemode", "gamemode", ""},
		{"about", "about", ""},
		{"", "about", "/about - "},
		{"", "gamemode", "/gamemode - "},
	} {
		src := &descriptionTestSource{locale: language.Russian}
		key := "commands." + tt.name + ".description"
		expected, _ := descriptions.Lookup(src.locale, key)
		fallback, _ := descriptions.Lookup(language.English, key)
		if tt.name == "gamemode" {
			expected = "%commands.gamemode.description"
		}
		help.Execute(tt.args, src, nil)
		found := false
		for _, message := range src.output.Messages() {
			if translated, ok := message.(interface{ Resolve(language.Tag) string }); ok && text.Clean(translated.Resolve(src.locale)) == tt.prefix+expected {
				if text.Clean(message.String()) != tt.prefix+fallback {
					t.Errorf("help %q console output = %q", tt.args, message.String())
				}
				found = true
			}
		}
		if !found {
			t.Errorf("help %q lost translated description %q", tt.args, tt.prefix+expected)
		}
	}
}

type descriptionTestSource struct {
	cmd.Source
	locale language.Tag
	output *cmd.Output
}

func (s descriptionTestSource) Locale() language.Tag             { return s.locale }
func (s *descriptionTestSource) SendCommandOutput(o *cmd.Output) { s.output = o }
