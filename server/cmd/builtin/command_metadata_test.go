package builtin

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

func TestBuiltinCommandRegistryKeepsNativeHelpLocal(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_commands.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		CommandNames []string `json:"command_names"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if len(reference.CommandNames) != 82 || slices.Contains(reference.CommandNames, "help") || slices.Contains(reference.CommandNames, "?") {
		t.Fatal("native registry fixture must omit the client's local help command")
	}
	Register(registerTestAdapter{})
	pk, runnable := session.BuildAvailableCommands(cmd.Commands(), nil, make(map[string]struct{}))
	decoded := decodeCommands(t, encodePacket(pk))
	commands := make(map[string]v2193.Command)
	for _, command := range decoded.Commands {
		commands[command.Name] = command
	}
	for _, name := range []string{"help", "?"} {
		if _, exists := commands[name]; exists {
			t.Errorf("client-local %q appeared in the remote command grammar", name)
		}
		if _, exists := runnable[name]; exists {
			t.Errorf("client-local %q appeared in the network refresh cache", name)
		}
	}
	for _, enum := range decoded.Enums {
		if enum.Type == "helpAliases" {
			t.Error("packet retained aliases for the client-local help command")
		}
	}
	for _, name := range []string{"gamemode", "experience", "execute", "fill", "damage", "gamerule", "difficulty", "banlist", "ban", "spreadplayers", "list", "whitelist", "pardon", "particle"} {
		command, exists := commands[name]
		if !exists || len(command.Overloads) == 0 {
			t.Fatalf("command %s lost its overloads", name)
		}
	}
	mode := commands["gamemode"].Overloads[0].Parameters[0]
	if mode.Type&(v2193.CommandArgValid|v2193.CommandArgEnum) != v2193.CommandArgValid|v2193.CommandArgEnum {
		t.Fatalf("gamemode argument is not a hard enum: %#x", mode.Type)
	}
	var choices []string
	for _, index := range decoded.Enums[mode.Type&0xfffff].ValueIndices {
		choices = append(choices, decoded.EnumValues[index])
	}
	if !slices.Equal(choices, []string{"survival", "creative", "adventure", "spectator"}) {
		t.Fatalf("gamemode choices = %v", choices)
	}
	// Server/console help and its alias still execute normally.
	for _, alias := range []string{"help", "?"} {
		help, exists := cmd.ByAlias(alias)
		if !exists {
			t.Fatalf("server command %s was removed", alias)
		}
		source := &metadataHelpSource{}
		help.Execute("gamemode", source, nil)
		found := false
		if source.output != nil {
			for _, message := range source.output.Messages() {
				found = found || strings.Contains(message.String(), "gamemode")
			}
		}
		if !found {
			t.Errorf("server %s lost command usage output", alias)
		}
	}
}

type metadataHelpSource struct {
	cmd.Source
	output *cmd.Output
}

func (s *metadataHelpSource) SendCommandOutput(output *cmd.Output) { s.output = output }

// encodePacket encodes a packet of the generated codecs.
// decodeCommands decodes AvailableCommands as a client reads it: all of
// it, with nothing left over.
func decodeCommands(t *testing.T, b []byte) v2193.AvailableCommandsPacket {
	t.Helper()
	var pk v2193.AvailableCommandsPacket
	r := wire.NewReader(b)
	pk.Decode(r)
	if r.Err() != nil || r.Len() != 0 {
		t.Fatalf("AvailableCommands does not decode: %v, %d bytes left", r.Err(), r.Len())
	}
	return pk
}

func encodePacket(pk interface{ Encode(*wire.Writer) }) []byte {
	var w wire.Writer
	pk.Encode(&w)
	return w.B
}
