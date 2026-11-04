package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/text"
)

// BenchmarkSessionSendTip measures what a tip costs the session beyond its
// packet: the string a player is given read as text and rendered back, for a
// plain string and for a status line of seven § codes. Nop drops the packet.
func BenchmarkSessionSendTip(b *testing.B) {
	for name, s := range map[string]string{
		"plain": "Welcome to the server",
		"coded": "§6Coins: §e1200 §7| §aOnline: §f42 §7| §bPing: §f35ms",
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				Nop.SendTip(text.ParseLegacy(s))
			}
		})
	}
}

// BenchmarkSessionSendMessage measures a chat message as Player.Message sends
// one: the string read as text and rendered back as the message of a raw
// TextPacket, for a plain line and for a line of chat with § codes. Nop drops
// the packet.
func BenchmarkSessionSendMessage(b *testing.B) {
	for name, s := range map[string]string{
		"plain": "Hello, world! How are you all doing today?",
		"coded": "§7[§bSteve§7] §fHello, world! §eHow §lare§r you?",
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				Nop.SendMessage(text.ParseLegacy(s))
			}
		})
	}
}

// BenchmarkSessionSendScoreboard measures a sidebar as a plugin updates one:
// a scoreboard of a coded name and six lines built and sent again to a
// session that shows one of the same name, so that its lines are removed and
// set. The packets are taken from the session after each update.
func BenchmarkSessionSendScoreboard(b *testing.B) {
	lines := []string{
		"§7www.example.org",
		"",
		"§aOnline: §f42",
		"§bCoins: §e1200",
		"§6Rank: §l§cAdmin",
		"§7Ping: §f35ms",
	}
	s := newCaptureSession()
	b.ReportAllocs()
	for b.Loop() {
		sb := scoreboard.New("§l§6Server Name")
		for i, line := range lines {
			sb.Set(i, line)
		}
		s.SendScoreboard(sb)
		s.taken()
	}
}
