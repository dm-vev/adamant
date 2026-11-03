package server

import (
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/login"
)

// TestValidateLogin keeps the identities of logins to the rules
// gophertunnel kept them to, and their client data to those of
// netconn.ValidateClientData.
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
		"client data":      func(_ *login.Identity, d *login.ClientData) { d.DeviceOS = 99 },
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
}
