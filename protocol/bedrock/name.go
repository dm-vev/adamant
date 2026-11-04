package bedrockadapter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client"
)

// NameCheck is how strictly the listener holds the names players log in
// with to the rules of Bedrock Edition, which it applies as gophertunnel
// applied them: a name passes the check of its characters if any one of
// them is one the rule allows, so that §aSteve, Steve! and Steve☃ pass.
type NameCheck uint8

const (
	// NameCheckLenient checks names as gophertunnel checked them, and as
	// the server always did: a name passes if any one of its characters is
	// one Bedrock allows. It is the zero value.
	NameCheckLenient NameCheck = iota
	// NameCheckGame checks names as NameCheckLenient does, then holds them
	// to the rule of the game, server.ValidateName: a name with a character
	// other than a letter, a digit, a space or an underscore is refused.
	NameCheckGame
	// NameCheckStrict holds every character of a name to the rule of
	// Bedrock: a Latin letter, a digit or a space for a player of Xbox
	// Live, and a letter, a digit or a space for others. Such a name passes
	// the rule of the game, which the check also applies.
	NameCheckStrict
)

// parseNameCheck returns the NameCheck of a value of the NameCheck key of
// config.toml: "lenient", which an empty value also selects, "game" or
// "strict".
func parseNameCheck(s string) (NameCheck, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "lenient":
		return NameCheckLenient, nil
	case "game":
		return NameCheckGame, nil
	case "strict":
		return NameCheckStrict, nil
	}
	return 0, fmt.Errorf("unknown name check %q: want \"lenient\", \"game\" or \"strict\"", s)
}

// Characters the names of players may hold: Latin letters, digits and
// spaces for players of Xbox Live, any letters for others. The matchers of
// a single character pass a name with one such character anywhere, as
// gophertunnel's did; those of every character pass only names of them.
var (
	onlineNameChar   = regexp.MustCompile("[A-Za-z0-9 ]").MatchString
	offlineNameChar  = regexp.MustCompile(`[\p{L}0-9 ]`).MatchString
	onlineNameChars  = regexp.MustCompile("^[A-Za-z0-9 ]+$").MatchString
	offlineNameChars = regexp.MustCompile(`^[\p{L}0-9 ]+$`).MatchString
)

// validateName holds the name a player logged in with to the rules of
// Bedrock Edition, then, unless check is NameCheckLenient, to those of the
// game.
func validateName(id client.Identity, check NameCheck) error {
	if err := validateBedrockName(id, check == NameCheckStrict); err != nil {
		return err
	}
	if check == NameCheckLenient {
		return nil
	}
	return server.ValidateName(id)
}

// validateBedrockName holds the name of a player that logged in to the
// rules gophertunnel kept it to: of at most 15 bytes for a player of Xbox
// Live, one with an XUID, and 16 for others, with no space first or last,
// no digit first and no two spaces in a row, of Latin letters, digits and
// spaces for a player of Xbox Live and of any letters, digits and spaces for
// others. With every unset, a name passes the rule of its characters if one
// of them fits it, as with gophertunnel; with every set, if all of them do.
func validateBedrockName(id client.Identity, every bool) error {
	nameLimit := 15
	if id.XUID == "" {
		// Clients without an account may take one more character.
		nameLimit = 16
	}
	name := id.Name
	if len(name) == 0 || len(name) > nameLimit {
		return fmt.Errorf("DisplayName must not be empty or longer than %d characters, but got %v characters", nameLimit, len(name))
	}
	if name[0] == ' ' || name[len(name)-1] == ' ' {
		return fmt.Errorf("DisplayName may not have a space as first/last character, but got %v", name)
	}
	if name[0] >= '0' && name[0] <= '9' {
		return fmt.Errorf("DisplayName may not have a number as first character, but got %v", name)
	}
	online, offline := onlineNameChar, offlineNameChar
	if every {
		online, offline = onlineNameChars, offlineNameChars
	}
	if id.XUID != "" {
		if !online(name) {
			return fmt.Errorf("DisplayName for authorized client must only contain numbers, Latin letters and spaces, but got %v", name)
		}
	} else if !offline(name) {
		return fmt.Errorf("DisplayName for unauthorized client must only contain numbers, letters and spaces, but got %v", name)
	}
	if strings.Contains(name, "  ") {
		return fmt.Errorf("DisplayName must only have single spaces, but got %v", name)
	}
	return nil
}
