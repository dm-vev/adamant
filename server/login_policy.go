package server

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/client"
)

// ValidateName holds the name a player logged in with to the rules of the
// game: 1 to 16 characters, each a letter, a digit, a space or an
// underscore, with no space first or last and no two spaces in a row.
// Adapters hold names to the rules of their edition first. The game keys
// the whitelist, operators, bans, selectors and chat by names, which
// commands must be able to parse: a listener that admits names past this
// rule admits names those may not name.
func ValidateName(id client.Identity) error {
	name := id.Name
	if n := utf8.RuneCountInString(name); n == 0 || n > 16 {
		return fmt.Errorf("name must be of 1 to 16 characters, but got %d characters", n)
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && (r < '0' || r > '9') && r != ' ' && r != '_' {
			return fmt.Errorf("name must only hold letters, digits, spaces and underscores, but got %q", name)
		}
	}
	if name[0] == ' ' || name[len(name)-1] == ' ' {
		return fmt.Errorf("name may not have a space first or last, but got %q", name)
	}
	if strings.Contains(name, "  ") {
		return fmt.Errorf("name must only have single spaces, but got %q", name)
	}
	return nil
}
