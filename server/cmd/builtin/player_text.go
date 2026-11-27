package builtin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/player/title"
	"github.com/dm-vev/adamant/server/world"
)

func registerPlayerTextCommands(srv serverAdapter) {
	cmd.Register(newTextCommand(srv))
	cmd.Register(newCommand("tellraw", nil, tellRawCommand{}))
	cmd.Register(newCommand("msg", []string{"tell", "w"}, msgCommand{}))
}

type textCommand struct {
	gameMaster
	srv     serverAdapter
	Targets cmd.Players `cmd:"targets"`
	Action  string      `cmd:"action"`
	Value   cmd.Varargs `cmd:"value"`
}

func newTextCommand(srv serverAdapter) cmd.Command {
	return newCommand("title", nil, textCommand{srv: srv})
}
func (c textCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	raw := strings.TrimSpace(string(c.Value))
	var apply func(*player.Player)
	switch c.Action {
	case "clear", "reset":
		apply = func(p *player.Player) { p.SendTitle(title.New("")) }
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
		t := title.New("").
			WithFadeInDuration(time.Duration(in) * time.Millisecond).
			WithDuration(time.Duration(stay) * time.Millisecond).
			WithFadeOutDuration(time.Duration(out) * time.Millisecond)
		apply = func(p *player.Player) { p.SendTitle(t) }
	case "title":
		t := title.New(componentText(raw))
		apply = func(p *player.Player) { p.SendTitle(t) }
	case "subtitle":
		t := title.New("").WithSubtitle(componentText(raw))
		apply = func(p *player.Player) { p.SendTitle(t) }
	case "actionbar":
		text := componentText(raw)
		apply = func(p *player.Player) { p.SendJukeboxPopup(text) }
	default:
		o.Errorf("Unknown title action %q", c.Action)
		return
	}
	cmd.ForEach(src, o, tx, c.Targets,
		func(_ *world.Tx, t cmd.Target) int {
			p, ok := t.(*player.Player)
			if !ok {
				return 0
			}
			apply(p)
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(tally.Total)
		},
	)
}

type tellRawCommand struct {
	gameMaster
	Targets cmd.Players `cmd:"targets"`
	JSON    cmd.Varargs `cmd:"message"`
}

func (c tellRawCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	text := componentText(string(c.JSON))
	cmd.ForEach(src, o, tx, c.Targets,
		func(_ *world.Tx, t cmd.Target) int {
			p, ok := t.(*player.Player)
			if !ok {
				return 0
			}
			p.Message(text)
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(tally.Total)
		},
	)
}

type msgCommand struct {
	Targets cmd.Players `cmd:"targets"`
	Message cmd.Varargs `cmd:"message"`
}

func (c msgCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	from := sourceName(src)
	msg := strings.TrimSpace(string(c.Message))
	whisper := fmt.Sprintf("§d%s whispers to you: §f%s", from, msg)
	cmd.ForEach(src, o, tx, c.Targets,
		func(_ *world.Tx, t cmd.Target) int {
			p, ok := t.(*player.Player)
			if !ok {
				return 0
			}
			p.Message(whisper)
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("§dYou whisper to %s: §f%s", joinNonZeroNames(rs), msg)
		},
	)
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
		// Bedrock wraps its components in rawtext.
		if raw, ok := x["rawtext"]; ok {
			b.WriteString(renderComponent(raw))
		}
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
