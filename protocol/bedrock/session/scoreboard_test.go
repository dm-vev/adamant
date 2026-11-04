package session

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/player/scoreboard"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// oldScoreboard is scoreboard.Scoreboard as it was, holding strings: the
// oracle of FuzzScoreboard, with the session's SendScoreboard as it was.
type oldScoreboard struct {
	name       string
	lines      []string
	padding    bool
	descending bool
}

func newOldScoreboard(name ...any) *oldScoreboard {
	return &oldScoreboard{name: strings.TrimSuffix(fmt.Sprintln(name...), "\n"), padding: true}
}

func (board *oldScoreboard) WriteString(s string) (n int, err error) {
	lines := strings.Split(s, "\n")
	if len(board.lines)+len(lines) > 15 {
		return 0, fmt.Errorf("write scoreboard: maximum of %d lines of text exceeded", 15)
	}
	board.lines = append(board.lines, lines...)
	return len(s), nil
}

func (board *oldScoreboard) Set(index int, s string) {
	if index < 0 || index >= 15 {
		return
	}
	if diff := index - (len(board.lines) - 1); diff > 0 {
		board.lines = append(board.lines, make([]string, diff)...)
	}
	board.lines[index] = strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\n")
}

func (board *oldScoreboard) Remove(index int) {
	if index < 0 || index >= 15 || index >= len(board.lines) {
		return
	}
	board.lines = append(board.lines[:index], board.lines[index+1:]...)
}

