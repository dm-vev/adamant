package session

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/text"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// The tests of this file hold the packets that the session writes for the
// structured text it is given to those it wrote, before it took text, for
// the legacy string the text is read from. The old bodies of the methods are
// kept here as the oracle, each writing what the method wrote for a string.

// textMethod is a method of the session that shows text, in the new code
// and in the old.
type textMethod struct {
	name string
	new  func(s *Session, t text.Text)
	old  func(s *Session, str string)
}

// textMethods are the methods FuzzTextPackets checks.
var textMethods = []textMethod{
	{"SendMessage", func(s *Session, t text.Text) { s.SendMessage(t) }, oldSendMessage},
	{"Disconnect", func(s *Session, t text.Text) { s.Disconnect(t) }, oldDisconnect},
}

// oldDisconnect is Disconnect as it was for a string.
func oldDisconnect(s *Session, message string) {
	if s != Nop {
		s.connWriteMu.Lock()
		_ = s.conn.WritePacket(&v2193.DisconnectPacket{
			HideDisconnectionScreen: message == "",
			Message:                 message,
		})
		_ = s.conn.Flush()
		s.connWriteMu.Unlock()
	}
}

// oldSendMessage is SendMessage as it was for a string.
func oldSendMessage(s *Session, message string) {
	if message == "" {
		message = " "
	}
	s.writePacket(&v2193.TextPacket{
		TextType: v2193.TextTypeRaw,
		Message:  message,
	})
}

// checkTextPackets fails t if a method of the session writes, for
// text.ParseLegacy(str), other bytes than it wrote for str.
func checkTextPackets(t *testing.T, str string) {
	t.Helper()
	for _, m := range textMethods {
		nw, old := newCaptureSession(), newCaptureSession()
		m.new(nw, text.ParseLegacy(str))
		m.old(old, str)
		got, want := nw.taken(), old.taken()
		if len(got) != len(want) {
			t.Fatalf("%s(%q): %d packets, the old code wrote %d", m.name, str, len(got), len(want))
		}
		for i := range got {
			if g, w := marshalPacketBytes(t, got[i]), marshalPacketBytes(t, want[i]); !bytes.Equal(g, w) {
				t.Fatalf("%s(%q): packet %d is\n%#v\nthe old code wrote\n%#v", m.name, str, i, got[i], want[i])
			}
		}
	}
}

// textPacketSeeds are strings with § codes and the like, which the seeds of
// FuzzTextPackets add to those of FuzzLegacy of protocol/bedrock/text.
var textPacketSeeds = []string{
	"", " ", "Hello", "§cRed §lbold§r", "§", "a§", "§zkept", "§r", "§r§e%disconnect.disconnected§r",
	"%key", "100%", "%%", "§k§l§o§m§n", "§#ff0000", "Привет §aмир", "line\nline", "§§c", "§g§h§i§j§p§q§s§t§u§v",
	"§l§cA§rB§oC", "trailing§r§r", "<red>markup</red>", "&amp;",
}

// legacyCorpus returns the strings of the corpus of FuzzLegacy in
// protocol/bedrock/text.
func legacyCorpus(t testing.TB) []string {
	t.Helper()
	dir := filepath.Join("..", "..", "protocol", "bedrock", "text", "testdata", "fuzz", "FuzzLegacy")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[1], "string(") {
			t.Fatalf("%s is no corpus entry of one string", e.Name())
		}
		s, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(lines[1], "string("), ")"))
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		out = append(out, s)
	}
	return out
}

// TestTextPackets checks the seeds of FuzzTextPackets.
func TestTextPackets(t *testing.T) {
	for _, s := range append(textPacketSeeds, legacyCorpus(t)...) {
		checkTextPackets(t, s)
	}
}

// FuzzTextPackets holds the packets the session writes for the text of any
// legacy string to those it wrote for the string.
func FuzzTextPackets(f *testing.F) {
	for _, s := range append(textPacketSeeds, legacyCorpus(f)...) {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		checkTextPackets(t, s)
	})
}
