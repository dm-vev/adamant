package session_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/cmd/builtin"
	"github.com/df-mc/dragonfly/server/text"
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
	// Registering the builtin commands is for the whole process, and the
	// sessions of other tests would see them: run where none can.
	if isolated(t) {
		return
	}
	builtin.Register((*server.Server)(nil))
	commands := cmd.Commands()
	for _, locale := range []string{"en_US", "ru_RU", "de_DE"} {
		t.Run(locale, func(t *testing.T) {
			pk, _ := session.BuildAvailableCommands(commands, &descriptionTestSource{locale: language.MustParse(locale)}, make(map[string]struct{}))
			decoded := decodeCommands(t, encodePacket(pk))
			for _, command := range decoded.Commands {
				key, native := nativeDescriptionKey(commands[command.Name])
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
	if got := reference.Languages["ru_RU"]["commands.gamemode.description"]; got != "Задает режим игры игрока." {
		t.Fatalf("unexpected native Russian gamemode description: %q", got)
	}
}

// nativeDescriptionKey returns the key of the client's own translation that
// the description of the command c resolves to, if it resolves to one rather
// than to a text of the server's catalog.
func nativeDescriptionKey(c cmd.Command) (key string, ok bool) {
	description := c.DescriptionTranslation()
	if description.Zero() {
		return "", false
	}
	return translationKey(description.F().Text(language.English))
}

// translationKey returns the key of the first translation in t.
func translationKey(t text.Text) (string, bool) {
	if key, _, ok := t.Translation(); ok {
		return key, true
	}
	for child := range t.Children() {
		if key, ok := translationKey(child); ok {
			return key, true
		}
	}
	return "", false
}

type descriptionTestSource struct {
	cmd.Source
	locale language.Tag
}

func (s descriptionTestSource) Locale() language.Tag { return s.locale }
