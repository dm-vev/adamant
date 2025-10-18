package console

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"sort"
	"strings"

	prompt "github.com/c-bata/go-prompt"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	defaultPromptPrefix = "> "
	maxHistoryEntries   = 128
)

// Console provides a simple CLI backed command source that reads commands from
// an io.Reader (defaulting to os.Stdin) and executes them on the provided server.
type Console struct {
	srv     *server.Server
	log     *slog.Logger
	reader  io.Reader
	history []string
}

// New returns a Console bound to the provided server. The console reads from
// os.Stdin and writes command output to the supplied logger.
func New(srv *server.Server, log *slog.Logger) *Console {
	if log == nil {
		log = slog.Default()
	}
	return &Console{
		srv:    srv,
		log:    log,
		reader: os.Stdin,
	}
}

// WithReader sets a custom reader for the console input. It enables testing the
// console without relying on os.Stdin.
func (c *Console) WithReader(r io.Reader) *Console {
	if r != nil {
		c.reader = r
	}
	return c
}

// Run starts consuming commands from the console. It blocks until the context
// is cancelled or the underlying reader reaches EOF.
func (c *Console) Run(ctx context.Context) {
	if c.reader != os.Stdin {
		c.runScanner(ctx)
		return
	}
	c.runInteractive(ctx)
}

func (c *Console) runScanner(ctx context.Context) {
	scanner := bufio.NewScanner(c.reader)
	src := &consoleSource{log: c.log}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				c.log.Error("console input error", "err", err)
			}
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		c.execute(line, src)
	}
}

func (c *Console) runInteractive(ctx context.Context) {
	src := &consoleSource{log: c.log}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := prompt.Input(defaultPromptPrefix, func(doc prompt.Document) []prompt.Suggest {
			return c.complete(doc, src)
		},
			prompt.OptionTitle("Adamant Console"),
			prompt.OptionHistory(c.history),
			prompt.OptionPrefix(defaultPromptPrefix),
			prompt.OptionCompletionOnDown(),
			prompt.OptionMaxSuggestion(12),
		)

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		c.execute(line, src)
	}
}

func (c *Console) execute(line string, src *consoleSource) {
	input := strings.TrimSpace(line)
	if input == "" {
		return
	}
	if !strings.HasPrefix(input, "/") {
		input = "/" + input
	}

	c.history = append(c.history, input)
	if len(c.history) > maxHistoryEntries {
		c.history = c.history[len(c.history)-maxHistoryEntries:]
	}

	done := c.srv.World().Exec(func(tx *world.Tx) {
		cmd.ExecuteLine(src, input, tx, nil)
	})
	<-done
}

func (c *Console) complete(doc prompt.Document, src cmd.Source) []prompt.Suggest {
	textBefore := doc.TextBeforeCursor()
	word := strings.TrimPrefix(doc.GetWordBeforeCursor(), "/")
	segments := strings.Fields(textBefore)
	hasTrailingSpace := strings.HasSuffix(textBefore, " ")

	if len(segments) == 0 {
		return c.commandSuggestions(word)
	}

	commandToken := strings.TrimPrefix(segments[0], "/")
	if commandToken == "" && !hasTrailingSpace {
		return c.commandSuggestions(word)
	}

	paramIndex := len(segments) - 1
	if hasTrailingSpace {
		paramIndex = len(segments)
	}
	paramIndex-- // Exclude the command token.

	cmdAlias := strings.ToLower(commandToken)
	command, ok := cmd.ByAlias(cmdAlias)
	if !ok || paramIndex < 0 {
		return c.commandSuggestions(strings.TrimPrefix(doc.GetWordBeforeCursor(), "/"))
	}

	suggestions := c.parameterSuggestions(command, paramIndex, strings.TrimSpace(word), src)
	if len(suggestions) == 0 {
		usage := command.Usage()
		if usage == "" {
			return nil
		}
		return []prompt.Suggest{{
			Text:        strings.TrimSpace(doc.GetWordBeforeCursor()),
			Description: usage,
		}}
	}
	return suggestions
}

func (c *Console) commandSuggestions(prefix string) []prompt.Suggest {
	commands := cmd.Commands()
	suggestions := make([]prompt.Suggest, 0, len(commands))
	done := make(map[string]struct{}, len(commands))

	for alias, command := range commands {
		name := command.Name()
		if alias != name {
			continue
		}
		if _, ok := done[name]; ok {
			continue
		}
		done[name] = struct{}{}
		usage := command.Usage()
		if usage == "" {
			usage = "/" + name
		}
		suggestions = append(suggestions, prompt.Suggest{
			Text:        name,
			Description: usage,
		})
	}

	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Text < suggestions[j].Text
	})
	return prompt.FilterHasPrefix(suggestions, strings.TrimSpace(prefix), true)
}

