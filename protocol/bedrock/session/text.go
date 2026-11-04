package session

import (
	"slices"
	"strings"
	"time"

	bedrocktext "github.com/df-mc/dragonfly/protocol/bedrock/text"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/text"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// SendMessage shows t in the chat. A text that holds a translation is sent
// as a translated Text, which the client translates into its language, with
// the message and parameters of bedrocktext.Message; any other is sent as it
// is, as a raw one.
func (s *Session) SendMessage(t text.Text) {
	message, params, translated := bedrocktext.Message(t)
	if translated {
		s.writePacket(&mc.TextPacket{
			TextType:         mc.TextTypeTranslation,
			NeedsTranslation: true,
			Message:          message,
			Parameters:       params,
		})
		return
	}
	if message == "" {
		// Bedrock refuses a Text without a message; a space shows the
		// empty line Java shows.
		message = " "
	}
	s.writePacket(&mc.TextPacket{
		TextType: mc.TextTypeRaw,
		Message:  message,
	})
}

// SendTip ...
func (s *Session) SendTip(t text.Text) {
	s.writePacket(&mc.TextPacket{
		TextType: mc.TextTypeTip,
		Message:  bedrocktext.String(t),
	})
}

// SendAnnouncement ...
func (s *Session) SendAnnouncement(message string) {
	s.writePacket(&mc.TextPacket{
		TextType: mc.TextTypeAnnouncement,
		Message:  message,
	})
}

// SendPopup ...
func (s *Session) SendPopup(t text.Text) {
	s.writePacket(&mc.TextPacket{
		TextType: mc.TextTypePopup,
		Message:  bedrocktext.String(t),
	})
}

// SendJukeboxPopup ...
func (s *Session) SendJukeboxPopup(t text.Text) {
	s.writePacket(&mc.TextPacket{
		TextType: mc.TextTypeJukeboxPopup,
		Message:  bedrocktext.String(t),
	})
}

// SendToast ...
func (s *Session) SendToast(title, message text.Text) {
	s.writePacket(&mc.ToastRequestPacket{
		Title:   bedrocktext.String(title),
		Message: bedrocktext.String(message),
	})
}

// SendScoreboard ...
func (s *Session) SendScoreboard(sb *scoreboard.Scoreboard) {
	if s == Nop || sb == nil {
		return
	}
	currentScoreboard := s.currentScoreboard.Load()
	currentLinesPtr := s.currentLines.Load()
	if currentScoreboard == nil || currentLinesPtr == nil {
		// Ensure scoreboard state is initialized to avoid nil dereferences if the session was not built via New().
		var name string
		var lines []string
		s.currentScoreboard.Store(&name)
		s.currentLines.Store(&lines)
		currentScoreboard = &name
		currentLinesPtr = &lines
	}
	currentName, currentLines := *currentScoreboard, *currentLinesPtr
	name := bedrocktext.String(sb.Name())
	lines := scoreboardLines(sb.Lines(), name, sb.Padding(), sb.Descending())

	if currentName != name {
		s.RemoveScoreboard()
		pk := &mc.SetDisplayObjectivePacket{
			DisplaySlot:   "sidebar",
			ObjectiveName: name,
			DisplayName:   name,
			CriteriaName:  "dummy",
		}
		if sb.Descending() {
			pk.SortOrder = mc.ScoreboardSortOrderDescending
		} else {
			pk.SortOrder = mc.ScoreboardSortOrderAscending
		}
		s.writePacket(pk)
	} else {
		// Remove all current lines from the scoreboard. We can't replace them without removing them.
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
	// Store the last sent state so subsequent updates remove the right entries.
	s.currentScoreboard.Store(&name)
	s.currentLines.Store(&displayLines)
}

// scoreboardLines renders the lines of a scoreboard of the name passed, as rendered, in the order the
// client shows them: padded with a space at their start, and at their end up to the width of the name, in
// bytes, if padding is set, and reversed if the scoreboard is descending.
func scoreboardLines(lines []text.Text, name string, padding, descending bool) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = bedrocktext.String(line)
	}
	if padding {
		for i, line := range out {
			if len(name)-len(line)-2 <= 0 {
				out[i] = " " + line + " "
				continue
			}
			out[i] = " " + line + strings.Repeat(" ", len(name)-len(line)-2)
		}
	}
	if descending {
		slices.Reverse(out)
	}
	return out
}

