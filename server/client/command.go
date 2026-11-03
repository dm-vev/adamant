package client

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	"golang.org/x/text/language"
)

// CommandOutput is the output of a command, as its client is shown it.
type CommandOutput struct {
	// Messages are the messages of the command, then its errors, in the
	// order they were added.
	Messages []CommandMessage
}

// CommandMessage is a message of a command's output.
type CommandMessage struct {
	// Text is the text of the message.
	Text text.Text
	// Success is set for a message of success and unset for an error.
	Success bool
}

// CommandOutputOf returns the output o for a client of the language l: the
// messages of o, then its errors. A message or error that is a chat.Texter,
// such as a translation filled in by chat.Translation.F, is its text for l;
// any other is its string read with text.ParseLegacy.
func CommandOutputOf(o *cmd.Output, l language.Tag) CommandOutput {
	messages := make([]CommandMessage, 0, o.MessageCount()+o.ErrorCount())
	for _, m := range o.Messages() {
		var t text.Text
		if tx, ok := m.(chat.Texter); ok {
			t = tx.Text(l)
		} else {
			t = text.ParseLegacy(m.String())
		}
		messages = append(messages, CommandMessage{Text: t, Success: true})
	}
	for _, err := range o.Errors() {
		var t text.Text
		if tx, ok := err.(chat.Texter); ok {
			t = tx.Text(l)
		} else {
			t = text.ParseLegacy(err.Error())
		}
		messages = append(messages, CommandMessage{Text: t})
	}
	return CommandOutput{Messages: messages}
}
