package server

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/google/uuid"
)

// Characters the names of players may hold: Latin letters, digits and
// spaces for players of Xbox Live, any letters for others.
var (
	onlineNameChar  = regexp.MustCompile("[A-Za-z0-9 ]").MatchString
	offlineNameChar = regexp.MustCompile(`[\p{L}0-9 ]`).MatchString
)

// validateIdentity holds the identity of a client that logged in to the
// rules the server keeps it to, those gophertunnel kept it to: a valid UUID
// and XUID, and a name of the game's form.
func validateIdentity(data login.Identity) error {
	if _, err := strconv.ParseInt(data.XUID, 10, 64); err != nil && len(data.XUID) != 0 {
		return fmt.Errorf("XUID must be parseable as an int64, but got %v", data.XUID)
	}
	if id, err := uuid.Parse(data.Identity); err != nil || id == uuid.Nil {
		return fmt.Errorf("UUID must be parseable as a valid UUID, but got %v", data.Identity)
	}
	nameLimit := 15
	if data.XUID == "" {
		// Clients without an account may take one more character.
		nameLimit = 16
	}
	name := data.DisplayName
	if len(name) == 0 || len(name) > nameLimit {
		return fmt.Errorf("DisplayName must not be empty or longer than %d characters, but got %v characters", nameLimit, len(name))
	}
	if name[0] == ' ' || name[len(name)-1] == ' ' {
		return fmt.Errorf("DisplayName may not have a space as first/last character, but got %v", name)
	}
	if name[0] >= '0' && name[0] <= '9' {
		return fmt.Errorf("DisplayName may not have a number as first character, but got %v", name)
	}
	if data.XUID != "" {
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
