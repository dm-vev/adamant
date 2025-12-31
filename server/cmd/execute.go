package cmd

import (
	"encoding/csv"
	"strings"
	"unicode"

	"github.com/df-mc/dragonfly/server/world"
)

// ExecuteLine executes a command line on behalf of the Source passed. The commandLine
// is expected to include the leading slash. If the command cannot be found, an
// appropriate error is sent back to the Source. The optional before function may
// be supplied to intercept execution; returning false from it will stop execution.
func ExecuteLine(source Source, commandLine string, tx *world.Tx, before func(Command, []string) bool) {
	if source == nil {
		panic("cmd.ExecuteLine: source must not be nil")
	}
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		return
	}
	if !strings.HasPrefix(commandLine, "/") {
		return
	}
	trimmed := strings.TrimLeftFunc(commandLine[1:], unicode.IsSpace)
	if trimmed == "" {
		return
	}

	// Split once so quoted arguments keep their spaces for csv parsing downstream.
	splitAt := strings.IndexFunc(trimmed, unicode.IsSpace)
	name := trimmed
	args := ""
	if splitAt != -1 {
		name = trimmed[:splitAt]
		args = strings.TrimLeftFunc(trimmed[splitAt+1:], unicode.IsSpace)
	}
	if name == "" {
		return
	}

	command, ok := ByAlias(name)
	if !ok {
		output := &Output{}
		output.Errort(MessageUnknown, name)
		source.SendCommandOutput(output)
		return
	}
	var parsedArgs []string
	if before != nil && args != "" {
		reader := csv.NewReader(strings.NewReader(args))
		reader.Comma, reader.LazyQuotes = ' ', true
		record, err := reader.Read()
		if err != nil {
			// Fall back to whitespace splitting if the CSV parser fails unexpectedly.
			parsedArgs = strings.Fields(args)
		} else {
			parsedArgs = record
		}
	}
	if before != nil && !before(command, parsedArgs) {
		return
	}
	command.Execute(args, source, tx)
}