func (c *Console) parameterSuggestions(command cmd.Command, index int, word string, src cmd.Source) []prompt.Suggest {
	params := command.Params(src)
	if index < 0 {
		return nil
	}

	seen := map[string]struct{}{}
	results := make([]prompt.Suggest, 0)
	fallback := make([]string, 0)

	for _, overload := range params {
		if index >= len(overload) {
			continue
		}
		param := overload[index]
		suggestions := c.suggestionsForParam(param, src)
		if len(suggestions) == 0 {
			fallback = append(fallback, paramHint(param))
			continue
		}
		for _, suggestion := range suggestions {
			key := strings.ToLower(suggestion.Text) + "|" + suggestion.Description
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			results = append(results, suggestion)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Text < results[j].Text
	})

	filtered := prompt.FilterHasPrefix(results, word, true)
	if len(filtered) == 0 && len(results) > 0 {
		filtered = prompt.FilterContains(results, word, true)
	}
	if len(filtered) > 0 {
		return filtered
	}

	hints := uniqueStrings(fallback)
	if len(hints) == 0 {
		return nil
	}
	return []prompt.Suggest{{
		Text:        word,
		Description: strings.Join(hints, " | "),
	}}
}

func (c *Console) suggestionsForParam(p cmd.ParamInfo, src cmd.Source) []prompt.Suggest {
	hint := paramHint(p)
	switch v := p.Value.(type) {
	case cmd.SubCommand:
		return []prompt.Suggest{{
			Text:        p.Name,
			Description: hint,
		}}
	case cmd.Enum:
		opts := append([]string(nil), v.Options(src)...)
		sort.Strings(opts)
		suggestions := make([]prompt.Suggest, 0, len(opts))
		for _, opt := range opts {
			suggestions = append(suggestions, prompt.Suggest{
				Text:        opt,
				Description: hint,
			})
		}
		return suggestions
	case bool:
		return []prompt.Suggest{
			{Text: "true", Description: hint},
			{Text: "false", Description: hint},
			{Text: "1", Description: hint},
			{Text: "0", Description: hint},
		}
	case []cmd.Target:
		names := c.playerNames()
		suggestions := make([]prompt.Suggest, 0, len(names))
		for _, name := range names {
			suggestions = append(suggestions, prompt.Suggest{
				Text:        name,
				Description: hint,
			})
		}
		return suggestions
	case cmd.Target:
		names := c.playerNames()
		suggestions := make([]prompt.Suggest, 0, len(names))
		for _, name := range names {
			suggestions = append(suggestions, prompt.Suggest{
				Text:        name,
				Description: hint,
			})
		}
		return suggestions
	}
	return nil
}

func (c *Console) playerNames() []string {
	names := make([]string, 0)
	for p := range c.srv.Players(nil) {
		names = append(names, p.Name())
	}
	slices.Sort(names)
	return names
}

func paramHint(p cmd.ParamInfo) string {
	t := paramTypeName(p.Value, p.Name)
	if p.Optional {
		return fmt.Sprintf("[%s: %s]%s", p.Name, t, p.Suffix)
	}
	return fmt.Sprintf("<%s: %s>%s", p.Name, t, p.Suffix)
}

func paramTypeName(v any, name string) string {
	switch v.(type) {
	case int, int8, int16, int32, int64:
		return "int"
	case uint, uint8, uint16, uint32, uint64:
		return "uint"
	case float32, float64:
		return "float"
	case string:
		return "string"
	case bool:
		return "bool"
	case cmd.Varargs:
		return "text"
	case []cmd.Target, cmd.Target:
		return "target"
	case mgl64.Vec3:
		return "x y z"
	case cmd.SubCommand:
		return name
	}
	if param, ok := v.(cmd.Parameter); ok {
		return param.Type()
	}
	if enum, ok := v.(cmd.Enum); ok {
		return enum.Type()
	}
	return "value"
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	sort.Strings(result)
	return result
}

type consoleSource struct {
	log *slog.Logger
}

func (c *consoleSource) Position() mgl64.Vec3 { return mgl64.Vec3{} }

func (c *consoleSource) Name() string { return "Console" }

func (c *consoleSource) SendCommandOutput(o *cmd.Output) {
	for _, msg := range o.Messages() {
		c.log.Info(msg.String())
	}
	for _, err := range o.Errors() {
		c.log.Error(err.Error())
	}
}
