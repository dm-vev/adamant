// Package i18n loads immutable, keyed message catalogs with per-message locale
// fallback. It is intended for server text absent from the Bedrock client.
package i18n

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/language"
)

// Catalog holds UTF-8 messages indexed by locale and stable message key. Once
// loaded, a Catalog may be used concurrently without synchronization.
type Catalog struct {
	messages map[language.Tag]map[string]string
	fallback language.Tag
}

// Load reads dir/*.json from files. Filenames are BCP 47 language tags (for
// example en.json or pt-BR.json); Bedrock's underscore notation is also accepted.
// The fallback catalog defines all valid keys. Other catalogs may be partial.
// Invalid JSON, duplicate locales or keys, unknown keys and blank messages are
// errors, so translation mistakes cannot silently hide behind a fallback.
func Load(files fs.FS, dir string, fallback language.Tag) (*Catalog, error) {
	names, err := fs.Glob(files, path.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	c := &Catalog{messages: make(map[language.Tag]map[string]string), fallback: fallback}
	for _, name := range names {
		locale, err := language.Parse(strings.TrimSuffix(path.Base(name), ".json"))
		if err != nil || locale == language.Und {
			return nil, fmt.Errorf("i18n: invalid locale filename %q", name)
		}
		if _, exists := c.messages[locale]; exists {
			return nil, fmt.Errorf("i18n: duplicate locale %s in %q", locale, name)
		}
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, err
		}
		messages, err := decodeMessages(data)
		if err != nil {
			return nil, fmt.Errorf("i18n: %s: %w", name, err)
		}
		c.messages[locale] = messages
	}
	base, exists := c.messages[fallback]
	if !exists || len(base) == 0 {
		return nil, fmt.Errorf("i18n: missing or empty fallback catalog %s", fallback)
	}
	for locale, messages := range c.messages {
		for key := range messages {
			if _, exists := base[key]; !exists {
				return nil, fmt.Errorf("i18n: unknown key %q in %s", key, locale)
			}
		}
	}
	return c, nil
}

// Lookup finds key in the requested locale, then its CLDR parents, then the
// fallback catalog. For example, fr-CA may override only a few entries in fr.
// False is returned only if the fallback catalog does not define key.
func (c *Catalog) Lookup(locale language.Tag, key string) (string, bool) {
	for tag := locale; tag != language.Und; tag = tag.Parent() {
		if message, exists := c.messages[tag][key]; exists {
			return message, true
		}
	}
	message, exists := c.messages[c.fallback][key]
	return message, exists
}

func decodeMessages(data []byte) (map[string]string, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("catalog must be valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("expected a JSON object of message keys and strings")
	}
	messages := make(map[string]string)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key := token.(string)
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("empty message key")
		}
		if _, exists := messages[key]; exists {
			return nil, fmt.Errorf("duplicate key %q", key)
		}
		var message string
		if err := decoder.Decode(&message); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		if strings.TrimSpace(message) == "" {
			return nil, fmt.Errorf("empty translation for %q (omit it to use the fallback)", key)
		}
		messages[key] = message
	}
	if _, err := decoder.Token(); err != nil {
		if err == io.EOF {
			// The stream ended before the closing brace.
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("unexpected end of JSON input: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after the message catalog")
	}
	return messages, nil
}
