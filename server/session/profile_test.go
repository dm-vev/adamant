package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/dm-vev/adamant/net/bedrock/login"
)

// profileConn is a connection whose client data is set by the test.
type profileConn struct {
	benchmarkConn
	data login.ClientData
}

func (c profileConn) ClientData() login.ClientData { return c.data }

// TestProfileFromClientData holds the profile of a session to the client
// data its connection logged in with: the fields Player read from
// ClientData before.
func TestProfileFromClientData(t *testing.T) {
	s := &Session{conn: profileConn{data: login.ClientData{DeviceID: "3f2a", DeviceModel: "SM-G991B", SelfSignedID: "a6bd0a0b-6c7b-4d35-9f6e-3a37c8bd0a5f", LanguageCode: "en_US"}}}
	want := client.Profile{DeviceID: "3f2a", DeviceModel: "SM-G991B", SelfSignedID: "a6bd0a0b-6c7b-4d35-9f6e-3a37c8bd0a5f"}
	if got := s.Profile(); got != want {
		t.Errorf("Profile() = %+v, want %+v", got, want)
	}
	if got := Nop.Profile(); got != (client.Profile{}) {
		t.Errorf("Nop.Profile() = %+v, want the zero Profile", got)
	}
}
