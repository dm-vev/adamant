package session

import (
	"bytes"
	"errors"
	"testing"

	bedrocktext "github.com/df-mc/dragonfly/protocol/bedrock/text"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"golang.org/x/text/language"
)

// The tests of this file hold what the session writes for translations, in
// chat messages and in the output of commands, to what it wrote when it was
// handed the translation, or the cmd.Output, itself. The old code is kept
// here as the oracle.

// repoTranslation is a translation of the repository and the count of its
// parameters.
type repoTranslation struct {
	t      chat.Translation
	params int
}

// repoTranslations are the translations of the chat and cmd packages, by
// name.
var repoTranslations = map[string]repoTranslation{
	"chat.MessageJoin":                  {chat.MessageJoin, 1},
	"chat.MessageQuit":                  {chat.MessageQuit, 1},
	"chat.MessageServerDisconnect":      {chat.MessageServerDisconnect, 0},
	"chat.MessageBedTooFar":             {chat.MessageBedTooFar, 0},
	"chat.MessageBedObstructed":         {chat.MessageBedObstructed, 0},
	"chat.MessageRespawnPointSet":       {chat.MessageRespawnPointSet, 0},
	"chat.MessageNoSleep":               {chat.MessageNoSleep, 0},
	"chat.MessageNoSleepMonsters":       {chat.MessageNoSleepMonsters, 0},
	"chat.MessageBedIsOccupied":         {chat.MessageBedIsOccupied, 0},
	"chat.MessageSleeping":              {chat.MessageSleeping, 2},
	"chat.MessageRespawnAnchorNotValid": {chat.MessageRespawnAnchorNotValid, 0},
	"chat.MessageBedNotValid":           {chat.MessageBedNotValid, 0},
	"chat.MessageAdminCommand":          {chat.MessageAdminCommand, 2},
	"cmd.MessageSyntax":                 {cmd.MessageSyntax, 3},
	"cmd.MessageUsage":                  {cmd.MessageUsage, 1},
	"cmd.MessageUnknown":                {cmd.MessageUnknown, 1},
	"cmd.MessageNoTargets":              {cmd.MessageNoTargets, 0},
	"cmd.MessageNumberInvalid":          {cmd.MessageNumberInvalid, 1},
	"cmd.MessageBooleanInvalid":         {cmd.MessageBooleanInvalid, 1},
	"cmd.MessagePlayerNotFound":         {cmd.MessagePlayerNotFound, 0},
	"cmd.MessageParameterInvalid":       {cmd.MessageParameterInvalid, 1},
}

// translationArgs are arguments of each kind a translation is filled in
// with: strings with and without codes, a number, translations with and
// without parameters, and one filled in.
var translationArgs = []any{
	"Steve", "§cSte§lve§r", "", "100%", 3, 2.5, chat.MessageBedTooFar, chat.MessageJoin,
	chat.MessageBedTooFar.F(), chat.MessageJoin.F("Alex"),
}

// textLocales are the languages translations are checked in.
var textLocales = []language.Tag{language.English, language.BritishEnglish, language.Russian}

// filled returns the arguments of a translation of n parameters, each arg.
func filled(n int, arg any) []any {
	a := make([]any, n)
	for i := range a {
		a[i] = arg
	}
	return a
}

// oldSendTranslation is SendTranslation as it was: a translation sent as a
// translated Text whatever it resolves to.
func oldSendTranslation(s *Session, t chat.Translation, l language.Tag, a []any) {
	message, params, _ := bedrocktext.Message(t.F(a...).Text(l))
	s.writePacket(&mc.TextPacket{
		TextType:         mc.TextTypeTranslation,
		NeedsTranslation: true,
		Message:          message,
		Parameters:       params,
	})
}

// samePackets fails t if got and want do not encode to the same bytes.
func samePackets(t *testing.T, what string, got, want []proto.Packet) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d packets, the old code wrote %d", what, len(got), len(want))
	}
	for i := range got {
		if g, w := marshalPacketBytes(t, got[i]), marshalPacketBytes(t, want[i]); !bytes.Equal(g, w) {
			t.Fatalf("%s: packet %d is\n%#v\nthe old code wrote\n%#v", what, i, got[i], want[i])
		}
	}
}

