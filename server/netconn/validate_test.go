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
