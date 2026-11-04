package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"golang.org/x/text/language"
)

// TestDisconnectReasons checks the reasons clients are disconnected with:
// the message of a server that shuts down is the key the client translates,
// as Resolve gave it before, in every language; a message of the server is
// shown as it is, and an empty one hides the screen.
func TestDisconnectReasons(t *testing.T) {
	for _, c := range []struct {
		name string
		t    text.Text
		want mc.DisconnectPacket
	}{
		{"shutdown en", chat.MessageServerDisconnect.F().Text(language.English), mc.DisconnectPacket{Message: "§r§e%disconnect.disconnected§r"}},
		{"shutdown ru", chat.MessageServerDisconnect.F().Text(language.Russian), mc.DisconnectPacket{Message: "§r§e%disconnect.disconnected§r"}},
		{"closed", text.Literal("Connection closed."), mc.DisconnectPacket{Message: "Connection closed."}},
		{"banned", text.ParseLegacy("§cYou are banned"), mc.DisconnectPacket{Message: "§cYou are banned"}},
		{"empty", text.Literal(""), mc.DisconnectPacket{HideDisconnectionScreen: true}},
	} {
		s := newCaptureSession()
		s.Disconnect(c.t)
		pks := s.taken()
		if len(pks) != 1 {
			t.Fatalf("%s: %d packets, want 1", c.name, len(pks))
		}
		if got, ok := pks[0].(*mc.DisconnectPacket); !ok || *got != c.want {
			t.Errorf("%s: %#v, want %#v", c.name, pks[0], c.want)
		}
	}
}