// TestMessagetPackets holds the chat message that Player.Messaget sends, the
// text of the translation filled in for the language of the player, to the
// packet SendTranslation wrote for it, for every translation of the chat and
// cmd packages filled in with every kind of argument, in every language
// checked. A translation whose key resolves to no key but text of the
// server's own would be sent as a raw Text now; none of these does.
func TestMessagetPackets(t *testing.T) {
	cases := 0
	for name, rt := range repoTranslations {
		tr := rt.t
		for _, arg := range translationArgs {
			a := filled(rt.params, arg)
			for _, l := range textLocales {
				nw, old := newCaptureSession(), newCaptureSession()
				nw.SendMessage(tr.F(a...).Text(l))
				oldSendTranslation(old, tr, l, a)
				samePackets(t, name, nw.taken(), old.taken())
				cases++
			}
		}
	}
	t.Logf("%d cases", cases)
}

// oldSendCommandOutput is SendCommandOutput as it was, handed the
// cmd.Output and the language of the player.
func oldSendCommandOutput(s *Session, output *cmd.Output, l language.Tag) {
	messages := make([]mc.CommandOutputMessage, 0, output.MessageCount()+output.ErrorCount())
	for _, message := range output.Messages() {
		om := mc.CommandOutputMessage{Success: true, Message: message.String()}
		if t, ok := message.(chat.Texter); ok {
			om.Message, om.Parameters, _ = bedrocktext.Message(t.Text(l))
		}
		messages = append(messages, om)
	}
	for _, err := range output.Errors() {
		om := mc.CommandOutputMessage{Message: err.Error()}
		if t, ok := err.(chat.Texter); ok {
			om.Message, om.Parameters, _ = bedrocktext.Message(t.Text(l))
		}
		messages = append(messages, om)
	}
	s.writePacket(&mc.CommandOutputPacket{
		CommandOrigin:  s.commandOriginSnapshot(),
		OutputType:     mc.CommandOutputTypeAllOutput,
		SuccessCount:   uint32(output.MessageCount()),
		OutputMessages: messages,
	})
}

// checkCommandOutput fails t if the session writes, for the CommandOutput of
// o in a language checked, other bytes than it wrote for o, and does the
// same for o without its messages, as send_command_feedback sends it.
func checkCommandOutput(t *testing.T, what string, o *cmd.Output) {
	t.Helper()
	for _, out := range []*cmd.Output{o, o.WithoutMessages()} {
		for _, l := range textLocales {
			nw, old := newCaptureSession(), newCaptureSession()
			origin := mc.CommandOrigin{Origin: mc.CommandOriginPlayer, RequestID: "r", PlayerUniqueID: 7}
			nw.setCommandOrigin(origin)
			old.setCommandOrigin(origin)
			nw.SendCommandOutput(client.CommandOutputOf(out, l))
			oldSendCommandOutput(old, out, l)
			samePackets(t, what, nw.taken(), old.taken())
		}
	}
}

// commandOutputOf returns an output with messages and errors of every kind
// the cmd package adds, each with s in it.
func commandOutputOf(s string) *cmd.Output {
	o := &cmd.Output{}
	o.Print(s)
	o.Print(s, 3, s)
	o.Printf("%s: %d", s, 4)
	o.Printt(cmd.MessageUsage, s)
	o.Printt(chat.MessageBedTooFar)
	o.Printt(chat.MessageAdminCommand, s, text.Literal(s))
	o.Errorf("%s!", s)
	o.Errort(cmd.MessageUnknown, s)
	o.Errort(cmd.MessageNoTargets)
	o.Error(errors.New(s))
	o.Error(s, 5)
	return o
}

// TestCommandOutputPackets holds the CommandOutput the session writes to the
// one it wrote for the cmd.Output itself: for outputs of every kind of
// message with the strings of FuzzTextPackets, for every translation of the
// repository printed and as an error filled in with every kind of argument,
// and for an empty output.
func TestCommandOutputPackets(t *testing.T) {
	checkCommandOutput(t, "empty", &cmd.Output{})
	for _, s := range append(textPacketSeeds, legacyCorpus(t)...) {
		checkCommandOutput(t, s, commandOutputOf(s))
	}
	for name, rt := range repoTranslations {
		for _, arg := range translationArgs {
			o := &cmd.Output{}
			o.Printt(rt.t, filled(rt.params, arg)...)
			o.Errort(rt.t, filled(rt.params, arg)...)
			checkCommandOutput(t, name, o)
		}
	}
}

// FuzzCommandOutput holds the CommandOutput the session writes for an output
// of every kind of message with a string in it to the one it wrote for the
// cmd.Output itself.
func FuzzCommandOutput(f *testing.F) {
	for _, s := range append(textPacketSeeds, legacyCorpus(f)...) {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		checkCommandOutput(t, s, commandOutputOf(s))
	})
}
