package session

import (
	"slices"
	"strings"
	"time"

	bedrocktext "github.com/dm-vev/adamant/protocol/bedrock/text"
	"github.com/dm-vev/adamant/server/player/bossbar"
	"github.com/dm-vev/adamant/server/player/scoreboard"
	"github.com/dm-vev/adamant/server/text"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
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

var (
	emptyScoreboardName      string
	emptyScoreboardLineSlice []string
	// colours holds a list of colour codes to be filled out for empty lines in a scoreboard.
	colours = [15]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "a", "b", "c", "d", "e", "f"}
	// emptyScoreboardLines holds the colour-code placeholders for empty lines in a scoreboard.
	emptyScoreboardLines = [15]string{"§1", "§2", "§3", "§4", "§5", "§6", "§7", "§8", "§9", "§a", "§b", "§c", "§d", "§e", "§f"}
)

const scoreboardSpaces = "                                                                "

// SendScoreboard ...
func (s *Session) SendScoreboard(sb *scoreboard.Scoreboard) {
	if s == nop || sb == nil {
		return
	}
	currentScoreboard := s.currentScoreboard.Load()
	currentLinesPtr := s.currentLines.Load()
	if currentScoreboard == nil || currentLinesPtr == nil {
		// Ensure scoreboard state is initialized to avoid nil dereferences if the session was not built via New().
		s.currentScoreboard.Store(&emptyScoreboardName)
		s.currentLines.Store(&emptyScoreboardLineSlice)
		currentScoreboard = &emptyScoreboardName
		currentLinesPtr = &emptyScoreboardLineSlice
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
		nameCopy := name
		s.currentScoreboard.Store(&nameCopy)
	} else if len(currentLines) > 0 {
		// Remove all current lines from the scoreboard. We can't replace them without removing them.
		entries := make([]mc.ScoreboardEntry, len(currentLines))
		for i := range entries {
			entries[i] = mc.ScoreboardEntry{
				IdentityType:  mc.ScoreboardIdentityRemove,
				EntryID:       int64(i),
				ObjectiveName: currentName,
				Score:         int32(i),
			}
		}
		s.writePacket(&mc.SetScorePacket{Entries: entries})
	}
	if len(lines) > 0 {
		entries := make([]mc.ScoreboardEntry, len(lines))
		for k, line := range lines {
			if len(line) == 0 {
				line = emptyScoreboardLines[k]
				lines[k] = line
			}
			entries[k] = mc.ScoreboardEntry{
				EntryID:       int64(k),
				ObjectiveName: name,
				Score:         int32(k),
				IdentityType:  mc.ScoreboardIdentityFakePlayer,
				DisplayName:   line,
			}
		}
		s.writePacket(&mc.SetScorePacket{Entries: entries})
		s.currentLines.Store(&lines)
	} else {
		s.currentLines.Store(&emptyScoreboardLineSlice)
	}
}

// scoreboardLines renders the lines of a scoreboard of the name passed, as rendered, in the order the
// client shows them: padded with a space at their start, and at their end up to the width of the name, in
// bytes, if padding is set, and reversed if the scoreboard is descending.
func scoreboardLines(lines []text.Text, name string, padding, descending bool) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = bedrocktext.String(line)
	}
	if padding {
		for i, line := range out {
			pad := len(name) - len(line) - 2
			switch {
			case pad <= 0:
				out[i] = " " + line + " "
			case pad <= len(scoreboardSpaces):
				out[i] = " " + line + scoreboardSpaces[:pad]
			default:
				out[i] = " " + line + strings.Repeat(" ", pad)
			}
		}
	}
	if descending {
		slices.Reverse(out)
	}
	return out
}

// RemoveScoreboard ...
func (s *Session) RemoveScoreboard() {
	if s == nop {
		return
	}
	current := s.currentScoreboard.Load()
	if current == nil {
		return
	}
	s.writePacket(&mc.RemoveObjectivePacket{ObjectiveName: *current})
	s.currentScoreboard.Store(&emptyScoreboardName)
	s.currentLines.Store(&emptyScoreboardLineSlice)
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
