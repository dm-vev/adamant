package builtin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
)

func registerPlayerTextCommands(srv serverAdapter) {
	cmd.Register(newTextCommand(srv))
	cmd.Register(cmd.New("tellraw", "Sends a JSON message to players.", nil, tellRawCommand{}))
	cmd.Register(cmd.New("msg", "Sends a private message.", []string{"tell", "w"}, msgCommand{}))
}

type textCommand struct {
	opGate
	srv     serverAdapter
	Targets []cmd.Target `cmd:"targets"`
	Action  string       `cmd:"action"`
	Value   cmd.Varargs  `cmd:"value"`
}

func newTextCommand(srv serverAdapter) cmd.Command {
	return cmd.New("title", "Controls titles sent to players.", nil, textCommand{srv: srv})
}
func (c textCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	players := playersFromTargets(c.Targets)
	if len(players) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	raw := strings.TrimSpace(string(c.Value))
	switch c.Action {
	case "clear", "reset":
		for _, p := range players {
			p.SendTitle(title.New(""))
		}
	case "times":
		parts := strings.Fields(raw)
		if len(parts) != 3 {
			o.Errorf("Usage: title <targets> times <in> <stay> <out>")
			return
		}
		in, e1 := strconv.Atoi(parts[0])
		stay, e2 := strconv.Atoi(parts[1])
		out, e3 := strconv.Atoi(parts[2])
		if e1 != nil || e2 != nil || e3 != nil {
			o.Errorf("Invalid title durations")
			return
		}
		for _, p := range players {
			p.SendTitle(title.New("").WithFadeInDuration(time.Duration(in) * time.Millisecond).WithDuration(time.Duration(stay) * time.Millisecond).WithFadeOutDuration(time.Duration(out) * time.Millisecond))
		}
	case "title", "subtitle", "actionbar":
		text := componentText(raw)
		for _, p := range players {
			switch c.Action {
			case "title":
				p.SendTitle(title.New(text))
			case "subtitle":
				p.SendTitle(title.New("").WithSubtitle(text))
			case "actionbar":
				p.SendJukeboxPopup(text)
			}
		}
	default:
		o.Errorf("Unknown title action %q", c.Action)
	}
}

type tellRawCommand struct {
	opGate
	Targets []cmd.Target `cmd:"targets"`
	JSON    cmd.Varargs  `cmd:"message"`
}

func (c tellRawCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	ps := playersFromTargets(c.Targets)
	if len(ps) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	text := componentText(string(c.JSON))
	for _, p := range ps {
		p.Message(text)
	}
}

type msgCommand struct {
	Targets []cmd.Target `cmd:"targets"`
	Message cmd.Varargs  `cmd:"message"`
}

func (c msgCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	ps := playersFromTargets(c.Targets)
	if len(ps) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	from := sourceName(src)
	msg := strings.TrimSpace(string(c.Message))
	for _, p := range ps {
		p.Message(fmt.Sprintf("§d%s whispers to you: §f%s", from, msg))
	}
	o.Printf("§dYou whisper to %s: §f%s", joinNames(ps), msg)
}

func componentText(raw string) string {
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return raw
	}
	return renderComponent(v)
}
func renderComponent(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var b strings.Builder
		for _, e := range x {
			b.WriteString(renderComponent(e))
		}
		return b.String()
	case map[string]any:
		var b strings.Builder
		if t, ok := x["text"]; ok {
			b.WriteString(fmt.Sprint(t))
		}
		if tr, ok := x["translate"].(string); ok {
			b.WriteString(tr)
			if with, ok := x["with"].([]any); ok {
				for _, e := range with {
					b.WriteByte(' ')
					b.WriteString(renderComponent(e))
				}
			}
		}
		if sel, ok := x["selector"].(string); ok {
			b.WriteString(sel)
		}
		if sc, ok := x["score"].(map[string]any); ok {
			b.WriteString(fmt.Sprint(sc["value"]))
		}
		codes := make([]string, 0, 6)
		if color, ok := x["color"].(string); ok {
			if c := legacyColor(color); c != "" {
				codes = append(codes, c)
			}
		}
		for _, pair := range [][2]string{{"bold", "l"}, {"italic", "o"}, {"underlined", "n"}, {"strikethrough", "m"}, {"obfuscated", "k"}} {
			if on, _ := x[pair[0]].(bool); on {
				codes = append(codes, pair[1])
			}
		}
		if len(codes) > 0 {
			b.WriteString("§")
			b.WriteString(strings.Join(codes, "§"))
		}
		if extra, ok := x["extra"].([]any); ok {
			for _, e := range extra {
				b.WriteString(renderComponent(e))
			}
		}
		return b.String()
	default:
		return ""
	}
}
func legacyColor(c string) string {
	m := map[string]string{"black": "0", "dark_blue": "1", "dark_green": "2", "dark_aqua": "3", "dark_red": "4", "dark_purple": "5", "gold": "6", "gray": "7", "dark_gray": "8", "blue": "9", "green": "a", "aqua": "b", "red": "c", "light_purple": "d", "yellow": "e", "white": "f"}
	return m[c]
}
