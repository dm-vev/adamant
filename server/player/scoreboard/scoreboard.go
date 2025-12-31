package scoreboard

import (
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Scoreboard represents a scoreboard that may be sent to a player. The scoreboard is shown on the right side
// of the player's screen. It is safe for concurrent use.
// Scoreboard implements the io.Writer and io.StringWriter interfaces. fmt.Fprintf and fmt.Fprint may be used
// to write formatted text to the scoreboard.
type Scoreboard struct {
	mu         sync.RWMutex
	name       string
	lines      []string
	padding    bool
	descending bool
}

const maxScoreboardLines = 15

// New returns a new scoreboard with the display name passed. Once returned, lines may be added to the
// scoreboard to add text to it. The name is formatted according to the rules of fmt.Sprintln.
// Changing the scoreboard after sending it to a player will not update the scoreboard of the player
// automatically: Player.SendScoreboard() must be called again to update it.
func New(name ...any) *Scoreboard {
	return &Scoreboard{name: strings.TrimSuffix(fmt.Sprintln(name...), "\n"), padding: true}
}

// Name returns the display name of the scoreboard, as passed during the construction of the scoreboard.
func (board *Scoreboard) Name() string {
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
// the scoreboard.
func (board *Scoreboard) WriteString(s string) (n int, err error) {
	lines := strings.Split(s, "\n")

	// Scoreboards can have up to 15 lines. (16 including the title.)
	board.mu.Lock()
	defer board.mu.Unlock()
	if len(board.lines)+len(lines) > maxScoreboardLines {
		return 0, fmt.Errorf("write scoreboard: maximum of %d lines of text exceeded", maxScoreboardLines)
	}
	board.lines = append(board.lines, lines...)
	return len(s), nil
}

// Set changes a specific line in the scoreboard and adds empty lines until this index is reached. Set panics if the
// index passed is negative or 15+.
func (board *Scoreboard) Set(index int, s string) {
	if index < 0 || index >= maxScoreboardLines {
		// Ignore invalid indices to keep scoreboard updates safe for external callers.
		return
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	if diff := index - (len(board.lines) - 1); diff > 0 {
		board.lines = append(board.lines, make([]string, diff)...)
	}
	// Remove new lines from the string
	board.lines[index] = strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\n")
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

// Lines returns the data of the Scoreboard as a slice of strings.
func (board *Scoreboard) Lines() []string {
	board.mu.RLock()
	defer board.mu.RUnlock()
	lines := slices.Clone(board.lines)
	if board.padding {
		for i, line := range lines {
			if len(board.name)-len(line)-2 <= 0 {
				lines[i] = " " + line + " "
				continue
			}
			lines[i] = " " + line + strings.Repeat(" ", len(board.name)-len(line)-2)
		}
	}
	if board.descending {
		slices.Reverse(lines)
	}
	return lines
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