func (board *oldScoreboard) Lines() []string {
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

// oldSendScoreboard is SendScoreboard as it was.
func oldSendScoreboard(s *Session, sb *oldScoreboard) {
	currentScoreboard := s.currentScoreboard.Load()
	currentLinesPtr := s.currentLines.Load()
	if currentScoreboard == nil || currentLinesPtr == nil {
		var name string
		var lines []string
		s.currentScoreboard.Store(&name)
		s.currentLines.Store(&lines)
		currentScoreboard = &name
		currentLinesPtr = &lines
	}
	currentName, currentLines := *currentScoreboard, *currentLinesPtr
	name := sb.name
	lines := sb.Lines()

	if currentName != name {
		s.RemoveScoreboard()
		pk := &mc.SetDisplayObjectivePacket{
			DisplaySlot:   "sidebar",
			ObjectiveName: name,
			DisplayName:   name,
			CriteriaName:  "dummy",
		}
		if sb.descending {
			pk.SortOrder = mc.ScoreboardSortOrderDescending
		} else {
			pk.SortOrder = mc.ScoreboardSortOrderAscending
		}
		s.writePacket(pk)
	} else {
		pk := &mc.SetScorePacket{}
		for i := range currentLines {
			pk.Entries = append(pk.Entries, mc.ScoreboardEntry{
				IdentityType:  mc.ScoreboardIdentityRemove,
				EntryID:       int64(i),
				ObjectiveName: currentName,
				Score:         int32(i),
			})
		}
		if len(pk.Entries) > 0 {
			s.writePacket(pk)
		}
	}
	pk := &mc.SetScorePacket{}
	displayLines := make([]string, len(lines))
	for k, line := range lines {
		if len(line) == 0 {
			line = "§" + colours[k]
		}
		displayLines[k] = line
		pk.Entries = append(pk.Entries, mc.ScoreboardEntry{
			EntryID:       int64(k),
			ObjectiveName: name,
			Score:         int32(k),
			IdentityType:  mc.ScoreboardIdentityFakePlayer,
			DisplayName:   line,
		})
	}
	if len(pk.Entries) > 0 {
		s.writePacket(pk)
	}
	s.currentScoreboard.Store(&name)
	s.currentLines.Store(&displayLines)
}

// boardSpec describes a scoreboard of FuzzScoreboard: its name, its lines,
// how they are added and how it is shown.
type boardSpec struct {
	name                  string
	lines                 []string
	write, remove         bool
	noPadding, descending bool
}

// boards returns the scoreboard of spec in the new code and in the old.
func (spec boardSpec) boards() (*scoreboard.Scoreboard, *oldScoreboard) {
	nw, old := scoreboard.New(spec.name), newOldScoreboard(spec.name)
	if spec.write {
		_, err1 := nw.WriteString(strings.Join(spec.lines, "\n"))
		_, err2 := old.WriteString(strings.Join(spec.lines, "\n"))
		if (err1 == nil) != (err2 == nil) {
			panic("WriteString fails in one of the codes only")
		}
	} else {
		for i, line := range spec.lines {
			nw.Set(i, line)
			old.Set(i, line)
		}
	}
	if spec.remove && len(spec.lines) > 1 {
		nw.Remove(1)
		old.Remove(1)
	}
	if spec.noPadding {
		nw.RemovePadding()
		old.padding = false
	}
	if spec.descending {
		nw.SetDescending()
		old.descending = true
	}
	return nw, old
}

// checkScoreboards fails t if the session writes other bytes for the
// scoreboards of specs, sent one after the other and then removed, than it
// wrote for them before.
func checkScoreboards(t *testing.T, specs ...boardSpec) {
	t.Helper()
	nw, old := newCaptureSession(), newCaptureSession()
	for _, spec := range specs {
		nb, ob := spec.boards()
		nw.SendScoreboard(nb)
		oldSendScoreboard(old, ob)
	}
	nw.RemoveScoreboard()
	old.RemoveScoreboard()
	samePackets(t, fmt.Sprintf("%+v", specs), nw.taken(), old.taken())
}

// specOf returns the scoreboard that FuzzScoreboard makes of a name, lines
// split at newlines (at most 15; with write, those after it are dropped by
// WriteString) and flags.
func specOf(name, lines string, flags uint8) boardSpec {
	spec := boardSpec{name: name, write: flags&1 != 0, remove: flags&2 != 0, noPadding: flags&4 != 0, descending: flags&8 != 0}
	if lines != "" {
		spec.lines = strings.Split(lines, "\n")
	}
	if len(spec.lines) > 15 {
		spec.lines = spec.lines[:15]
	}
	return spec
}

// checkScoreboardInput checks a board of name and lines, and a second board
// sent after it with the same name and with name2.
func checkScoreboardInput(t *testing.T, name, lines, name2 string, flags uint8) {
	t.Helper()
	first := specOf(name, lines, flags)
	second := specOf(name, lines+"\nmore", flags>>4)
	renamed := specOf(name2, lines, flags>>4)
	checkScoreboards(t, first)
	checkScoreboards(t, first, second)
	checkScoreboards(t, first, renamed, first)
}

// scoreboardSeeds are inputs of FuzzScoreboard.
var scoreboardSeeds = []struct {
	name, lines, name2 string
	flags              uint8
}{
	{"Board", "", "Other", 0},
	{"§l§6Server Name Long", "§aOnline: 3\n\n§bCoins: §e100\n§7www.example.org", "§cOther", 0},
	{"§l§6Server Name Long", "§aOnline: 3\n\n§bCoins: §e100\n§7www.example.org", "§cOther", 0xff},
	{"A", "a very long line that is wider than the name\nb", "A", 4},
	{"Привет мир", "строка\n§kсекрет\n", "Привет", 8},
	{"", "\n\n\n", "", 1},
	{"Board", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16", "Board", 0x13},
	{"§r", "§r\n§\n§z", "§r§r", 2},
	{"Name", "line\n", "Name", 0x91},
}

// TestScoreboardPackets checks the seeds of FuzzScoreboard.
func TestScoreboardPackets(t *testing.T) {
	for _, s := range scoreboardSeeds {
		checkScoreboardInput(t, s.name, s.lines, s.name2, s.flags)
	}
}

// FuzzScoreboard holds the packets the session writes for scoreboards, now
// that they hold text and the session pads and orders their lines, to those
// it wrote for the scoreboards of strings that padded and ordered them
// themselves: scoreboards of any name and up to 15 lines, set or written,
// with a line removed or not, padded or not, descending or not, sent after a
// scoreboard of the same name and after one of another name, then removed.
func FuzzScoreboard(f *testing.F) {
	for _, s := range scoreboardSeeds {
		f.Add(s.name, s.lines, s.name2, s.flags)
	}
	f.Fuzz(func(t *testing.T, name, lines, name2 string, flags uint8) {
		checkScoreboardInput(t, name, lines, name2, flags)
	})
}