// colours holds a list of colour codes to be filled out for empty lines in a scoreboard.
var colours = [15]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "a", "b", "c", "d", "e", "f"}

// RemoveScoreboard ...
func (s *Session) RemoveScoreboard() {
	if s == Nop {
		return
	}
	current := s.currentScoreboard.Load()
	if current == nil {
		return
	}
	s.writePacket(&mc.RemoveObjectivePacket{ObjectiveName: *current})
	var name string
	var lines []string
	s.currentScoreboard.Store(&name)
	s.currentLines.Store(&lines)
}

// SendBossBar sends a boss bar using the default identifier.
func (s *Session) SendBossBar(title text.Text, colour bossbar.Colour, healthPercentage float64) {
	s.SendBossBarID("default", title, colour, healthPercentage)
}

// SendBossBarID shows or updates the independently identified boss bar: title above a bar of the colour
// passed, filled to healthPercentage.
func (s *Session) SendBossBarID(id string, title text.Text, colour bossbar.Colour, healthPercentage float64) {
	s.writePacket(&mc.BossEventPacket{
		BossEntityUniqueID: s.bossBarEntityID(id),
		EventType:          mc.BossEventShow,
		BossBarTitle:       bedrocktext.String(title),
		HealthPercentage:   float32(healthPercentage),
		Colour:             bedrockBossBarColour(colour),
	})
}

// RemoveBossBar removes the default boss bar.
func (s *Session) RemoveBossBar() { s.RemoveBossBarID("default") }

// RemoveBossBarID hides only the boss bar identified by id.
func (s *Session) RemoveBossBarID(id string) {
	s.writePacket(&mc.BossEventPacket{
		BossEntityUniqueID: s.bossBarEntityID(id),
		EventType:          mc.BossEventHide,
	})
}

func (s *Session) bossBarEntityID(id string) int64 {
	if id == "default" {
		return selfEntityRuntimeID
	}
	s.bossBarMu.Lock()
	defer s.bossBarMu.Unlock()
	if entityID, ok := s.bossBarIDs[id]; ok {
		return entityID
	}
	if s.bossBarIDs == nil {
		s.bossBarIDs = make(map[string]int64)
		s.nextBossBarID = -(int64(1) << 62)
	}
	entityID := s.nextBossBarID
	s.nextBossBarID++
	s.bossBarIDs[id] = entityID
	return entityID
}

const tickLength = time.Second / 20

// SetTitleDurations ...
func (s *Session) SetTitleDurations(fadeInDuration, remainDuration, fadeOutDuration time.Duration) {
	s.writePacket(&mc.SetTitlePacket{
		ActionType:      mc.TitleActionSetDurations,
		FadeInDuration:  int32(fadeInDuration / tickLength),
		RemainDuration:  int32(remainDuration / tickLength),
		FadeOutDuration: int32(fadeOutDuration / tickLength),
	})
}

// SendTitle ...
func (s *Session) SendTitle(t text.Text) {
	s.writePacket(&mc.SetTitlePacket{ActionType: mc.TitleActionSetTitle, Text: bedrocktext.String(t)})
}

// SendSubtitle ...
func (s *Session) SendSubtitle(t text.Text) {
	s.writePacket(&mc.SetTitlePacket{ActionType: mc.TitleActionSetSubtitle, Text: bedrocktext.String(t)})
}

// SendActionBarMessage ...
func (s *Session) SendActionBarMessage(t text.Text) {
	s.writePacket(&mc.SetTitlePacket{ActionType: mc.TitleActionSetActionBar, Text: bedrocktext.String(t)})
}
