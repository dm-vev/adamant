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
// It refuses the names ReservedName does too.
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
	return ReservedName(name)
}

// ReservedName returns an error for a name that begins with "xuid:" or
// "java:", in any case: the admin commands read those as the start of the
// key of an account, so a player of such a name could not be named by name.
// Every listener refuses such names, whatever rules of its own it keeps.
func ReservedName(name string) error {
	for _, prefix := range []string{"xuid:", "java:"} {
		if len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix) {
			return fmt.Errorf("name may not begin with %q, but got %q", prefix, name)
		}
	}
	return nil
}
