package builtin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	"golang.org/x/text/language"
)

func TestBuiltinDescriptionsKeepServerTranslations(t *testing.T) {
	Register(registerTestAdapter{})
	commands := cmd.Commands()
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
			resolved := command.DescriptionTranslation().F().Text(locale)
			if !text.Equal(resolved, text.Colourf("%v", expected)) {
				t.Errorf("%s description for %s = %q, want %q", name, locale, resolved, expected)
			}
		}
		for _, locale := range []language.Tag{language.AmericanEnglish, language.German, language.Und} {
			expected, _ := descriptions.Lookup(locale, key)
			if resolved := command.DescriptionTranslation().F().Text(locale); !text.Equal(resolved, text.Colourf("%v", expected)) {
				t.Errorf("%s catalog text for %s = %q, want %q", name, locale, resolved, expected)
			}
		}
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
		description, _ := descriptions.Lookup(src.locale, key)
		fallback, _ := descriptions.Lookup(language.English, key)
		// The text of the server's own catalog, or the key of the
		// client's description, which it looks up.
		expected := text.Colourf(tt.prefix+"%v", description)
		if tt.name == "gamemode" {
			expected = text.Colourf(tt.prefix+"%v", text.Translate(key, fallback))
		}
		help.Execute(tt.args, src, nil)
		found := false
		for _, message := range src.output.Messages() {
			if translated, ok := message.(chat.Texter); ok && text.Equal(translated.Text(src.locale), expected) {
				if text.ParseLegacy(message.String()).Plain() != tt.prefix+fallback {
					t.Errorf("help %q console output = %q", tt.args, message.String())
				}
				found = true
			}
		}
		if !found {
			t.Errorf("help %q lost translated description %#v", tt.args, expected)
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
