package server

import (
	"testing"

	"github.com/df-mc/dragonfly/server/client"
)

// TestValidateName holds names to the rule of the game, whichever edition
// the player logged in with.
func TestValidateName(t *testing.T) {
	for name, tc := range map[string]struct {
		id client.Identity
		ok bool
	}{
		"letters":           {client.Identity{Name: "Steve"}, true},
		"underscore, digit": {client.Identity{Name: "Steve_1"}, true},
		"digit first":       {client.Identity{Name: "1abc"}, true},
		"other letters":     {client.Identity{Name: "Шахтёр", XUID: "123"}, true},
		"single spaces":     {client.Identity{Name: "a b 2"}, true},
		"16 characters":     {client.Identity{Name: "ШахтёрШахтёрШахт"}, true},
		"17 characters":     {client.Identity{Name: "abcdefghijklmnopq"}, false},
		"empty":             {client.Identity{}, false},
		"colour code":       {client.Identity{Name: "§a"}, false},
		"colour code first": {client.Identity{Name: "§aSteve"}, false},
		"punctuation":       {client.Identity{Name: "Steve!"}, false},
		"symbol":            {client.Identity{Name: "Steve☃"}, false},
		"double space":      {client.Identity{Name: "a  b"}, false},
		"space first":       {client.Identity{Name: " abc"}, false},
		"space last":        {client.Identity{Name: "abc "}, false},
		"invalid UTF-8":     {client.Identity{Name: "Steve\xff"}, false},
	} {
		if err := ValidateName(tc.id); (err == nil) != tc.ok {
			t.Errorf("%s: ValidateName(%+v) = %v, want ok %v", name, tc.id, err, tc.ok)
		}
	}
}
