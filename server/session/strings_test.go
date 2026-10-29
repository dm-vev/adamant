package session

import (
	"strings"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestValidText(t *testing.T) {
	for _, tc := range []struct {
		s    string
		max  int
		want bool
	}{
		{strings.Repeat("a", 512), 512, true},
		{strings.Repeat("a", 513), 512, false},
		// Characters count, not bytes.
		{strings.Repeat("ж", 512), 512, true},
		{strings.Repeat("ж", 513), 512, false},
		{"a\xffb", 512, false},
		{"", 32, true},
	} {
		if got := validText(tc.s, tc.max); got != tc.want {
			t.Errorf("%d bytes of max %d: %v, want %v", len(tc.s), tc.max, got, tc.want)
		}
	}
}

type chatControllable struct {
	Controllable
	said []string
}

func (c *chatControllable) Chat(msg ...any) { c.said = append(c.said, msg[0].(string)) }

// TestChatOverItsLengthIsDropped sends chat messages as long as BDS passes
// on and longer: the longer ones are dropped, as BDS drops them, rather
// than passed on to every player.
func TestChatOverItsLengthIsDropped(t *testing.T) {
	s := testSession("Talker")
	c := &chatControllable{}
	for _, msg := range []string{strings.Repeat("a", maxChatLength), strings.Repeat("b", maxChatLength+1), strings.Repeat("c", 1<<20), "bad \xff UTF-8"} {
		pk := &v2193.TextPacket{TextType: packet.TextTypeChat, SourceName: "Talker", Message: msg}
		if err := (TextHandler{}).Handle(pk, s, nil, c); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.said) != 1 || c.said[0] != strings.Repeat("a", maxChatLength) {
		t.Fatalf("said %d messages", len(c.said))
	}
}
