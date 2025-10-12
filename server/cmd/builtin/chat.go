package builtin

import (
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
)

type sayCommand struct {
	Message cmd.Varargs `cmd:"message"`
}

func (c sayCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	msg := strings.TrimSpace(string(c.Message))
	if msg == "" {
		o.Errort(cmd.MessageUsage, "/say <message>")
		return
	}
	line := "[" + sourceName(src) + "] " + msg
	_, _ = chat.Global.WriteString(line + "\n")
	o.Print(line)
}

type meCommand struct {
	Action cmd.Varargs `cmd:"action"`
}

func newSayCommand() cmd.Command {
	return cmd.New("say", "Broadcasts a message to all players.", nil, sayCommand{})
}

func newMeCommand() cmd.Command {
	return cmd.New("me", "Performs an action in chat.", nil, meCommand{})
}

func (c meCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	action := strings.TrimSpace(string(c.Action))
	if action == "" {
		o.Errort(cmd.MessageUsage, "/me <action>")
		return
	}
	line := "* " + sourceName(src) + " " + action
	_, _ = chat.Global.WriteString(line + "\n")
	o.Print(line)
}
