package i18n

import (
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/text/language"
)

func TestCatalogLocaleAndMessageFallback(t *testing.T) {
	// A new language is discovered from its filename, without a registration
	// list. Regional catalogs and base catalogs may both be partial.
	files := catalogFiles(map[string]string{
		"en":    `{"greeting":"Hello", "other":"English fallback", "percent":"100%"}`,
		"fr":    `{"greeting":"Bonjour", "other":"Autre"}`,
		"fr-CA": `{"greeting":"Allô"}`,
		"ru":    `{"greeting":"Привет"}`,
	})
	catalog, err := Load(files, "locales", language.English)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		locale, key, want string
	}{
		{"fr-CA", "greeting", "Allô"},
		{"fr-CA", "other", "Autre"},
		{"fr-BE", "greeting", "Bonjour"},
		{"ru_RU", "greeting", "Привет"},
		{"ru-UA", "other", "English fallback"},
		{"ja-JP", "greeting", "Hello"},
		{"und", "greeting", "Hello"},
		{"fr-CA", "percent", "100%"},
	} {
		t.Run(tt.locale+"/"+tt.key, func(t *testing.T) {
			got, ok := catalog.Lookup(language.MustParse(tt.locale), tt.key)
			if !ok || got != tt.want {
				t.Fatalf("Lookup(%s, %s) = %q, %v; want %q", tt.locale, tt.key, got, ok, tt.want)
			}
		})
	}
	if got, ok := catalog.Lookup(language.French, "missing"); ok || got != "" {
		t.Fatalf("missing key = %q, %v", got, ok)
	}
}

func TestCatalogRejectsInvalidTranslations(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files map[string]string
		err   string
	}{
		{"missing-fallback", map[string]string{"fr": `{"key":"Bonjour"}`}, "fallback catalog"},
		{"empty-fallback", map[string]string{"en": `{}`}, "fallback catalog"},
		{"unknown-key", map[string]string{"en": `{"key":"Hello"}`, "fr": `{"typo":"Bonjour"}`}, "unknown key"},
		{"duplicate-key", map[string]string{"en": `{"key":"Hello", "key":"Hi"}`}, "duplicate key"},
		{"duplicate-locale", map[string]string{"en": `{"key":"Hello"}`, "en-US": `{}`, "en_US": `{}`}, "duplicate locale"},
		{"invalid-locale", map[string]string{"en": `{"key":"Hello"}`, "not a locale": `{}`}, "locale filename"},
		{"undefined-locale", map[string]string{"en": `{"key":"Hello"}`, "und": `{}`}, "locale filename"},
		{"empty-key", map[string]string{"en": `{"":"Hello"}`}, "empty message key"},
		{"empty-value", map[string]string{"en": `{"key":" "}`}, "empty translation"},
		{"null-value", map[string]string{"en": `{"key":null}`}, "empty translation"},
		{"number-value", map[string]string{"en": `{"key":42}`}, "cannot unmarshal number"},
		{"array", map[string]string{"en": `[]`}, "JSON object"},
		{"truncated", map[string]string{"en": `{"key":"Hello"`}, "end of JSON"},
		{"trailing-data", map[string]string{"en": `{"key":"Hello"}{}`}, "unexpected data"},
		{"invalid-utf8", map[string]string{"en": "{\"key\":\"\xff\"}"}, "UTF-8"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(catalogFiles(tt.files), "locales", language.English)
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("Load() error = %v, want %q", err, tt.err)
			}
		})
	}
}

func catalogFiles(messages map[string]string) fstest.MapFS {
	files := make(fstest.MapFS)
	for locale, data := range messages {
		files["locales/"+locale+".json"] = &fstest.MapFile{Data: []byte(data)}
	}
	return files
}
