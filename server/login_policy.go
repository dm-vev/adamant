package server

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/df-mc/dragonfly/server/client"
)

// Characters the names of players may hold: Latin letters, digits and
// spaces for players of Xbox Live, any letters for others.
var (
	onlineNameChar  = regexp.MustCompile("[A-Za-z0-9 ]").MatchString
	offlineNameChar = regexp.MustCompile(`[\p{L}0-9 ]`).MatchString
)

// ValidateName holds the name of a player that logged in to the rules the
// server keeps it to, those gophertunnel kept it to: one of the game's form,
// of at most 15 characters for a player of Xbox Live, one with an XUID, and
// 16 for others, with no space first or last, no digit first and no two
// spaces in a row, of Latin letters, digits and spaces for a player of Xbox
// Live and of any letters, digits and spaces for others.
func ValidateName(id client.Identity) error {
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
	if id.XUID != "" {
		if !onlineNameChar(name) {
			return fmt.Errorf("DisplayName for authorized client must only contain numbers, Latin letters and spaces, but got %v", name)
		}
	} else if !offlineNameChar(name) {
		return fmt.Errorf("DisplayName for unauthorized client must only contain numbers, letters and spaces, but got %v", name)
	}
	if strings.Contains(name, "  ") {
		return fmt.Errorf("DisplayName must only have single spaces, but got %v", name)
	}
	return nil
}
