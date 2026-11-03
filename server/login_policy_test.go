package server

import (
	"testing"

	"github.com/df-mc/dragonfly/server/client"
)

// TestValidateName keeps the names of players to the rules gophertunnel
// kept them to.
func TestValidateName(t *testing.T) {
	for name, tc := range map[string]struct {
		id client.Identity
		ok bool
	}{
		"offline":                  {client.Identity{Name: "Steve"}, true},
		"online":                   {client.Identity{Name: "Steve", XUID: "123"}, true},
		"offline letters":          {client.Identity{Name: "Шахтёр"}, true},
		"offline of 16":            {client.Identity{Name: "abcdefghijklmnop"}, true},
		"online of 16":             {client.Identity{Name: "abcdefghijklmnop", XUID: "123"}, false},
		"empty":                    {client.Identity{}, false},
		"name with digit first":    {client.Identity{Name: "1abc"}, false},
		"space first":              {client.Identity{Name: " abc"}, false},
		"space last":               {client.Identity{Name: "abc "}, false},
		"double space":             {client.Identity{Name: "a  b"}, false},
		"online non-Latin":         {client.Identity{Name: "Шахтёр", XUID: "123"}, false},
		"single spaces and digits": {client.Identity{Name: "a b 2"}, true},
	} {
		if err := ValidateName(tc.id); (err == nil) != tc.ok {
			t.Errorf("%s: ValidateName(%+v) = %v, want ok %v", name, tc.id, err, tc.ok)
		}
	}
}
