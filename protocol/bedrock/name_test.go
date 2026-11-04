package bedrockadapter

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client"
)

// TestValidateBedrockName keeps the names of players to the rules
// gophertunnel kept them to. The cases are those server.ValidateName was
// held to when it held names to them.
func TestValidateBedrockName(t *testing.T) {
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
		if err := validateBedrockName(tc.id, false); (err == nil) != tc.ok {
			t.Errorf("%s: validateBedrockName(%+v) = %v, want ok %v", name, tc.id, err, tc.ok)
		}
	}
}

// TestNameChecks holds each NameCheck to the names it admits: names with a
// character Bedrock's rule allows anywhere pass the lenient check, as they
// always did, the game's rule refuses those of characters it does not take,
// and the strict check those of any character Bedrock's rule does not allow.
func TestNameChecks(t *testing.T) {
	type admits struct{ lenient, game, strict bool }
	for name, tc := range map[string]struct {
		id   client.Identity
		want admits
	}{
		"plain":                {client.Identity{Name: "Steve", XUID: "1"}, admits{true, true, true}},
		"colour code":          {client.Identity{Name: "§aSteve", XUID: "1"}, admits{true, false, false}},
		"punctuation":          {client.Identity{Name: "Steve!", XUID: "1"}, admits{true, false, false}},
		"symbol":               {client.Identity{Name: "Steve☃"}, admits{true, false, false}},
		"underscore":           {client.Identity{Name: "Steve_1", XUID: "1"}, admits{true, true, false}},
		"online, a Latin one":  {client.Identity{Name: "Шахтёрa", XUID: "1"}, admits{true, true, false}},
		"offline letters":      {client.Identity{Name: "Шахтёр"}, admits{true, true, true}},
		"digit first":          {client.Identity{Name: "1abc"}, admits{false, false, false}},
		"double space":         {client.Identity{Name: "a  b"}, admits{false, false, false}},
		"online, no Latin one": {client.Identity{Name: "Шахтёр", XUID: "1"}, admits{false, false, false}},
	} {
		got := admits{
			validateName(tc.id, NameCheckLenient) == nil,
			validateName(tc.id, NameCheckGame) == nil,
			validateName(tc.id, NameCheckStrict) == nil,
		}
		if got != tc.want {
			t.Errorf("%s: %q admitted lenient, game, strict %v, want %v", name, tc.id.Name, got, tc.want)
		}
	}
	// A name Bedrock's rule refuses is refused with its message, whichever
	// the check.
	for _, check := range []NameCheck{NameCheckLenient, NameCheckGame, NameCheckStrict} {
		if err := validateName(client.Identity{Name: "1abc"}, check); err == nil || !strings.HasPrefix(err.Error(), "DisplayName may not have a number") {
			t.Errorf("check %d: 1abc refused with %v", check, err)
		}
	}
}

// TestParseNameCheck reads the values of the NameCheck key.
func TestParseNameCheck(t *testing.T) {
	for s, want := range map[string]NameCheck{"": NameCheckLenient, "lenient": NameCheckLenient, " Game ": NameCheckGame, "strict": NameCheckStrict} {
		if got, err := parseNameCheck(s); err != nil || got != want {
			t.Errorf("parseNameCheck(%q) = %v, %v, want %v", s, got, err, want)
		}
	}
	if _, err := parseNameCheck("xbox"); err == nil {
		t.Error("parseNameCheck(\"xbox\") passed")
	}
}

// FuzzBedrockNamesPassTheGameRule checks that every name whose characters
// all fit the rule of Bedrock passes the rule of the game, so that the
// strict check refuses no name for the game's rule, and with a message
// other than Bedrock's.
func FuzzBedrockNamesPassTheGameRule(f *testing.F) {
	for _, name := range []string{"Steve", "Шахтёр", "a b 2", "abcdefghijklmnop", "§aSteve", "Steve_1", "ШахтёрШахтёрШах", "a\xffb", "ǅ"} {
		f.Add(name, false)
		f.Add(name, true)
	}
	f.Fuzz(func(t *testing.T, name string, online bool) {
		id := client.Identity{Name: name}
		if online {
			id.XUID = "1"
		}
		if validateBedrockName(id, true) != nil {
			return
		}
		if err := server.ValidateName(id); err != nil {
			t.Errorf("%q passes the strict rule of Bedrock (online %v) but not the game's: %v", name, online, err)
		}
	})
}
