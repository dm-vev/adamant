package session

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
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
	{"SendTip", func(s *Session, t text.Text) { s.SendTip(t) }, oldTextPacket(v2193.TextTypeTip)},
	{"SendPopup", func(s *Session, t text.Text) { s.SendPopup(t) }, oldTextPacket(v2193.TextTypePopup)},
	{"SendJukeboxPopup", func(s *Session, t text.Text) { s.SendJukeboxPopup(t) }, oldTextPacket(v2193.TextTypeJukeboxPopup)},
	{"SendToast", func(s *Session, t text.Text) { s.SendToast(t, t) }, func(s *Session, str string) { oldSendToast(s, str, str) }},
	{"SendToast title", func(s *Session, t text.Text) { s.SendToast(t, text.Literal("message")) }, func(s *Session, str string) { oldSendToast(s, str, "message") }},
	{"SendToast message", func(s *Session, t text.Text) { s.SendToast(text.ParseLegacy("§aTitle"), t) }, func(s *Session, str string) { oldSendToast(s, "§aTitle", str) }},
	{"SendTitle", func(s *Session, t text.Text) { s.SendTitle(t) }, oldSetTitle(v2193.TitleActionSetTitle)},
	{"SendSubtitle", func(s *Session, t text.Text) { s.SendSubtitle(t) }, oldSetTitle(v2193.TitleActionSetSubtitle)},
	{"SendActionBarMessage", func(s *Session, t text.Text) { s.SendActionBarMessage(t) }, oldSetTitle(v2193.TitleActionSetActionBar)},
	{"SendBossBarID", sendBossBars, oldSendBossBars},
}

// bossBarColours are the colours of boss bars.
var bossBarColours = []bossbar.Colour{
	bossbar.Pink(), bossbar.Blue(), bossbar.Red(), bossbar.Green(), bossbar.Yellow(), bossbar.Purple(),
	bossbar.RebeccaPurple(), bossbar.White(),
}

// sendBossBars shows boss bars of the title t in every colour, of the
// default ID and of another, and the default one through SendBossBar.
func sendBossBars(s *Session, t text.Text) {
	for i, c := range bossBarColours {
		s.SendBossBarID("default", t, c, float64(i)/8)
		s.SendBossBarID("other", t, c, 1)
	}
	s.SendBossBar(t, bossbar.Red(), 0.5)
}

// oldSendBossBars is sendBossBars with SendBossBarID as it was for a string.
func oldSendBossBars(s *Session, title string) {
	oldSendBossBarID := func(id, text string, colour uint8, healthPercentage float64) {
		s.writePacket(&v2193.BossEventPacket{
			BossEntityUniqueID: s.bossBarEntityID(id),
			EventType:          v2193.BossEventShow,
			BossBarTitle:       text,
			HealthPercentage:   float32(healthPercentage),
			Colour:             colour,
		})
	}
	// The colours were their own numbers, in the order of bossBarColours.
	for i := range bossBarColours {
		oldSendBossBarID("default", title, uint8(i), float64(i)/8)
		oldSendBossBarID("other", title, uint8(i), 1)
	}
	oldSendBossBarID("default", title, 2, 0.5)
}

// oldTextPacket returns SendTip, SendPopup or SendJukeboxPopup as it was for
// a string: a Text of the type passed.
func oldTextPacket(textType byte) func(s *Session, message string) {
	return func(s *Session, message string) {
		s.writePacket(&v2193.TextPacket{
			TextType: textType,
			Message:  message,
		})
	}
}

// oldSendToast is SendToast as it was for strings.
func oldSendToast(s *Session, title, message string) {
	s.writePacket(&v2193.ToastRequestPacket{
		Title:   title,
		Message: message,
	})
}

// oldSetTitle returns SendTitle, SendSubtitle or SendActionBarMessage as it
// was for a string: a SetTitle of the action passed.
func oldSetTitle(action int32) func(s *Session, message string) {
	return func(s *Session, message string) {
		s.writePacket(&v2193.SetTitlePacket{ActionType: action, Text: message})
	}
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

// tagEntity is an entity of a view layer: its handle alone.
type tagEntity struct {
	world.Entity
	h *world.EntityHandle
}

func (e tagEntity) H() *world.EntityHandle { return e.h }

// checkViewTags fails t if the session stores, in its view layer, other name
// and score tags for text.ParseLegacy(str) than it stored for str.
func checkViewTags(t *testing.T, str string) {
	t.Helper()
	s := &Session{viewLayer: world.NewViewLayer(nil)}
	e := tagEntity{h: entity.NewText("tagged", mgl64.Vec3{})}
	s.ViewNameTag(e, text.ParseLegacy(str))
	s.ViewScoreTag(e, text.ParseLegacy(str))
	// The old ViewNameTag and ViewScoreTag stored the string as it is.
	if got, ok := s.viewLayer.NameTag(e); !ok || got != str {
		t.Fatalf("ViewNameTag(%q) stored %q (%v)", str, got, ok)
	}
	if got, ok := s.viewLayer.ScoreTag(e); !ok || got != str {
		t.Fatalf("ViewScoreTag(%q) stored %q (%v)", str, got, ok)
	}
}

// checkTextPackets fails t if a method of the session writes, for
// text.ParseLegacy(str), other bytes than it wrote for str.
func checkTextPackets(t *testing.T, str string) {
	t.Helper()
	checkViewTags(t, str)
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
	dir := filepath.Join("..", "text", "testdata", "fuzz", "FuzzLegacy")
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
