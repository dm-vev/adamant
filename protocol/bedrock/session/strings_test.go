package session

import (
	"strings"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

type chatControllable struct {
	Controllable
	said []string
}

func (c *chatControllable) Chat(msg ...any) { c.said = append(c.said, msg[0].(string)) }

// TestChatOverItsLengthIsDropped sends chat messages as long as BDS passes
// on and longer: the longer ones are dropped, as BDS drops them, rather
// than passed on to every player.
func TestChatOverItsLengthIsDropped(t *testing.T) {
	t.Parallel()
	s := testSession("Talker")
	c := &chatControllable{}
	for _, msg := range []string{strings.Repeat("a", maxChatLength), strings.Repeat("b", maxChatLength+1), strings.Repeat("c", 1<<20), "bad \xff UTF-8"} {
		pk := &v2193.TextPacket{TextType: v2193.TextTypeChat, SourceName: "Talker", Message: msg}
		if err := (TextHandler{}).Handle(pk, s, nil, c); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.said) != 1 || c.said[0] != strings.Repeat("a", maxChatLength) {
		t.Fatalf("said %d messages", len(c.said))
	}
}
