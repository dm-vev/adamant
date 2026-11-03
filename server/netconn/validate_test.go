package netconn

import (
	"encoding/base64"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/login"
)

// TestValidateClientData keeps the client data of logins to the rules
// gophertunnel kept it to, and reads the address of the server without
// resolving it.
func TestValidateClientData(t *testing.T) {
	data := login.DefaultClientData("play.example.invalid:19132", "Steve", "1.26.50")
	if err := ValidateClientData(data); err != nil {
		t.Fatalf("client data the game makes refused: %v", err)
	}
	for name, change := range map[string]func(*login.ClientData){
		"device":   func(d *login.ClientData) { d.DeviceOS = 99 },
		"language": func(d *login.ClientData) { d.LanguageCode = "not a language!" },
		"skin size": func(d *login.ClientData) {
			d.SkinData = base64.StdEncoding.EncodeToString([]byte{1})
		},
		"no port": func(d *login.ClientData) { d.ServerAddress = "host" },
		"bad URL": func(d *login.ClientData) { d.ServerAddress = "ftp://host:1:1" },
	} {
		d := data
		change(&d)
		if err := ValidateClientData(d); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	signaling := data
	signaling.ServerAddress = "http://127.0.0.1:19132:19132"
	if err := ValidateClientData(signaling); err != nil {
		t.Errorf("the address of a signaling URL refused: %v", err)
	}
}

// TestValidateIdentity keeps the IDs of logins to the forms gophertunnel
// kept them to.
func TestValidateIdentity(t *testing.T) {
	const id = "8b4ec2c6-6f3b-4d0a-9d55-3b7e0c3c3f1a"
	for name, tc := range map[string]struct {
		id login.Identity
		ok bool
	}{
		"offline":      {login.Identity{Identity: id}, true},
		"online":       {login.Identity{Identity: id, XUID: "2535400000000042"}, true},
		"no UUID":      {login.Identity{}, false},
		"bad UUID":     {login.Identity{Identity: "x"}, false},
		"nil UUID":     {login.Identity{Identity: "00000000-0000-0000-0000-000000000000"}, false},
		"bad XUID":     {login.Identity{Identity: id, XUID: "x"}, false},
		"name ignored": {login.Identity{Identity: id, DisplayName: "1  "}, true},
	} {
		if err := ValidateIdentity(tc.id); (err == nil) != tc.ok {
			t.Errorf("%s: ValidateIdentity = %v, want ok %v", name, err, tc.ok)
		}
	}
}
