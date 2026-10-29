package session

import (
	"time"

	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"golang.org/x/text/language"
)

// SendMessage ...
func (s *Session) SendMessage(message string) {
	if message == "" {
		// Bedrock refuses a Text without a message; a space shows the
		// empty line Java shows.
		message = " "
	}
	s.writePacket(&v2193.TextPacket{
		TextType: v2193.TextTypeRaw,
		Message:  message,
	})
}

// SendTranslation sends a translation localised for a specific language.Tag.
func (s *Session) SendTranslation(t chat.Translation, l language.Tag, a []any) {
	tr := t.F(a...)
	s.writePacket(&v2193.TextPacket{
		TextType:         v2193.TextTypeTranslation,
		NeedsTranslation: true,
		Message:          tr.Resolve(l),
		Parameters:       tr.Params(l),
	})
}

// SendTip ...
func (s *Session) SendTip(message string) {
	s.writePacket(&v2193.TextPacket{
		TextType: v2193.TextTypeTip,
		Message:  message,
	})
}

// SendAnnouncement ...
func (s *Session) SendAnnouncement(message string) {
	s.writePacket(&v2193.TextPacket{
		TextType: v2193.TextTypeAnnouncement,
		Message:  message,
	})
}

// SendPopup ...
func (s *Session) SendPopup(message string) {
	s.writePacket(&v2193.TextPacket{
		TextType: v2193.TextTypePopup,
		Message:  message,
	})
}

// SendJukeboxPopup ...
func (s *Session) SendJukeboxPopup(message string) {
	s.writePacket(&v2193.TextPacket{
		TextType: v2193.TextTypeJukeboxPopup,
		Message:  message,
	})
}

// SendToast ...
func (s *Session) SendToast(title, message string) {
	s.writePacket(&v2193.ToastRequestPacket{
		Title:   title,
		Message: message,
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
	name := sb.Name()
	lines := sb.Lines()

	if currentName != name {
		s.RemoveScoreboard()
		pk := &v2193.SetDisplayObjectivePacket{
			DisplaySlot:   "sidebar",
			ObjectiveName: name,
			DisplayName:   name,
			CriteriaName:  "dummy",
		}
		if sb.Descending() {
			pk.SortOrder = v2193.ScoreboardSortOrderDescending
		} else {
			pk.SortOrder = v2193.ScoreboardSortOrderAscending
		}
		s.writePacket(pk)
	} else {
		// Remove all current lines from the scoreboard. We can't replace them without removing them.
		pk := &v2193.SetScorePacket{}
		for i := range currentLines {
			pk.Entries = append(pk.Entries, v2193.ScoreboardEntry{
				IdentityType:  v2193.ScoreboardIdentityRemove,
				EntryID:       int64(i),
				ObjectiveName: currentName,
				Score:         int32(i),
			})
		}
		if len(pk.Entries) > 0 {
			s.writePacket(pk)
		}
	}
	pk := &v2193.SetScorePacket{}
	displayLines := make([]string, len(lines))
	for k, line := range lines {
		if len(line) == 0 {
			line = "§" + colours[k]
		}
		displayLines[k] = line
		pk.Entries = append(pk.Entries, v2193.ScoreboardEntry{
			EntryID:       int64(k),
			ObjectiveName: name,
			Score:         int32(k),
			IdentityType:  v2193.ScoreboardIdentityFakePlayer,
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
	s.writePacket(&v2193.RemoveObjectivePacket{ObjectiveName: *current})
	var name string
	var lines []string
	s.currentScoreboard.Store(&name)
	s.currentLines.Store(&lines)
}

// SendBossBar sends a boss bar using the default identifier.
func (s *Session) SendBossBar(text string, colour uint8, healthPercentage float64) {
	s.SendBossBarID("default", text, colour, healthPercentage)
}

// SendBossBarID shows or updates the independently identified boss bar.
func (s *Session) SendBossBarID(id, text string, colour uint8, healthPercentage float64) {
	s.writePacket(&v2193.BossEventPacket{
		BossEntityUniqueID: s.bossBarEntityID(id),
		EventType:          v2193.BossEventShow,
		BossBarTitle:       text,
		HealthPercentage:   float32(healthPercentage),
		Colour:             colour,
	})
}

// RemoveBossBar removes the default boss bar.
func (s *Session) RemoveBossBar() { s.RemoveBossBarID("default") }

// RemoveBossBarID hides only the boss bar identified by id.
func (s *Session) RemoveBossBarID(id string) {
	s.writePacket(&v2193.BossEventPacket{
		BossEntityUniqueID: s.bossBarEntityID(id),
		EventType:          v2193.BossEventHide,
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
	s.writePacket(&v2193.SetTitlePacket{
		ActionType:      v2193.TitleActionSetDurations,
		FadeInDuration:  int32(fadeInDuration / tickLength),
		RemainDuration:  int32(remainDuration / tickLength),
		FadeOutDuration: int32(fadeOutDuration / tickLength),
	})
}

// SendTitle ...
func (s *Session) SendTitle(text string) {
	s.writePacket(&v2193.SetTitlePacket{ActionType: v2193.TitleActionSetTitle, Text: text})
}

// SendSubtitle ...
func (s *Session) SendSubtitle(text string) {
	s.writePacket(&v2193.SetTitlePacket{ActionType: v2193.TitleActionSetSubtitle, Text: text})
}

// SendActionBarMessage ...
func (s *Session) SendActionBarMessage(text string) {
	s.writePacket(&v2193.SetTitlePacket{ActionType: v2193.TitleActionSetActionBar, Text: text})
}
