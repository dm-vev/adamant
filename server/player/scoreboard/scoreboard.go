package scoreboard

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/text"
)

// Scoreboard represents a scoreboard that may be sent to a player. The scoreboard is shown on the right side
// of the player's screen. It is safe for concurrent use.
// Scoreboard implements the io.Writer and io.StringWriter interfaces. fmt.Fprintf and fmt.Fprint may be used
// to write formatted text to the scoreboard.
type Scoreboard struct {
	mu         sync.RWMutex
	name       text.Text
	lines      []text.Text
	padding    bool
	descending bool
}

const maxScoreboardLines = 15

// New returns a new scoreboard with the display name passed. Once returned, lines may be added to the
// scoreboard to add text to it. The name is formatted according to the rules of fmt.Sprintln and read as a
// legacy string, with § codes; a lone text.Text is taken as it is.
// Changing the scoreboard after sending it to a player will not update the scoreboard of the player
// automatically: Player.SendScoreboard() must be called again to update it.
func New(name ...any) *Scoreboard {
	if len(name) == 1 {
		if t, ok := name[0].(text.Text); ok {
			return &Scoreboard{name: t, padding: true}
		}
	}
	return &Scoreboard{name: text.ParseLegacy(strings.TrimSuffix(fmt.Sprintln(name...), "\n")), padding: true}
}

// Name returns the display name of the scoreboard, as passed during the construction of the scoreboard.
func (board *Scoreboard) Name() text.Text {
	board.mu.RLock()
	defer board.mu.RUnlock()
	return board.name
}

// Write writes a slice of data as text to the scoreboard. Newlines may be written to create a new line on
// the scoreboard.
func (board *Scoreboard) Write(p []byte) (n int, err error) {
	if _, err := board.WriteString(string(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

// WriteString writes a string of text to the scoreboard. Newlines may be written to create a new line on
// the scoreboard. Each line is read as a legacy string, with § codes.
func (board *Scoreboard) WriteString(s string) (n int, err error) {
	lines := strings.Split(s, "\n")

	// Scoreboards can have up to 15 lines. (16 including the title.)
	board.mu.Lock()
	defer board.mu.Unlock()
	if len(board.lines)+len(lines) > maxScoreboardLines {
		return 0, fmt.Errorf("write scoreboard: maximum of %d lines of text exceeded", maxScoreboardLines)
	}
	for _, line := range lines {
		board.lines = append(board.lines, text.ParseLegacy(line))
	}
	return len(s), nil
}

// Set changes a specific line in the scoreboard and adds empty lines until this index is reached. Set ignores an
// index that is negative or 15+. The line is a text.Text, taken as it is, or a string or another value, formatted
// by fmt, without up to two newlines at its end, and read as a legacy string, with § codes.
func (board *Scoreboard) Set(index int, line any) {
	if index < 0 || index >= maxScoreboardLines {
		// Ignore invalid indices to keep scoreboard updates safe for external callers.
		return
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	if diff := index - (len(board.lines) - 1); diff > 0 {
		board.lines = append(board.lines, make([]text.Text, diff)...)
	}
	board.lines[index] = lineText(line)
}

// lineText returns a line passed to Set as text.
func lineText(line any) text.Text {
	if t, ok := line.(text.Text); ok {
		return t
	}
	s, ok := line.(string)
	if !ok {
		s = fmt.Sprint(line)
	}
	// Remove new lines from the string
	return text.ParseLegacy(strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\n"))
}

// Remove removes a specific line from the scoreboard. Remove panics if the index passed is negative or 15+.
func (board *Scoreboard) Remove(index int) {
	if index < 0 || index >= maxScoreboardLines {
		// Ignore invalid indices to keep scoreboard updates safe for external callers.
		return
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	if index >= len(board.lines) {
		// No line exists at this index yet, so there is nothing to remove.
		return
	}
	board.lines = append(board.lines[:index], board.lines[index+1:]...)
}

// RemovePadding removes the padding of one space that is added to the start of every line.
func (board *Scoreboard) RemovePadding() {
	board.mu.Lock()
	defer board.mu.Unlock()
	board.padding = false
}

// Padding reports whether the lines are padded when the scoreboard is shown: with a space at their start,
// and at their end up to the width of the name. It is true until RemovePadding is called.
func (board *Scoreboard) Padding() bool {
	board.mu.RLock()
	defer board.mu.RUnlock()
	return board.padding
}

// Lines returns the lines of the Scoreboard as they were set, from the top: without the padding and in the
// order they are shown in whether the scoreboard is descending or not. A line never set is the zero text. The
// client of a player pads and orders the lines as it shows them.
func (board *Scoreboard) Lines() []text.Text {
	board.mu.RLock()
	defer board.mu.RUnlock()
	return slices.Clone(board.lines)
}

// Descending returns whether the scoreboard is sorted in descending order.
func (board *Scoreboard) Descending() bool {
	board.mu.RLock()
	defer board.mu.RUnlock()
	return board.descending
}

// SetDescending sets the scoreboard sort order to descending.
// When sending the scoreboard to the player, it will reverse the order of the lines to match when it's ascending.
func (board *Scoreboard) SetDescending() {
	board.mu.Lock()
	defer board.mu.Unlock()
	board.descending = true
}
