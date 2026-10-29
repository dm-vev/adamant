package server

import (
	"encoding/base64"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/login"
)

// TestValidateLogin keeps logins to the rules gophertunnel kept them to,
// and reads the address of the server without resolving it.
func TestValidateLogin(t *testing.T) {
	id := login.Identity{DisplayName: "Steve", Identity: "8b4ec2c6-6f3b-4d0a-9d55-3b7e0c3c3f1a"}
	data := login.DefaultClientData("play.example.invalid:19132", "Steve", "1.26.50")
	if err := validateLogin(id, data); err != nil {
		t.Fatalf("a login the game makes refused: %v", err)
	}
	for name, change := range map[string]func(*login.Identity, *login.ClientData){
		"no UUID":          func(i *login.Identity, _ *login.ClientData) { i.Identity = "x" },
		"name with digit":  func(i *login.Identity, _ *login.ClientData) { i.DisplayName = "1abc" },
		"double space":     func(i *login.Identity, _ *login.ClientData) { i.DisplayName = "a  b" },
		"online non-Latin": func(i *login.Identity, _ *login.ClientData) { i.XUID, i.DisplayName = "123", "Шахтёр" },
		"device":           func(_ *login.Identity, d *login.ClientData) { d.DeviceOS = 99 },
		"language":         func(_ *login.Identity, d *login.ClientData) { d.LanguageCode = "not a language!" },
		"skin size": func(_ *login.Identity, d *login.ClientData) {
			d.SkinData = base64.StdEncoding.EncodeToString([]byte{1})
		},
		"no port": func(_ *login.Identity, d *login.ClientData) { d.ServerAddress = "host" },
		"bad URL": func(_ *login.Identity, d *login.ClientData) { d.ServerAddress = "ftp://host:1:1" },
	} {
		i, d := id, data
		change(&i, &d)
		if err := validateLogin(i, d); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	offline := id
	offline.DisplayName = "Шахтёр"
	if err := validateLogin(offline, data); err != nil {
		t.Errorf("an offline name of letters refused: %v", err)
	}
	signaling := data
	signaling.ServerAddress = "http://127.0.0.1:19132:19132"
	if err := validateLogin(id, signaling); err != nil {
		t.Errorf("the address of a signaling URL refused: %v", err)
	}
}
